package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
)

// stripANSI removes ANSI escape codes from a string for test assertions.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// syncTestRepo sets up a temporary git repository with an SDF stack of 3
// branches for testing sync plan computation:
//
//	main (base) ← branchA [a1] ← branchB [b1] ← branchC [c1]
//
// All BaseTips are set correctly. The caller is chdir'd into the repo on
// branchC.
func syncTestRepo(t *testing.T) (repoDir string) {
	t.Helper()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	writeFile := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Initialize repo
	git("init", "-b", "main")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "Test")
	git("config", "commit.gpgsign", "false")

	// Initial commit on main
	writeFile("README.md", "# test\n")
	git("add", "README.md")
	git("commit", "-m", "initial")
	mainTip := git("rev-parse", "HEAD")

	// branchA
	git("checkout", "-b", "branchA")
	writeFile("a1.txt", "a1\n")
	git("add", "a1.txt")
	git("commit", "-m", "a1")
	branchATip := git("rev-parse", "HEAD")

	// branchB
	git("checkout", "-b", "branchB")
	writeFile("b1.txt", "b1\n")
	git("add", "b1.txt")
	git("commit", "-m", "b1")
	branchBTip := git("rev-parse", "HEAD")

	// branchC
	git("checkout", "-b", "branchC")
	writeFile("c1.txt", "c1\n")
	git("add", "c1.txt")
	git("commit", "-m", "c1")

	// Write SDF stack
	s := &stack.Stack{
		StackID: "test-stack",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "branchA", Status: "open", BaseTip: mainTip},
			{Branch: "branchB", Status: "open", BaseTip: branchATip},
			{Branch: "branchC", Status: "open", BaseTip: branchBTip},
		},
	}
	if err := stack.Save(dir, s); err != nil {
		t.Fatal(err)
	}
	git("add", ".sdf")
	git("commit", "-m", "sdf: init stack")

	return dir
}

// syncTestRepoWithRemote creates a test repo for execution-path tests.
// Like syncTestRepo but: (1) .sdf is gitignored (matching real usage) so
// rebases don't conflict with on-disk stack files, and (2) a bare remote
// is added so git push works.
// Returns the repo dir (cwd is already set to it).
func syncTestRepoWithRemote(t *testing.T) string {
	t.Helper()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	writeFile := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Initialize repo with .sdf gitignored (matching real usage).
	git("init", "-b", "main")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "Test")
	git("config", "commit.gpgsign", "false")

	writeFile(".gitignore", ".sdf/\n")
	writeFile("README.md", "# test\n")
	git("add", ".")
	git("commit", "-m", "initial")
	mainTip := git("rev-parse", "HEAD")

	// branchA
	git("checkout", "-b", "branchA")
	writeFile("a1.txt", "a1\n")
	git("add", "a1.txt")
	git("commit", "-m", "a1")
	branchATip := git("rev-parse", "HEAD")

	// branchB
	git("checkout", "-b", "branchB")
	writeFile("b1.txt", "b1\n")
	git("add", "b1.txt")
	git("commit", "-m", "b1")
	branchBTip := git("rev-parse", "HEAD")

	// branchC
	git("checkout", "-b", "branchC")
	writeFile("c1.txt", "c1\n")
	git("add", "c1.txt")
	git("commit", "-m", "c1")

	// Write SDF stack (untracked, gitignored)
	s := &stack.Stack{
		StackID: "test-stack",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "branchA", Status: "open", BaseTip: mainTip},
			{Branch: "branchB", Status: "open", BaseTip: branchATip},
			{Branch: "branchC", Status: "open", BaseTip: branchBTip},
		},
	}
	if err := stack.Save(dir, s); err != nil {
		t.Fatal(err)
	}

	// Create bare remote and push all branches
	remoteDir := filepath.Join(t.TempDir(), "remote.git")
	git("clone", "--bare", dir, remoteDir)
	git("remote", "add", "origin", remoteDir)
	git("push", "origin", "--all")

	return dir
}

// testBus creates a Bus that writes to a buffer for test output capture.
func testBus(t *testing.T) (*render.Bus, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	bus := render.NewBus(&buf, io.Discard, render.Options{})
	return bus, &buf
}

// filterActions returns only actions of the given kind from the plan.
func filterActions(plan []syncAction, kind string) []syncAction {
	var result []syncAction
	for _, a := range plan {
		if a.kind == kind {
			result = append(result, a)
		}
	}
	return result
}

// actionBranches extracts branch names from a list of actions.
func actionBranches(actions []syncAction) []string {
	var names []string
	for _, a := range actions {
		names = append(names, a.branch)
	}
	return names
}

// --- computeSyncPlan tests ---

func TestComputeSyncPlan_InSync(t *testing.T) {
	syncTestRepo(t)

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	plan := computeSyncPlan(s, nil)

	if len(plan) != 0 {
		t.Errorf("expected empty plan when everything is in sync, got %d actions", len(plan))
		for _, a := range plan {
			t.Logf("  %s %s", a.kind, a.branch)
		}
	}
}

func TestComputeSyncPlan_MergedHead(t *testing.T) {
	syncTestRepo(t)

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Mark branchA as merged
	s.Nodes[0].Status = "merged"

	plan := computeSyncPlan(s, nil)

	// Should have: skip branchA (merged), update-tip branchB
	skips := filterActions(plan, "skip-merged")
	rebases := filterActions(plan, "rebase")
	updateTips := filterActions(plan, "update-tip")
	pushes := filterActions(plan, "push")

	if len(skips) != 1 || skips[0].branch != "branchA" {
		t.Errorf("expected 1 skip-merged for branchA, got %v", skips)
	}

	// branchB is already correctly based on main ancestry; sync should
	// refresh its tracked base tip without rewriting commits.
	if len(rebases) != 0 {
		t.Fatalf("expected no rebase actions, got %d", len(rebases))
	}
	if len(updateTips) != 1 || updateTips[0].branch != "branchB" {
		t.Errorf("expected 1 update-tip for branchB, got %v", actionBranches(updateTips))
	}

	// No push is needed when only the recorded base tip is refreshed.
	if len(pushes) != 0 {
		t.Errorf("expected no pushes for update-tip-only sync, got %v", pushes)
	}
}

