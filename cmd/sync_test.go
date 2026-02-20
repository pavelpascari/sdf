package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
)

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
	os.MkdirAll(filepath.Join(dir, ".sdf", "context"), 0755)
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

	// Should have: skip branchA (merged), rebase branchB onto main, push branchB
	skips := filterActions(plan, "skip-merged")
	rebases := filterActions(plan, "rebase")
	pushes := filterActions(plan, "push")

	if len(skips) != 1 || skips[0].branch != "branchA" {
		t.Errorf("expected 1 skip-merged for branchA, got %v", skips)
	}

	// branchB should be rebased onto main (since branchA merged)
	// branchC should cascade (since branchB was rebased)
	if len(rebases) < 1 {
		t.Fatal("expected at least 1 rebase action")
	}
	if rebases[0].branch != "branchB" || rebases[0].onto != "main" {
		t.Errorf("expected rebase branchB onto main, got rebase %s onto %s",
			rebases[0].branch, rebases[0].onto)
	}

	// branchB should be pushed
	if len(pushes) < 1 || pushes[0].branch != "branchB" {
		t.Errorf("expected push for branchB, got %v", pushes)
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
	pushes := filterActions(plan, "push")

	if len(skips) != 1 {
		t.Errorf("expected 1 skip-merged, got %d", len(skips))
	}

	// Both branchB and branchC should be rebased
	rebaseBranches := actionBranches(rebases)
	if len(rebases) != 2 {
		t.Fatalf("expected 2 rebases (branchB + branchC cascade), got %d: %v",
			len(rebases), rebaseBranches)
	}
	if rebases[0].branch != "branchB" || rebases[0].onto != "main" {
		t.Errorf("expected first rebase: branchB onto main, got %s onto %s",
			rebases[0].branch, rebases[0].onto)
	}
	if rebases[1].branch != "branchC" || rebases[1].onto != "branchB" {
		t.Errorf("expected second rebase: branchC onto branchB, got %s onto %s",
			rebases[1].branch, rebases[1].onto)
	}

	// Both should be pushed
	pushBranches := actionBranches(pushes)
	if len(pushes) != 2 {
		t.Fatalf("expected 2 pushes (branchB + branchC), got %d: %v",
			len(pushes), pushBranches)
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
	pushes := filterActions(plan, "push")

	if len(skips) != 2 {
		t.Errorf("expected 2 skip-merged, got %d", len(skips))
	}

	// branchC should be rebased onto main (both A and B are merged, skipped)
	if len(rebases) < 1 {
		t.Fatal("expected at least 1 rebase")
	}
	if rebases[0].branch != "branchC" || rebases[0].onto != "main" {
		t.Errorf("expected rebase branchC onto main, got %s onto %s",
			rebases[0].branch, rebases[0].onto)
	}

	if len(pushes) < 1 {
		t.Error("expected at least 1 push action")
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

	if len(skips) != 1 || skips[0].branch != "branchB" {
		t.Errorf("expected skip-merged for branchB, got %v", skips)
	}

	// branchC should be rebased onto branchA (skipping merged branchB)
	if len(rebases) < 1 {
		t.Fatal("expected at least 1 rebase")
	}
	if rebases[0].branch != "branchC" || rebases[0].onto != "branchA" {
		t.Errorf("expected rebase branchC onto branchA, got %s onto %s",
			rebases[0].branch, rebases[0].onto)
	}
}

// --- printSyncPlan tests ---

func TestPrintSyncPlan_Output(t *testing.T) {
	plan := []syncAction{
		{kind: "skip-merged", branch: "feat/auth"},
		{kind: "rebase", branch: "feat/api", onto: "main"},
		{kind: "push", branch: "feat/api"},
		{kind: "update-pr-base", branch: "feat/api", pr: 42, onto: "main"},
		{kind: "update-title", branch: "feat/api", pr: 42, title: "feat: add API"},
		{kind: "update-description", branch: "feat/api", pr: 42},
	}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printSyncPlan(plan)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify each action type appears in the output
	checks := []struct {
		label    string
		contains string
	}{
		{"header", "Sync plan:"},
		{"merged", "feat/auth is merged"},
		{"rebase", "rebase feat/api onto main"},
		{"push", "force-push feat/api"},
		{"pr-base", "update PR #42 base to main"},
		{"title", `update PR #42 title: "feat: add API"`},
		{"description", "update PR #42 description via Claude"},
	}

	for _, c := range checks {
		if !strings.Contains(output, c.contains) {
			t.Errorf("printSyncPlan output missing %s: expected to contain %q\ngot:\n%s",
				c.label, c.contains, output)
		}
	}
}

// --- computeSyncPlan with update options ---

func TestComputeSyncPlan_WithUpdateTitles(t *testing.T) {
	syncTestRepo(t)

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Give branchA and branchB PRs
	s.Nodes[0].PR = 10
	s.Nodes[1].PR = 11

	opts := &syncOptions{
		updateTitles:       true,
		updateDescriptions: false,
		cfg:                cfgpkg.Defaults(),
	}

	plan := computeSyncPlan(s, opts)

	titleActions := filterActions(plan, "update-title")
	descActions := filterActions(plan, "update-description")

	// Should have title updates for open PRs (branchA and branchB have PRs)
	if len(titleActions) < 2 {
		t.Errorf("expected at least 2 update-title actions, got %d", len(titleActions))
	}
	for _, a := range titleActions {
		if a.title == "" {
			t.Errorf("update-title action for PR #%d has empty title", a.pr)
		}
	}

	// Should have no description updates (not enabled)
	if len(descActions) != 0 {
		t.Errorf("expected 0 update-description actions, got %d", len(descActions))
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
		updateTitles: true,
		cfg:          cfgpkg.Defaults(),
	}

	plan := computeSyncPlan(s, opts)

	titleActions := filterActions(plan, "update-title")

	// Should NOT have title update for merged branchA
	for _, a := range titleActions {
		if a.pr == 10 {
			t.Error("should not update title for merged PR #10")
		}
	}

	// Should have title updates for open PRs (branchB #11 and branchC #12)
	if len(titleActions) < 2 {
		t.Errorf("expected at least 2 update-title actions for open PRs, got %d", len(titleActions))
	}
}

// --- buildDescriptionPrompt tests ---

func TestBuildDescriptionPrompt(t *testing.T) {
	subjects := []string{"feat: add user auth", "fix: handle edge case"}
	diffStat := " auth.go | 50 +++++\n login.go | 20 +++\n"

	prompt := buildDescriptionPrompt("feat/auth", subjects, diffStat)

	checks := []string{
		"Branch: feat/auth",
		"feat: add user auth",
		"fix: handle edge case",
		"auth.go",
		"concise description",
	}

	for _, c := range checks {
		if !strings.Contains(prompt, c) {
			t.Errorf("prompt missing %q\ngot:\n%s", c, prompt)
		}
	}
}

func TestBuildDescriptionPrompt_NoDiff(t *testing.T) {
	subjects := []string{"initial commit"}
	prompt := buildDescriptionPrompt("feat/init", subjects, "")

	if strings.Contains(prompt, "Change summary") {
		t.Error("prompt should not contain Change summary when diffStat is empty")
	}
	if !strings.Contains(prompt, "initial commit") {
		t.Error("prompt should contain commit subject")
	}
}

// --- confirmSync tests ---

func TestConfirmSync_Accepts(t *testing.T) {
	inputs := []struct {
		input string
		want  bool
	}{
		{"\n", true},       // Enter (default yes)
		{"y\n", true},      // lowercase y
		{"Y\n", true},      // uppercase Y
		{"yes\n", true},    // full word
		{"YES\n", true},    // uppercase full word
		{"n\n", false},     // no
		{"no\n", false},    // no full word
		{"N\n", false},     // uppercase N
		{"abort\n", false}, // anything else
	}

	for _, tc := range inputs {
		t.Run(fmt.Sprintf("input=%q", strings.TrimSpace(tc.input)), func(t *testing.T) {
			// Replace stdin with a pipe
			oldStdin := os.Stdin
			r, w, _ := os.Pipe()
			os.Stdin = r

			// Capture stdout to suppress the "Proceed?" prompt
			oldStdout := os.Stdout
			_, devNull, _ := os.Pipe()
			os.Stdout = devNull

			w.WriteString(tc.input)
			w.Close()

			got := confirmSync()

			os.Stdin = oldStdin
			os.Stdout = oldStdout
			devNull.Close()

			if got != tc.want {
				t.Errorf("confirmSync() with input %q = %v, want %v",
					strings.TrimSpace(tc.input), got, tc.want)
			}
		})
	}
}
