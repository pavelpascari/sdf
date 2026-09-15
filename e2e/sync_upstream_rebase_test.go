//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestE2E_SyncAfterUpstreamBranchRebased verifies that when an upstream
// branch in the stack is rebased onto an updated base, `sdf sync` correctly
// cascade-rebases downstream branches — replaying only their branch-specific
// commits on top of the new parent.
//
// This is the scenario that trips people up with manual git: the downstream
// branch shares history with the upstream branch, so a naive `git rebase`
// replays shared commits and hits spurious conflicts. SDF's `--onto` rebase
// avoids this by tracking each node's BaseTip.
//
// Scenario: A developer builds a feature in two stacked PRs:
//
//	main ← add-signed-ids-engine ← add-signed-ids-wiring
//
// The engine branch gets review feedback and is amended (rewritten). After
// `sdf sync -y`, the wiring branch should be cleanly rebased on top of the
// rewritten engine — with only the wiring-specific commits replayed.
//
// This mirrors a real-world situation where Claude Code attempted a manual
// rebase of the wiring branch, hit conflicts from shared history, had to
// abort, manually identify wiring-only commits with `git log --not`, and
// then use `git rebase --onto` to replay just those commits. SDF automates
// all of that via `sdf sync`.
func TestE2E_SyncAfterUpstreamBranchRebased(t *testing.T) {
	dir := e2eRepo(t)
	setupRecording(t)
	prefix := testPrefix()

	t.Cleanup(func() {
		runGit(t, dir, "checkout", "main")
		cleanupPRs(t, dir, prefix)
		cleanupBranches(t, dir, prefix)
		os.RemoveAll(dir + "/.sdf")
	})

	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "pull", "origin", "main")

	stackName := prefix

	// ── Phase 1: Build initial 2-branch stack ──────────────────────────

	t.Log("Building stack: add-signed-ids-engine → add-signed-ids-wiring")

	runSDF(t, dir, "init", "--base", "main", "--branch", "add-signed-ids-engine", stackName)

	// Engine branch: core implementation (multiple commits to simulate real work)
	writeCommit(t, dir, prefix+"-engine.go",
		"package engine\n\ntype SignedID struct {\n\tPayload   []byte\n\tSignature []byte\n}\n\nfunc Generate(key []byte) SignedID { return SignedID{} }\n",
		"feat: add signed ID generation engine")
	writeCommit(t, dir, prefix+"-engine_test.go",
		"package engine_test\n\nimport \"testing\"\n\nfunc TestGenerate(t *testing.T) {}\n",
		"test: add engine unit tests")

	runSDF(t, dir, "branch", "add-signed-ids-wiring")

	// Wiring branch: integrate engine into the app
	writeCommit(t, dir, prefix+"-wiring.go",
		"package wiring\n\nimport \"engine\"\n\nfunc WireGenerator() engine.SignedID { return engine.Generate(nil) }\n",
		"feat: wire signed IDs into SignedIdGenerator")
	writeCommit(t, dir, prefix+"-wiring-parser.go",
		"package wiring\n\nfunc WireParser() error { return nil }\n",
		"feat: wire signed IDs into SignedIdParser")

	branchEngine := stackName + "/add-signed-ids-engine"
	branchWiring := stackName + "/add-signed-ids-wiring"

	// Create PRs for both branches
	for _, br := range []string{branchEngine, branchWiring} {
		runGit(t, dir, "checkout", br)
		out := runSDF(t, dir, "pr", "--json")
		t.Logf("PR created for %s: %s", br, out)
	}

	// Verify: 2 nodes with PRs
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/add-signed-ids-engine", HasPR: true, Status: "open"},
		{BranchSuffix: "/add-signed-ids-wiring", HasPR: true, Status: "open"},
	})

	// Record the engine tip and wiring tip before rewrite
	runGit(t, dir, "checkout", branchEngine)
	engineTipBefore := runGit(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "checkout", branchWiring)
	wiringTipBefore := runGit(t, dir, "rev-parse", "HEAD")
	t.Logf("Before rewrite — engine: %s, wiring: %s", engineTipBefore[:12], wiringTipBefore[:12])

	// ── Phase 2: Rewrite the engine branch (simulate review feedback) ──

	t.Log("Simulating review feedback: amending engine branch with improved implementation")
	runGit(t, dir, "checkout", branchEngine)

	// Add a new commit that changes the engine (simulates addressing review comments)
	writeCommit(t, dir, prefix+"-engine.go",
		"package engine\n\nimport \"crypto/hmac\"\n\ntype SignedID struct {\n\tPayload   []byte\n\tSignature []byte\n\tVersion   int\n}\n\nfunc Generate(key []byte) SignedID {\n\tsig := hmac.New(nil, key)\n\treturn SignedID{Version: 2, Signature: sig.Sum(nil)}\n}\n",
		"fix: use HMAC for signature generation per review feedback")
	writeCommit(t, dir, prefix+"-engine-version.go",
		"package engine\n\nconst VersionConstraint = \">= 2.0\"\n",
		"fix: remove version constraint from signed-ids gem")

	// Push the rewritten engine branch
	runGit(t, dir, "push", "origin", branchEngine)

	engineTipAfter := runGit(t, dir, "rev-parse", "HEAD")
	t.Logf("After rewrite — engine: %s (was %s)", engineTipAfter[:12], engineTipBefore[:12])

	// ── Phase 3: Sync should cascade-rebase wiring onto rewritten engine ─

	t.Log("Running sdf sync — wiring branch should rebase onto rewritten engine")
	runGit(t, dir, "checkout", branchWiring)
	syncOut := runSDF(t, dir, "sync", "-y")
	t.Log(syncOut)

	// Sync should have detected the rewritten engine and done work
	if strings.Contains(syncOut, "Everything is in sync") {
		t.Error("expected sync to detect stale wiring branch, but got 'Everything is in sync'")
	}

	// ── Phase 4: Verify correctness ────────────────────────────────────

	// 4a. Both PRs still open
	t.Log("Verifying both PRs are still open after rebase")
	for _, br := range []string{branchEngine, branchWiring} {
		info := runGH(t, dir, "pr", "view", br, "--json", "state")
		if !strings.Contains(info, "OPEN") {
			t.Errorf("PR for %s should be OPEN after sync, got: %s", br, info)
		}
	}

	// 4b. The rewritten engine tip is an ancestor of the wiring branch
	// (proves the wiring branch was rebased onto the NEW engine, not the old one)
	t.Log("Verifying rewritten engine tip is ancestor of wiring branch")
	runGit(t, dir, "fetch", "origin")
	args := []string{"merge-base", "--is-ancestor", engineTipAfter, "origin/" + branchWiring}
	if _, err := runGitMayFail(t, dir, args...); err != nil {
		t.Errorf("rewritten engine tip %s is NOT an ancestor of %s — rebase didn't cascade",
			engineTipAfter[:12], branchWiring)
	}

	// 4c. The OLD engine tip is NOT an ancestor of the wiring branch
	// (proves the wiring branch was actually rebased, not left on stale history)
	t.Log("Verifying old engine tip is no longer in wiring branch's ancestry")
	args = []string{"merge-base", "--is-ancestor", engineTipBefore, "origin/" + branchWiring}
	if _, err := runGitMayFail(t, dir, args...); err == nil {
		t.Errorf("old engine tip %s is STILL an ancestor of %s — wiring branch was not rebased",
			engineTipBefore[:12], branchWiring)
	}

	// 4d. Wiring-specific files still exist (commits were replayed, not lost)
	t.Log("Verifying wiring-specific commits were preserved")
	runGit(t, dir, "checkout", branchWiring)
	for _, f := range []string{prefix + "-wiring.go", prefix + "-wiring-parser.go"} {
		if _, err := os.Stat(dir + "/" + f); os.IsNotExist(err) {
			t.Errorf("wiring file %s is missing — wiring-specific commits were not replayed", f)
		}
	}

	// 4e. Engine review changes are also present in wiring branch
	t.Log("Verifying engine review changes are visible from wiring branch")
	for _, f := range []string{prefix + "-engine-version.go"} {
		if _, err := os.Stat(dir + "/" + f); os.IsNotExist(err) {
			t.Errorf("engine file %s is missing from wiring branch — rebase didn't pick up engine changes", f)
		}
	}

	// 4f. Stack topology unchanged
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/add-signed-ids-engine", HasPR: true, Status: "open"},
		{BranchSuffix: "/add-signed-ids-wiring", HasPR: true, Status: "open"},
	})

	// 4g. Commit count sanity: wiring branch should have exactly 2 commits
	// ahead of engine (the two wiring-specific commits). This proves SDF
	// replayed only the branch-specific commits, not shared history.
	t.Log("Verifying wiring branch has exactly 2 commits ahead of engine")
	countOut := runGit(t, dir, "rev-list", "--count", fmt.Sprintf("%s..%s", branchEngine, branchWiring))
	if countOut != "2" {
		t.Errorf("expected wiring to be exactly 2 commits ahead of engine, got %s — "+
			"shared commits may have been duplicated", countOut)
	}

	t.Log("Upstream rebase verified — wiring cleanly rebased onto rewritten engine, " +
		"only 2 wiring-specific commits replayed, no spurious conflicts")
}