func TestComputeSyncPlan_MergedWithPR(t *testing.T) {
	syncTestRepo(t)

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Mark branchA as merged, branchB has a PR
	s.Nodes[0].Status = "merged"
	s.Nodes[1].PR = 42

	plan := computeSyncPlan(s, nil)

	// Check for update-pr-base action (only if gh is available)
	prActions := filterActions(plan, "update-pr-base")
	if ghpkg.Available() {
		if len(prActions) < 1 {
			t.Error("expected update-pr-base action for branchB PR #42 (gh is available)")
		} else if prActions[0].pr != 42 || prActions[0].onto != "main" {
			t.Errorf("expected update PR #42 base to main, got PR #%d base to %s",
				prActions[0].pr, prActions[0].onto)
		}
	} else {
		if len(prActions) != 0 {
			t.Errorf("expected no update-pr-base actions when gh is unavailable, got %d", len(prActions))
		}
	}
}

func TestComputeSyncPlan_StaleBaseTip(t *testing.T) {
	dir := syncTestRepo(t)

	// Add a commit to main to make branchA's BaseTip stale
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	git("checkout", "main")
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0644)
	git("add", "new.txt")
	git("commit", "-m", "new commit on main")
	git("checkout", "branchC") // restore

	s, err := stack.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	plan := computeSyncPlan(s, nil)

	rebases := filterActions(plan, "rebase")
	pushes := filterActions(plan, "push")

	// branchA needs rebase (stale BaseTip)
	if len(rebases) < 1 {
		t.Fatal("expected at least 1 rebase action for stale BaseTip")
	}
	if rebases[0].branch != "branchA" || rebases[0].onto != "main" {
		t.Errorf("expected rebase branchA onto main, got rebase %s onto %s",
			rebases[0].branch, rebases[0].onto)
	}

	// branchA should be pushed
	if len(pushes) < 1 || pushes[0].branch != "branchA" {
		t.Errorf("expected push for branchA, got %v", actionBranches(pushes))
	}
}

func TestComputeSyncPlan_CascadeFromMerge(t *testing.T) {
	syncTestRepo(t)

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Mark branchA as merged → branchB rebases onto main,
	// then branchC cascades because branchB was rebased
	s.Nodes[0].Status = "merged"

	plan := computeSyncPlan(s, nil)

	skips := filterActions(plan, "skip-merged")
	rebases := filterActions(plan, "rebase")
	updateTips := filterActions(plan, "update-tip")
	pushes := filterActions(plan, "push")

	if len(skips) != 1 {
		t.Errorf("expected 1 skip-merged, got %d", len(skips))
	}

	// branchB can be handled by tip refresh; branchC stays unchanged.
	if len(rebases) != 0 {
		t.Fatalf("expected no rebases, got %d: %v", len(rebases), actionBranches(rebases))
	}
	if len(updateTips) != 1 || updateTips[0].branch != "branchB" {
		t.Fatalf("expected one update-tip (branchB), got %v", actionBranches(updateTips))
	}

	// No pushes are needed for update-tip-only operations.
	if len(pushes) != 0 {
		t.Fatalf("expected no pushes, got %d: %v", len(pushes), actionBranches(pushes))
	}
}

func TestComputeSyncPlan_CascadeFromStaleParent(t *testing.T) {
	dir := syncTestRepo(t)

	// Add a commit to main → branchA stale → cascade to B and C
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	git("checkout", "main")
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0644)
	git("add", "new.txt")
	git("commit", "-m", "new commit on main")
	git("checkout", "branchC")

	s, err := stack.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	plan := computeSyncPlan(s, nil)

	rebases := filterActions(plan, "rebase")
	pushes := filterActions(plan, "push")

	// All three branches should be rebased in cascade
	rebaseBranches := actionBranches(rebases)
	if len(rebases) != 3 {
		t.Fatalf("expected 3 rebases (full cascade), got %d: %v",
			len(rebases), rebaseBranches)
	}
	if rebases[0].branch != "branchA" || rebases[0].onto != "main" {
		t.Errorf("first rebase: want branchA onto main, got %s onto %s",
			rebases[0].branch, rebases[0].onto)
	}
	if rebases[1].branch != "branchB" || rebases[1].onto != "branchA" {
		t.Errorf("second rebase: want branchB onto branchA, got %s onto %s",
			rebases[1].branch, rebases[1].onto)
	}
	if rebases[2].branch != "branchC" || rebases[2].onto != "branchB" {
		t.Errorf("third rebase: want branchC onto branchB, got %s onto %s",
			rebases[2].branch, rebases[2].onto)
	}

	// All three should be pushed
	if len(pushes) != 3 {
		t.Errorf("expected 3 pushes, got %d: %v", len(pushes), actionBranches(pushes))
	}
}

func TestComputeSyncPlan_MergedTail(t *testing.T) {
	syncTestRepo(t)

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Mark the last node (branchC) as merged → just skip, no downstream to rebase
	s.Nodes[2].Status = "merged"

	plan := computeSyncPlan(s, nil)

	skips := filterActions(plan, "skip-merged")
	rebases := filterActions(plan, "rebase")

	if len(skips) != 1 || skips[0].branch != "branchC" {
		t.Errorf("expected 1 skip-merged for branchC, got %v", skips)
	}
	if len(rebases) != 0 {
		t.Errorf("expected no rebases when tail node is merged, got %d: %v",
			len(rebases), actionBranches(rebases))
	}
}

