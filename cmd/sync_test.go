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

	// branchB's parent changed to main, but ancestry is already valid, so
	// sync should refresh BaseTip instead of rewriting history.
	if len(rebases) != 0 {
		t.Fatalf("expected no rebase for ancestry-correct branchB, got %d", len(rebases))
	}
	if len(updateTips) != 1 || updateTips[0].branch != "branchB" || updateTips[0].onto != "main" {
		t.Errorf("expected update-tip branchB onto main, got %+v", updateTips)
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
		"2-5 sentences",
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
