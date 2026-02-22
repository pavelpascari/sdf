//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestE2E_SyncAfterBaseAdvances verifies that when new commits land on main
// (e.g., another PR merged), `sdf sync` correctly cascade-rebases the
// entire stack onto the new main tip.
//
// Scenario:
//
//	main ← branch-A (PR#1) ← branch-B (PR#2) ← branch-C (PR#3)
//
// Then a new commit is pushed to main (simulating another PR merging).
// After `sdf sync -y`:
//   - All three branches should be rebased onto new main
//   - All three PRs should remain OPEN
//   - The new commit from main should be reachable from all branches
func TestE2E_SyncAfterBaseAdvances(t *testing.T) {
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

	// --- Setup: Create a 3-branch stack with PRs ---
	t.Log("Setup: creating 3-branch stack")

	runSDF(t, dir, "init", "--base", "main", "--branch", "alpha", stackName)
	writeCommit(t, dir, prefix+"-alpha.txt", "alpha content\n", "feat: add alpha")

	runSDF(t, dir, "branch", "beta")
	writeCommit(t, dir, prefix+"-beta.txt", "beta content\n", "feat: add beta")

	runSDF(t, dir, "branch", "gamma")
	writeCommit(t, dir, prefix+"-gamma.txt", "gamma content\n", "feat: add gamma")

	branchA := stackName + "/alpha"
	branchB := stackName + "/beta"
	branchC := stackName + "/gamma"

	// Create PRs for all branches
	for _, br := range []string{branchA, branchB, branchC} {
		runGit(t, dir, "checkout", br)
		out := runSDF(t, dir, "pr", "--json")
		t.Logf("PR created for %s: %s", br, out)
	}

	// Verify: 3 nodes with PRs, all open
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/alpha", HasPR: true, Status: "open"},
		{BranchSuffix: "/beta", HasPR: true, Status: "open"},
		{BranchSuffix: "/gamma", HasPR: true, Status: "open"},
	})

	// --- Advance main: simulate another PR merging ---
	t.Log("Advancing main with a new commit")
	runGit(t, dir, "checkout", "main")

	// Create a file that won't conflict with the stack branches
	mainFile := prefix + "-main-advance.txt"
	writeCommit(t, dir, mainFile, "This commit simulates another PR merging into main.\n", "chore: unrelated change on main")
	runGit(t, dir, "push", "origin", "main")

	// Record the new main tip for later verification
	newMainTip := runGit(t, dir, "rev-parse", "HEAD")
	t.Logf("New main tip: %s", newMainTip[:12])

	// --- Sync: should cascade-rebase all three branches ---
	t.Log("Running sdf sync")
	runGit(t, dir, "checkout", branchC)
	syncOut := runSDF(t, dir, "sync", "-y")
	t.Log(syncOut)

	// Sync should have done real work (not "in sync")
	if strings.Contains(syncOut, "Everything is in sync") {
		t.Error("expected sync to detect stale branches, but got 'Everything is in sync'")
	}

	// Should mention rebasing
	if !strings.Contains(syncOut, "rebase") && !strings.Contains(syncOut, "Sync complete") {
		t.Errorf("expected sync to mention rebasing or completion, got: %s", syncOut)
	}

	// --- Verify: all PRs still open ---
	t.Log("Verifying all PRs are still open after rebase")
	for _, br := range []string{branchA, branchB, branchC} {
		info := runGH(t, dir, "pr", "view", br, "--json", "state")
		if !strings.Contains(info, "OPEN") {
			t.Errorf("PR for %s should be OPEN after sync, got: %s", br, info)
		}
	}

	// --- Verify: new main commit is ancestor of all branches ---
	t.Log("Verifying new main commit is reachable from all branches")
	runGit(t, dir, "fetch", "origin")
	for _, br := range []string{branchA, branchB, branchC} {
		// git merge-base --is-ancestor <main-tip> <branch>
		cmd := fmt.Sprintf("merge-base --is-ancestor %s origin/%s", newMainTip, br)
		args := strings.Fields(cmd)
		out, err := runGitMayFail(t, dir, args...)
		if err != nil {
			t.Errorf("new main commit %s is NOT an ancestor of %s (rebase didn't cascade): %s",
				newMainTip[:12], br, out)
		}
	}

	// Verify: stack state unchanged after sync (still 3 nodes, all open)
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/alpha", HasPR: true, Status: "open"},
		{BranchSuffix: "/beta", HasPR: true, Status: "open"},
		{BranchSuffix: "/gamma", HasPR: true, Status: "open"},
	})

	t.Log("Sync after base advance test passed — all branches rebased onto new main")
}