func TestComputeSyncPlan_MultipleMerged(t *testing.T) {
	syncTestRepo(t)

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Mark branchA and branchB as merged → both skipped,
	// branchC gets rebased onto main (ParentBranch skips merged nodes)
	s.Nodes[0].Status = "merged"
	s.Nodes[1].Status = "merged"

	plan := computeSyncPlan(s, nil)

	skips := filterActions(plan, "skip-merged")
	rebases := filterActions(plan, "rebase")
	updateTips := filterActions(plan, "update-tip")
	pushes := filterActions(plan, "push")

	if len(skips) != 2 {
		t.Errorf("expected 2 skip-merged, got %d", len(skips))
	}

	// branchC remains ancestry-correct and only needs its tracked base tip refreshed.
	if len(rebases) != 0 {
		t.Fatalf("expected no rebases, got %d", len(rebases))
	}
	if len(updateTips) != 1 || updateTips[0].branch != "branchC" {
		t.Errorf("expected update-tip for branchC, got %v", actionBranches(updateTips))
	}

	if len(pushes) != 0 {
		t.Errorf("expected no push actions, got %v", actionBranches(pushes))
	}
}

func TestComputeSyncPlan_MergedMiddle(t *testing.T) {
	syncTestRepo(t)

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Mark branchB (middle) as merged → branchC rebases onto branchA
	// (ParentBranch skips merged branchB, lands on branchA)
	s.Nodes[1].Status = "merged"

	plan := computeSyncPlan(s, nil)

	skips := filterActions(plan, "skip-merged")
	rebases := filterActions(plan, "rebase")
	updateTips := filterActions(plan, "update-tip")

	if len(skips) != 1 || skips[0].branch != "branchB" {
		t.Errorf("expected skip-merged for branchB, got %v", skips)
	}

	// branchC is already ancestry-correct on branchA and should use update-tip.
	if len(rebases) != 0 {
		t.Fatalf("expected no rebases, got %d", len(rebases))
	}
	if len(updateTips) != 1 || updateTips[0].branch != "branchC" {
		t.Errorf("expected update-tip for branchC, got %v", actionBranches(updateTips))
	}
}

// --- printSyncPlan tests ---

func TestPrintSyncPlan_Output(t *testing.T) {
	plan := []syncAction{
		{kind: "skip-merged", branch: "feat/auth", pr: 10},
		{kind: "rebase", branch: "feat/api", onto: "main"},
		{kind: "push", branch: "feat/api"},
		{kind: "update-pr-base", branch: "feat/api", pr: 42, onto: "main"},
		{kind: "update-content", branch: "feat/api", pr: 42},
	}

	// printSyncPlan writes through the bus, so capture via the bus writer
	var buf bytes.Buffer
	bus := render.NewBus(&buf, io.Discard, render.Options{})

	printSyncPlan(plan, bus)

	_ = bus.Finish()
	output := stripANSI(buf.String())

	// Verify each action type appears in the output
	checks := []struct {
		label    string
		contains string
	}{
		{"header", "Sync plan:"},
		{"merged", "PR #10 (feat/auth) merged"},
		{"rebase+push", "rebase feat/api onto main + push"},
		{"pr-base", "update PR #42 base"},
		{"content", "update PR #42 content"},
	}

	for _, c := range checks {
		if !strings.Contains(output, c.contains) {
			t.Errorf("printSyncPlan output missing %s: expected to contain %q\ngot:\n%s",
				c.label, c.contains, output)
		}
	}

	// Verify rebase+push are combined (no separate "push feat/api" line)
	if strings.Contains(output, "push feat/api\n") {
		t.Error("printSyncPlan should combine rebase+push, but found separate push line")
	}
}

// --- computeSyncPlan with update options ---

func TestComputeSyncPlan_WithContent(t *testing.T) {
	syncTestRepo(t)

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Give branchA and branchB PRs
	s.Nodes[0].PR = 10
	s.Nodes[1].PR = 11

	opts := &syncOptions{
		withContent: true,
		cfg:         cfgpkg.Defaults(),
	}

	plan := computeSyncPlan(s, opts)

	contentActions := filterActions(plan, "update-content")

	// Should have content updates for open PRs with PRs (branchA and branchB)
	if len(contentActions) != 2 {
		t.Errorf("expected 2 update-content actions, got %d", len(contentActions))
	}
}

func TestComputeSyncPlan_SkipsMergedForUpdates(t *testing.T) {
	syncTestRepo(t)

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Give all branches PRs, mark branchA as merged
	s.Nodes[0].PR = 10
	s.Nodes[0].Status = "merged"
	s.Nodes[1].PR = 11
	s.Nodes[2].PR = 12

	opts := &syncOptions{
		withContent: true,
		cfg:         cfgpkg.Defaults(),
	}

	plan := computeSyncPlan(s, opts)

	contentActions := filterActions(plan, "update-content")

	// Should NOT have content update for merged branchA
	for _, a := range contentActions {
		if a.pr == 10 {
			t.Error("should not update content for merged PR #10")
		}
	}

	// Should have content updates for open PRs (branchB #11 and branchC #12)
	if len(contentActions) != 2 {
		t.Errorf("expected 2 update-content actions for open PRs, got %d", len(contentActions))
	}
}

// --- stack-scoped sync tests (--from-head) ---

func TestComputeSyncPlan_StackScopedDefault(t *testing.T) {
	dir := syncTestRepo(t)

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Record the current main tip (this is what nodes[0].BaseTip points to)
	preFFBaseTip := git("rev-parse", "main")

	// Advance main with an unrelated commit (simulating fast-forward)
	git("checkout", "main")
	os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("unrelated\n"), 0644)
	git("add", "unrelated.txt")
	git("commit", "-m", "unrelated work on main")
	git("checkout", "branchC")

	// With fromHead=false and preFFBaseTip matching the old main tip,
	// no rebase should be triggered (main advanced from unrelated work).
	opts := &syncOptions{
		fromHead:     false,
		preFFBaseTip: preFFBaseTip,
	}

	plan := computeSyncPlan(s, opts)

	rebases := filterActions(plan, "rebase")
	if len(rebases) != 0 {
		t.Errorf("expected no rebases with stack-scoped sync when main advanced, got %d", len(rebases))
		for _, a := range rebases {
			t.Logf("  rebase %s onto %s", a.branch, a.onto)
		}
	}
}

func TestComputeSyncPlan_FromHead(t *testing.T) {
	dir := syncTestRepo(t)

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Advance main with an unrelated commit
	git("checkout", "main")
	os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("unrelated\n"), 0644)
	git("add", "unrelated.txt")
	git("commit", "-m", "unrelated work on main")
	git("checkout", "branchC")

	// With fromHead=true, should trigger full cascade rebase
	opts := &syncOptions{
		fromHead: true,
	}

	plan := computeSyncPlan(s, opts)

	rebases := filterActions(plan, "rebase")
	if len(rebases) != 3 {
		t.Errorf("expected 3 rebases with --from-head when main advanced, got %d", len(rebases))
		for _, a := range rebases {
			t.Logf("  rebase %s onto %s", a.branch, a.onto)
		}
	}
	if len(rebases) >= 1 && rebases[0].branch != "branchA" {
		t.Errorf("expected first rebase to be branchA, got %s", rebases[0].branch)
	}
}

func TestComputeSyncPlan_StackScopedWithMerge(t *testing.T) {
	dir := syncTestRepo(t)

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Record pre-FF base tip
	preFFBaseTip := git("rev-parse", "main")

	// Advance main (simulating branchA merged + other work)
	git("checkout", "main")
	os.WriteFile(filepath.Join(dir, "merged.txt"), []byte("merged\n"), 0644)
	git("add", "merged.txt")
	git("commit", "-m", "merged branchA into main")
	git("checkout", "branchC")

	// Mark branchA as merged
	s.Nodes[0].Status = "merged"

	// Even with fromHead=false, branchB should rebase because its parent
	// changed (from branchA to main) and its BaseTip (branchA's old SHA)
	// doesn't match preFFBaseTip (old main SHA).
	opts := &syncOptions{
		fromHead:     false,
		preFFBaseTip: preFFBaseTip,
	}

	plan := computeSyncPlan(s, opts)

	skips := filterActions(plan, "skip-merged")
	rebases := filterActions(plan, "rebase")
	updateTips := filterActions(plan, "update-tip")

	if len(skips) != 1 || skips[0].branch != "branchA" {
		t.Errorf("expected 1 skip-merged for branchA, got %v", skips)
	}

	// branchB's parent changed from branchA to main (merged node skipped).
	// The current main tip (M2, with merged.txt) is NOT an ancestor of branchB,
	// so branchB must be rebased, not just update-tip.
	if len(rebases) < 1 || rebases[0].branch != "branchB" {
		t.Fatalf("expected rebase for branchB (new main tip not ancestor), got rebases=%v updateTips=%v",
			actionBranches(rebases), actionBranches(updateTips))
	}

	// branchC should cascade from branchB's rebase.
	if len(rebases) < 2 || rebases[1].branch != "branchC" {
		t.Fatalf("expected cascade rebase for branchC, got rebases=%v",
			actionBranches(rebases))
	}

	// No update-tip actions — both branches need real rebases.
	if len(updateTips) != 0 {
		t.Errorf("expected no update-tip actions, got %v", actionBranches(updateTips))
	}
}

// --- buildDescriptionPrompt tests ---

func TestBuildDescriptionPrompt(t *testing.T) {
	subjects := []string{"feat: add user auth", "fix: handle edge case"}
	diff := "diff --git a/auth.go b/auth.go\n+func Login() {}\n"

	prompt := buildDescriptionPrompt("feat/auth", subjects, diff, "")

	checks := []string{
		"Branch: feat/auth",
		"feat: add user auth",
		"fix: handle edge case",
		"auth.go",
		"## Summary",
		"## Changes",
		"Diff:",
	}

	for _, c := range checks {
		if !strings.Contains(prompt, c) {
			t.Errorf("prompt missing %q\ngot:\n%s", c, prompt)
		}
	}
}

func TestBuildDescriptionPrompt_NoDiff(t *testing.T) {
	subjects := []string{"initial commit"}
	prompt := buildDescriptionPrompt("feat/init", subjects, "", "")

	if strings.Contains(prompt, "Diff:") {
		t.Error("prompt should not contain Diff section when diff is empty")
	}
	if !strings.Contains(prompt, "initial commit") {
		t.Error("prompt should contain commit subject")
	}
}

// confirmSync is now a thin wrapper around ui.Confirm (huh library).
// Interactive prompt behavior is tested by huh's own test suite.

// --- SyncResult JSON tests ---