// TestE2E_InsertBranchMidStack verifies that inserting a new branch in the
// middle of an existing stack correctly retargets PR bases and that a
// subsequent sync cascade-rebases downstream branches.
//
// Starting state:
//
//	main ← A (PR#1) ← B (PR#2) ← C (PR#3) ← D (PR#4)
//
// After inserting E between A and B:
//
//	main ← A (PR#1) ← E (new) ← B (PR#2) ← C (PR#3) ← D (PR#4)
//
// Verifications:
//   - E is inserted at correct position in the stack
//   - PR#2's base is retargeted to E's branch
//   - After `sdf sync`, B/C/D are rebased onto E
//   - All PRs remain OPEN
func TestE2E_InsertBranchMidStack(t *testing.T) {
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

	// --- Setup: Create a 4-branch stack (A → B → C → D) ---
	t.Log("Setup: creating 4-branch stack A → B → C → D")

	runSDF(t, dir, "init", "--base", "main", "--branch", "step-a", stackName)
	writeCommit(t, dir, prefix+"-a.txt", "step A\n", "feat: step A")

	runSDF(t, dir, "branch", "step-b")
	writeCommit(t, dir, prefix+"-b.txt", "step B\n", "feat: step B")

	runSDF(t, dir, "branch", "step-c")
	writeCommit(t, dir, prefix+"-c.txt", "step C\n", "feat: step C")

	runSDF(t, dir, "branch", "step-d")
	writeCommit(t, dir, prefix+"-d.txt", "step D\n", "feat: step D")

	branchA := stackName + "/step-a"
	branchB := stackName + "/step-b"
	branchC := stackName + "/step-c"
	branchD := stackName + "/step-d"
	branchE := stackName + "/step-e"

	// Create PRs for A, B, C, D
	t.Log("Creating PRs for A, B, C, D")
	prNumbers := make(map[string]int)
	for _, br := range []string{branchA, branchB, branchC, branchD} {
		runGit(t, dir, "checkout", br)
		out := runSDF(t, dir, "pr", "--json")

		var result struct {
			Number int `json:"number"`
		}
		json.Unmarshal([]byte(out), &result)
		prNumbers[br] = result.Number
		t.Logf("PR#%d created for %s", result.Number, br)
	}

	// Verify: 4 nodes A, B, C, D with PRs
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/step-a", HasPR: true, Status: "open"},
		{BranchSuffix: "/step-b", HasPR: true, Status: "open"},
		{BranchSuffix: "/step-c", HasPR: true, Status: "open"},
		{BranchSuffix: "/step-d", HasPR: true, Status: "open"},
	})

	// Verify initial PR bases
	pr2Base := runGH(t, dir, "pr", "view", fmt.Sprint(prNumbers[branchB]), "--json", "baseRefName")
	if !strings.Contains(pr2Base, "step-a") {
		t.Fatalf("before insert: PR for B should have base containing step-a, got: %s", pr2Base)
	}

	// --- Insert E between A and B ---
	t.Log("Inserting branch E between A and B")

	// Checkout branch A so `sdf branch` inserts after A
	runGit(t, dir, "checkout", branchA)
	runSDF(t, dir, "branch", "step-e")

	// Verify we're on branch E
	current := runGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if current != branchE {
		t.Fatalf("expected to be on %s after insert, got %s", branchE, current)
	}

	// Add a commit to E
	writeCommit(t, dir, prefix+"-e.txt", "step E (inserted)\n", "feat: step E inserted between A and B")

	// Create a PR for E
	eOut := runSDF(t, dir, "pr", "--json")
	var eResult struct {
		Number int `json:"number"`
	}
	json.Unmarshal([]byte(eOut), &eResult)
	prNumbers[branchE] = eResult.Number
	t.Logf("PR#%d created for %s (inserted)", eResult.Number, branchE)

	// Verify: 5 nodes A, E, B, C, D — E inserted between A and B
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/step-a", HasPR: true, Status: "open"},
		{BranchSuffix: "/step-e", HasPR: true, Status: "open"},
		{BranchSuffix: "/step-b", HasPR: true, Status: "open"},
		{BranchSuffix: "/step-c", HasPR: true, Status: "open"},
		{BranchSuffix: "/step-d", HasPR: true, Status: "open"},
	})

	// --- Verify: PR for B should now have base=E ---
	t.Log("Verifying PR for B was retargeted to E")
	pr2BaseAfter := runGH(t, dir, "pr", "view", fmt.Sprint(prNumbers[branchB]), "--json", "baseRefName")
	if !strings.Contains(pr2BaseAfter, "step-e") {
		t.Errorf("after insert: PR for B should have base containing step-e, got: %s", pr2BaseAfter)
	}

	// --- Sync: cascade-rebase B, C, D onto E ---
	t.Log("Running sdf sync to cascade-rebase downstream branches")
	runGit(t, dir, "checkout", branchD)
	syncOut := runSDF(t, dir, "sync", "-y")
	t.Log(syncOut)

	// --- Verify: all 5 PRs still open ---
	t.Log("Verifying all PRs are still open")
	for _, br := range []string{branchA, branchE, branchB, branchC, branchD} {
		info := runGH(t, dir, "pr", "view", br, "--json", "state")
		if !strings.Contains(info, "OPEN") {
			t.Errorf("PR for %s should be OPEN after sync, got: %s", br, info)
		}
	}

	// --- Verify: correct base chain after insert + sync ---
	t.Log("Verifying final base chain: A→E→B→C→D")

	expectedBases := map[string]string{
		branchA: "main",
		branchE: "step-a",
		branchB: "step-e",
		branchC: "step-b",
		branchD: "step-c",
	}

	for br, expectedBase := range expectedBases {
		prNum := prNumbers[br]
		info := runGH(t, dir, "pr", "view", fmt.Sprint(prNum), "--json", "baseRefName")

		var result struct {
			BaseRefName string `json:"baseRefName"`
		}
		json.Unmarshal([]byte(info), &result)

		if !strings.Contains(result.BaseRefName, expectedBase) {
			t.Errorf("PR for %s: expected base containing %q, got %q", br, expectedBase, result.BaseRefName)
		}
	}

	// --- Verify: E's commit is reachable from B, C, D ---
	t.Log("Verifying E's content is reachable from downstream branches")
	eTip := runGit(t, dir, "rev-parse", branchE)
	runGit(t, dir, "fetch", "origin")
	for _, br := range []string{branchB, branchC, branchD} {
		args := []string{"merge-base", "--is-ancestor", eTip, "origin/" + br}
		_, err := runGitMayFail(t, dir, args...)
		if err != nil {
			t.Errorf("E's tip is NOT an ancestor of %s — rebase didn't cascade through insert point", br)
		}
	}

	t.Log("Insert branch mid-stack test passed — A→E→B→C→D with correct bases")
}