func TestSyncResultJSON(t *testing.T) {
	result := SyncResult{
		Stack: "my-feature",
		Base:  "main",
		Branches: []BranchResult{
			{Branch: "feat-a", PR: 42, Action: "merged"},
			{Branch: "feat-b", PR: 43, Action: "rebased", Pushed: true, BaseUpdated: true},
			{Branch: "feat-c", Action: "blocked", Reason: "depends on a branch that failed"},
		},
		PRUpdates: []PRUpdate{
			{PR: 43, Field: "nav", Status: "updated"},
			{PR: 43, Field: "title", Status: "unchanged"},
		},
		Warnings: []string{"fetch failed"},
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var roundtrip SyncResult
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if roundtrip.Stack != "my-feature" {
		t.Errorf("stack: got %q, want %q", roundtrip.Stack, "my-feature")
	}
	if roundtrip.Base != "main" {
		t.Errorf("base: got %q, want %q", roundtrip.Base, "main")
	}
	if len(roundtrip.Branches) != 3 {
		t.Fatalf("branches: got %d, want 3", len(roundtrip.Branches))
	}
	if roundtrip.Branches[0].Action != "merged" {
		t.Errorf("branches[0].action: got %q, want %q", roundtrip.Branches[0].Action, "merged")
	}
	if roundtrip.Branches[1].Action != "rebased" {
		t.Errorf("branches[1].action: got %q, want %q", roundtrip.Branches[1].Action, "rebased")
	}
	if !roundtrip.Branches[1].Pushed {
		t.Error("branches[1].pushed: got false, want true")
	}
	if !roundtrip.Branches[1].BaseUpdated {
		t.Error("branches[1].base_updated: got false, want true")
	}
	if roundtrip.Branches[2].Reason != "depends on a branch that failed" {
		t.Errorf("branches[2].reason: got %q", roundtrip.Branches[2].Reason)
	}
	if len(roundtrip.PRUpdates) != 2 {
		t.Fatalf("pr_updates: got %d, want 2", len(roundtrip.PRUpdates))
	}
	if roundtrip.PRUpdates[0].Field != "nav" {
		t.Errorf("pr_updates[0].field: got %q, want %q", roundtrip.PRUpdates[0].Field, "nav")
	}
	if len(roundtrip.Warnings) != 1 || roundtrip.Warnings[0] != "fetch failed" {
		t.Errorf("warnings: got %v", roundtrip.Warnings)
	}
	if roundtrip.Error != "" {
		t.Errorf("error: got %q, want empty", roundtrip.Error)
	}
}

func TestSyncResultJSON_EmptyOmitsOptional(t *testing.T) {
	result := SyncResult{Stack: "test", Base: "main", Branches: []BranchResult{}}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	s := string(data)
	if strings.Contains(s, "pr_updates") {
		t.Error("empty pr_updates should be omitted")
	}
	if strings.Contains(s, "warnings") {
		t.Error("empty warnings should be omitted")
	}
	if strings.Contains(s, `"error"`) {
		t.Error("empty error should be omitted")
	}
	// branches should be [] not null
	if !strings.Contains(s, `"branches":[]`) {
		t.Errorf("branches should be empty array, got: %s", s)
	}
}

func TestSyncResultJSON_WithError(t *testing.T) {
	result := SyncResult{
		Stack: "test",
		Base:  "main",
		Branches: []BranchResult{
			{Branch: "feat-a", PR: 10, Action: "failed", Reason: "conflict"},
		},
		Error: "conflict in feat-a — cannot resolve in --json mode",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var roundtrip SyncResult
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if roundtrip.Error == "" {
		t.Error("error should be present")
	}
	if roundtrip.Branches[0].Action != "failed" {
		t.Errorf("action: got %q, want %q", roundtrip.Branches[0].Action, "failed")
	}
}

func TestSplitConventionalTitle(t *testing.T) {
	tests := []struct {
		input      string
		wantPrefix string
		wantBody   string
		wantOK     bool
	}{
		{"fix(sync): preserve title prefix", "fix(sync): ", "preserve title prefix", true},
		{"feat: add feature", "feat: ", "add feature", true},
		{"chore(deps-dev): bump eslint", "chore(deps-dev): ", "bump eslint", true},
		{"ci: update workflow", "ci: ", "update workflow", true},
		{"plain title", "", "", false},
		{"no colon", "", "", false},
		{"Uppercase: not conventional", "", "", false},
		{"feat((nested): bad", "", "", false},
		{"feat(unclosed: bad", "", "", false},
		{"feat): unmatched close", "", "", false},
		{": empty type", "", "", false},
	}
	for _, tt := range tests {
		prefix, body, ok := splitConventionalTitle(tt.input)
		if ok != tt.wantOK {
			t.Errorf("splitConventionalTitle(%q): ok = %v, want %v", tt.input, ok, tt.wantOK)
			continue
		}
		if ok {
			if prefix != tt.wantPrefix {
				t.Errorf("splitConventionalTitle(%q): prefix = %q, want %q", tt.input, prefix, tt.wantPrefix)
			}
			if body != tt.wantBody {
				t.Errorf("splitConventionalTitle(%q): body = %q, want %q", tt.input, body, tt.wantBody)
			}
		}
	}
}

func TestDetectBaseDrift(t *testing.T) {
	dir := syncTestRepo(t)

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	preFFBaseTip := git("rev-parse", "main")

	// Advance main
	git("checkout", "main")
	os.WriteFile(filepath.Join(dir, "drift.txt"), []byte("drift\n"), 0644)
	git("add", "drift.txt")
	git("commit", "-m", "unrelated on main")
	newMainTip := git("rev-parse", "main")
	git("checkout", "branchC")

	// When base advanced, hint should be non-empty
	hint := detectBaseDrift(s, preFFBaseTip, newMainTip)
	if hint == "" {
		t.Fatal("expected base drift hint when main advanced, got empty string")
	}
	if !strings.Contains(hint, "main") {
		t.Error("hint should mention the base branch name")
	}
	if !strings.Contains(hint, "--full") {
		t.Error("hint should mention --full flag")
	}
	if !strings.Contains(hint, "1 new commit") {
		t.Error("hint should mention commit count")
	}

	// When tips are the same, no drift
	noHint := detectBaseDrift(s, preFFBaseTip, preFFBaseTip)
	if noHint != "" {
		t.Errorf("expected no hint when tips match, got %q", noHint)
	}
}

// --- Fix 1: update-tip ancestor check uses wrong SHA ---

// TestComputeSyncPlan_UpdateTipAncestorCheck verifies that computeSyncPlan
// correctly produces a "rebase" action (not "update-tip") when preFFBaseTip is
// an ancestor of the branch but currentParentTip (after fast-forward) is NOT.
//
// Scenario:
//  1. main@M1 → branchA[a1]. BaseTip = M1.
//  2. main advances to M2. branchA is manually rebased onto M2 (outside sdf).
//  3. main advances further to M3.
//  4. sdf sync with preFFBaseTip=M2, currentParentTip=M3.
//     - compareTip = M2 ≠ M1 (BaseTip) → enters check
//     - M2 IS ancestor of branchA (rebased onto it in step 2)
//     - M3 is NOT ancestor of branchA
//     - Bug: old code checks IsAncestor(M2, A) → true → update-tip → stores M3
//     - Fix: check IsAncestor(M3, A) → false → rebase
func TestComputeSyncPlan_UpdateTipAncestorCheck(t *testing.T) {
	dir := syncTestRepo(t)

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	s, err := stack.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Step 2: advance main to M2 and rebase branchA onto it
	git("checkout", "main")
	os.WriteFile(filepath.Join(dir, "m2.txt"), []byte("m2\n"), 0644)
	git("add", "m2.txt")
	git("commit", "-m", "advance main to M2")
	m2 := git("rev-parse", "HEAD")

	git("checkout", "branchA")
	git("rebase", "main")

	// Step 3: advance main further to M3
	git("checkout", "main")
	os.WriteFile(filepath.Join(dir, "m3.txt"), []byte("m3\n"), 0644)
	git("add", "m3.txt")
	git("commit", "-m", "advance main to M3")
	git("checkout", "branchC")

	// branchA.BaseTip is still M1 (from syncTestRepo), not updated after manual rebase.
	// preFFBaseTip = M2 (local main before ff to M3).
	opts := &syncOptions{
		fromHead:     false,
		preFFBaseTip: m2,
	}

	plan := computeSyncPlan(s, opts)

	rebases := filterActions(plan, "rebase")
	updateTips := filterActions(plan, "update-tip")

	// branchA should NOT get update-tip — the current main (M3) is not its ancestor.
	for _, a := range updateTips {
		if a.branch == "branchA" {
			t.Errorf("branchA should NOT get update-tip when currentParentTip (M3) is not its ancestor")
		}
	}

	// branchA needs a real rebase onto the current main (M3).
	if len(rebases) < 1 || rebases[0].branch != "branchA" {
		t.Errorf("expected rebase for branchA (M3 not ancestor), got rebases=%v updateTips=%v",
			actionBranches(rebases), actionBranches(updateTips))
	}
}

// TestRunSyncFrom_UpdateTipNoFalseCorruption verifies that runSyncFrom does
// not corrupt BaseTip by storing a SHA that is not an ancestor of the branch.
// Same scenario as TestComputeSyncPlan_UpdateTipAncestorCheck but exercises
// the execution path.
func TestRunSyncFrom_UpdateTipNoFalseCorruption(t *testing.T) {
	dir := syncTestRepoWithRemote(t)

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	s, err := stack.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Advance main to M2 and rebase branchA onto it (outside sdf).
	git("checkout", "main")
	os.WriteFile(filepath.Join(dir, "m2.txt"), []byte("m2\n"), 0644)
	git("add", "m2.txt")
	git("commit", "-m", "advance main to M2")
	m2 := git("rev-parse", "HEAD")

	git("checkout", "branchA")
	git("rebase", "main")

	// Advance main further to M3.
	git("checkout", "main")
	os.WriteFile(filepath.Join(dir, "m3.txt"), []byte("m3\n"), 0644)
	git("add", "m3.txt")
	git("commit", "-m", "advance main to M3")
	git("checkout", "branchC")

	bus, _ := testBus(t)
	defer bus.Finish()

	opts := syncOptions{
		fromHead:     false,
		preFFBaseTip: m2,
	}

	err = runSyncFrom(dir, s, 0, &opts, nil, bus)
	if err != nil {
		t.Fatalf("runSyncFrom failed: %v", err)
	}

	// After sync, every node's BaseTip must be an ancestor of its branch.
	// The bug: update-tip stored M3 (currentParentTip) which is NOT an ancestor.
	for _, node := range s.Nodes {
		if node.Status == "merged" {
			continue
		}
		if node.BaseTip == "" {
			continue
		}
		cmd := exec.Command("git", "merge-base", "--is-ancestor", node.BaseTip, node.Branch)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Errorf("node %s: BaseTip %s is NOT an ancestor of branch — corrupted",
				node.Branch, node.BaseTip[:8])
		}
	}
}

// --- Fix 2: rebased map cascade tracking in runSyncFrom ---

// TestRunSyncFrom_CascadePropagation verifies that when the base branch
// advances, ALL downstream branches are rebased (not just the first).
// After sync, each node's BaseTip must be an ancestor of its branch,
// and commit counts must be correct.
func TestRunSyncFrom_CascadePropagation(t *testing.T) {
	dir := syncTestRepoWithRemote(t)

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	// Advance main so all branches need rebasing.
	git("checkout", "main")
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0644)
	git("add", "new.txt")
	git("commit", "-m", "advance main")
	git("checkout", "branchC")

	s, err := stack.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	bus, _ := testBus(t)
	defer bus.Finish()

	opts := syncOptions{fromHead: true}
	err = runSyncFrom(dir, s, 0, &opts, nil, bus)
	if err != nil {
		t.Fatalf("runSyncFrom failed: %v", err)
	}

	// Verify all three branches were rebased: BaseTip is ancestor of branch.
	for _, node := range s.Nodes {
		cmd := exec.Command("git", "merge-base", "--is-ancestor", node.BaseTip, node.Branch)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Errorf("node %s: BaseTip %s is not ancestor of branch after sync",
				node.Branch, node.BaseTip[:8])
		}
	}

	// Verify no stale commits leaked: the total commit count from main to
	// the last branch should not increase after sync. Each branch adds 1 commit
	// (a1, b1, c1) = 3 commits in main..branchC.
	totalCount := git("rev-list", "--count", "main..branchC")
	if totalCount != "3" {
		t.Errorf("expected 3 commits in main..branchC after sync (a1+b1+c1), got %s", totalCount)
	}
}

// TestRunSyncFrom_ForceCascadeFromRebasedParent verifies that the rebased map
// forces cascade even when SHA comparison alone might not detect the need.
// Scenario: branchA is rebased by runSyncFrom. branchB's BaseTip already
// matches the OLD branchA tip (before rebase). Without the rebased map,
// the comparison `currentParentTip != BaseTip` catches it. But this test
// validates the cascade occurs and results are correct.
func TestRunSyncFrom_ForceCascadeFromRebasedParent(t *testing.T) {
	dir := syncTestRepoWithRemote(t)

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	// Advance main to trigger cascade.
	git("checkout", "main")
	os.WriteFile(filepath.Join(dir, "trigger.txt"), []byte("trigger\n"), 0644)
	git("add", "trigger.txt")
	git("commit", "-m", "trigger cascade")
	git("checkout", "branchC")

	s, err := stack.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Record pre-sync branch tips to verify they actually changed.
	preTips := make(map[string]string)
	for _, node := range s.Nodes {
		preTips[node.Branch] = git("rev-parse", node.Branch)
	}

	bus, _ := testBus(t)
	defer bus.Finish()

	opts := syncOptions{fromHead: true}
	result := &SyncResult{}
	err = runSyncFrom(dir, s, 0, &opts, result, bus)
	if err != nil {
		t.Fatalf("runSyncFrom failed: %v", err)
	}

	// ALL three branches should have been rebased (tips changed).
	for _, node := range s.Nodes {
		postTip := git("rev-parse", node.Branch)
		if postTip == preTips[node.Branch] {
			t.Errorf("node %s: tip did not change — branch was NOT rebased", node.Branch)
		}
	}

	// Verify via SyncResult that all branches show "rebased" action.
	rebasedCount := 0
	for _, br := range result.Branches {
		if br.Action == "rebased" {
			rebasedCount++
		}
	}
	if rebasedCount != 3 {
		t.Errorf("expected 3 rebased branches in result, got %d", rebasedCount)
		for _, br := range result.Branches {
			t.Logf("  %s: %s", br.Branch, br.Action)
		}
	}
}

// --- Fix 3: --continue passes cascade info ---

// TestRunSyncContinue_CascadesDownstream verifies that after manual conflict
// resolution, sdf sync --continue correctly cascades rebases to downstream
// branches. Simulates: sync paused on branchB after branchA was rebased, user
// resolves branchB manually, then --continue cascades to branchC.
func TestRunSyncContinue_CascadesDownstream(t *testing.T) {
	dir := syncTestRepoWithRemote(t)

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	// Load the stack while still on branchC (where .sdf/ was committed).
	s, err := stack.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Step 1: advance main so the stack needs rebasing.
	git("checkout", "main")
	os.WriteFile(filepath.Join(dir, "advance.txt"), []byte("advance\n"), 0644)
	git("add", "advance.txt")
	git("commit", "-m", "advance main")

	// Step 2: manually rebase branchA onto new main (simulating what sdf sync
	// would have done before hitting a conflict on branchB).
	git("checkout", "branchA")
	git("rebase", "main")
	newAParentTip := git("rev-parse", "main")

	// Step 3: manually rebase branchB onto branchA (simulating user resolving
	// the conflict that caused the pause).
	git("checkout", "branchB")
	git("rebase", "branchA")
	newBParentTip := git("rev-parse", "branchA")

	// Step 4: set up stack state as it would be after sync paused on branchB.
	// branchA's BaseTip was updated during sync (before branchB paused).
	s.Nodes[0].BaseTip = newAParentTip
	// branchB's BaseTip is NOT yet updated (sync paused before updating it).
	// branchC's BaseTip is still the old branchB tip.
	if err := stack.Save(dir, s); err != nil {
		t.Fatal(err)
	}

	// Record branchC's pre-continue tip to verify it changes.
	preCTip := git("rev-parse", "branchC")

	// Step 5: set up SyncProgress as if sync paused on branchB.
	local := &stack.LocalState{
		SyncProgress: &stack.SyncProgress{
			PausedAt:       "branchB",
			ResumeIndex:    1, // branchB's index
			OriginalBranch: "branchC",
			ParentTip:      newBParentTip,
		},
	}
	if err := stack.SaveLocal(dir, local); err != nil {
		t.Fatal(err)
	}

	git("checkout", "branchB") // runSyncContinue expects we're on the paused branch

	bus, buf := testBus(t)

	result := &SyncResult{}
	err = runSyncContinue(dir, result, bus)
	_ = bus.Finish()
	if err != nil {
		t.Fatalf("runSyncContinue failed: %v\noutput:\n%s", err, buf.String())
	}

	// branchC must have been cascade-rebased (tip changed).
	postCTip := git("rev-parse", "branchC")
	if postCTip == preCTip {
		t.Error("branchC was NOT cascade-rebased after --continue — tip unchanged")
	}

	// Reload stack and verify branchC's BaseTip is an ancestor.
	s, err = stack.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range s.Nodes {
		if node.Status == "merged" || node.BaseTip == "" {
			continue
		}
		cmd := exec.Command("git", "merge-base", "--is-ancestor", node.BaseTip, node.Branch)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Errorf("after --continue: node %s BaseTip %s not ancestor of branch",
				node.Branch, node.BaseTip[:8])
		}
	}
}

// --- Fix 4: warn on merge-base fallback ---

// TestRunSyncFrom_WarnOnMergeBaseFallback verifies that when a node's BaseTip
// is not an ancestor of its branch (corrupted state), sync emits a warning
// about using the merge-base fallback.
func TestRunSyncFrom_WarnOnMergeBaseFallback(t *testing.T) {
	dir := syncTestRepoWithRemote(t)

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	s, err := stack.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Advance main twice so we can corrupt BaseTip with a non-ancestor SHA
	// that differs from the current parent tip.
	git("checkout", "main")
	os.WriteFile(filepath.Join(dir, "m2.txt"), []byte("m2\n"), 0644)
	git("add", "m2.txt")
	git("commit", "-m", "advance main to M2")
	m2 := git("rev-parse", "HEAD")

	os.WriteFile(filepath.Join(dir, "m3.txt"), []byte("m3\n"), 0644)
	git("add", "m3.txt")
	git("commit", "-m", "advance main to M3")
	git("checkout", "branchC")

	// Corrupt branchA's BaseTip to M2, which is NOT an ancestor of branchA
	// but differs from currentParentTip (M3). This forces the rebase path
	// and triggers the merge-base fallback.
	s.Nodes[0].BaseTip = m2

	var outBuf, errBuf bytes.Buffer
	bus := render.NewBus(&outBuf, &errBuf, render.Options{})

	opts := syncOptions{fromHead: true}
	_ = runSyncFrom(dir, s, 0, &opts, nil, bus)
	_ = bus.Finish()

	// Warnings go to errw in the TTY renderer.
	allOutput := stripANSI(outBuf.String()) + stripANSI(errBuf.String())
	if !strings.Contains(allOutput, "not in its ancestry") {
		t.Errorf("expected warning about stale base tip fallback, got:\nstdout:\n%s\nstderr:\n%s",
			outBuf.String(), errBuf.String())
	}
}

// --- Issue #197: sync must push all rebased branches ---

// TestRunSyncFrom_PushesBranchesAfterMergedPRs reproduces the scenario from
// issue #197: merged PRs at the top of a stack cause downstream branches to be
// re-parented to main. After sync, every rebased branch must be pushed to the
// remote — not just have its BaseTip bookkeeping updated.
//
// The bug (fixed in #214) was that the update-tip fast path checked
// IsAncestor(preFFBaseTip, branch) — which was true because the old main was
// in the branch's ancestry — but then stored currentParentTip (new main) and
// skipped the rebase+push entirely.
func TestRunSyncFrom_PushesBranchesAfterMergedPRs(t *testing.T) {
	dir := syncTestRepoWithRemote(t)

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	s, err := stack.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Capture the pre-FF base tip (old main, before the "merge" advances it).
	preFFBaseTip := git("rev-parse", "main")

	// Simulate: branchA's PR was squash-merged into main via GitHub UI.
	// This advances main with a new commit (the squash merge result).
	git("checkout", "main")
	os.WriteFile(filepath.Join(dir, "merged-a.txt"), []byte("squash merge of branchA\n"), 0644)
	git("add", "merged-a.txt")
	git("commit", "-m", "squash merge branchA into main")
	git("checkout", "branchC")

	// Mark branchA as merged in the stack (reconciliation would do this).
	s.Nodes[0].Status = "merged"
	if err := stack.Save(dir, s); err != nil {
		t.Fatal(err)
	}

	// Push the updated main to the remote so the remote knows about the merge.
	git("push", "origin", "main")

	bus, _ := testBus(t)
	defer bus.Finish()

	// Run sync with fromHead=false (default mode, NOT --full).
	// preFFBaseTip is the old main (before the squash merge).
	// currentParentTip will be the new main (after the squash merge).
	opts := syncOptions{
		fromHead:     false,
		preFFBaseTip: preFFBaseTip,
	}

	err = runSyncFrom(dir, s, 0, &opts, nil, bus)
	if err != nil {
		t.Fatalf("runSyncFrom failed: %v", err)
	}

	// The critical assertion from #197: after sync, every open branch's local
	// tip must match its remote tip. If a branch is "ahead of origin", the
	// push was silently skipped.
	for _, node := range s.Nodes {
		if node.Status == "merged" {
			continue
		}

		localTip := git("rev-parse", node.Branch)
		remoteTip := git("rev-parse", "origin/"+node.Branch)

		if localTip != remoteTip {
			t.Errorf("node %s: local tip %s != remote tip %s — branch was NOT pushed (issue #197)",
				node.Branch, localTip[:8], remoteTip[:8])
		}
	}
}

func TestStripConventionalPrefix(t *testing.T) {
	got := stripConventionalPrefix("feat(auth): add login endpoint")
	if got != "add login endpoint" {
		t.Fatalf("unexpected stripped title: %q", got)
	}

	got = stripConventionalPrefix("add login endpoint")
	if got != "add login endpoint" {
		t.Fatalf("unexpected plain title result: %q", got)
	}
}
