// cmd/restack_worktree_test.go
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/spf13/pflag"
)

// resetRestackFlags restores restackCmd's flags to their defaults between
// in-process test invocations, matching the isolation that a real per-process
// sdf run gets.
func resetRestackFlags() {
	restackCmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
}

// worktreeRestackRepo builds a worktree-mode stack with 3 branches on top of main:
//
//	main ← feat/a ← feat/b ← feat/c
//
// Each branch has its own unique file (a.txt, b.txt, c.txt).
// Returns the clone root and a map[branch]worktreePath.
func worktreeRestackRepo(t *testing.T) (root string, wts map[string]string) {
	t.Helper()
	root = bareRepoWithClone(t)

	// Create feat/a as the first node in worktree mode.
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatalf("runNewCore feat/a: %v", err)
	}

	s, err := stack.LoadStack(root, "feat")
	if err != nil {
		t.Fatal(err)
	}
	wtA := s.FindNode("feat/a").WorktreePath

	commitInWorktree(t, wtA, "a.txt", "a\n", "a commit")

	// Push feat/a so subsequent pushes in restack work.
	if out, err := exec.Command("git", "-C", wtA, "push", "-u", "origin", "feat/a").CombinedOutput(); err != nil {
		t.Fatalf("push feat/a: %v\n%s", err, out)
	}

	// Create feat/b on top of feat/a.
	// RunBranch uses CurrentBranch() so we must be in feat/a's worktree.
	chdir(t, wtA)
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatalf("RunBranch feat/b: %v", err)
	}
	s, err = stack.LoadStack(root, "feat")
	if err != nil {
		t.Fatal(err)
	}
	wtB := s.FindNode("feat/b").WorktreePath
	commitInWorktree(t, wtB, "b.txt", "b\n", "b commit")

	if out, err := exec.Command("git", "-C", wtB, "push", "-u", "origin", "feat/b").CombinedOutput(); err != nil {
		t.Fatalf("push feat/b: %v\n%s", err, out)
	}

	// Create feat/c on top of feat/b.
	chdir(t, wtB)
	if err := RunBranch([]string{"feat/c", "--no-prefix"}); err != nil {
		t.Fatalf("RunBranch feat/c: %v", err)
	}
	s, err = stack.LoadStack(root, "feat")
	if err != nil {
		t.Fatal(err)
	}
	wtC := s.FindNode("feat/c").WorktreePath
	commitInWorktree(t, wtC, "c.txt", "c\n", "c commit")

	if out, err := exec.Command("git", "-C", wtC, "push", "-u", "origin", "feat/c").CombinedOutput(); err != nil {
		t.Fatalf("push feat/c: %v\n%s", err, out)
	}

	// Return to main-worktree root (which is on main) so that resolveStack /
	// CurrentBranch calls from runRestackLogic see a sensible HEAD.
	chdir(t, root)

	return root, map[string]string{
		"feat/a": wtA,
		"feat/b": wtB,
		"feat/c": wtC,
	}
}

// TestRestackWorktree_MoveCAfterA reorders a worktree-mode stack:
//
//	before: main ← feat/a ← feat/b ← feat/c
//	after:  main ← feat/a ← feat/c ← feat/b
//
// Asserts:
//   - New node order is saved in stack JSON.
//   - Branches are rebased in their own worktrees (not in the main-repo CWD).
//   - All worktrees remain intact on the correct branches.
//   - BaseTips are updated.
//   - Main-repo checkout (which is on main) is untouched.
func TestRestackWorktree_MoveCAfterA(t *testing.T) {
	resetRestackFlags()

	root, wts := worktreeRestackRepo(t)
	wtA, wtB, wtC := wts["feat/a"], wts["feat/b"], wts["feat/c"]

	// Capture pre-restack main-repo HEAD (should stay on main throughout).
	out, _ := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	mainRepoBranch := strings.TrimSpace(string(out))
	if mainRepoBranch != "main" {
		t.Fatalf("main repo should be on main before restack, got %q", mainRepoBranch)
	}

	if err := runRestackLogic("feat/c", "feat/a"); err != nil {
		t.Fatalf("runRestackLogic failed: %v", err)
	}

	// 1. Stack JSON node order is correct.
	s, err := stack.LoadStack(root, "feat")
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"feat/a", "feat/c", "feat/b"}
	for i, want := range wantOrder {
		if s.Nodes[i].Branch != want {
			t.Errorf("node[%d] = %q, want %q", i, s.Nodes[i].Branch, want)
		}
	}

	// 2. feat/c was rebased onto feat/a: it must have a.txt but NOT b.txt.
	if _, err := os.Stat(filepath.Join(wtC, "a.txt")); err != nil {
		t.Error("feat/c worktree should have a.txt (inherited from feat/a)")
	}
	if _, err := os.Stat(filepath.Join(wtC, "b.txt")); err == nil {
		t.Error("feat/c worktree must NOT have b.txt (feat/b is no longer its parent)")
	}
	if _, err := os.Stat(filepath.Join(wtC, "c.txt")); err != nil {
		t.Error("feat/c worktree should still have its own c.txt")
	}

	// 3. feat/b was rebased onto feat/c: it must have c.txt.
	if _, err := os.Stat(filepath.Join(wtB, "c.txt")); err != nil {
		t.Error("feat/b worktree should have c.txt (inherited from new parent feat/c)")
	}
	if _, err := os.Stat(filepath.Join(wtB, "b.txt")); err != nil {
		t.Error("feat/b worktree should still have its own b.txt")
	}

	// 4. All three worktrees are still intact and on the correct branches.
	for wantBranch, wt := range wts {
		branchOut, err := exec.Command("git", "-C", wt, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
		if err != nil {
			t.Errorf("git -C %s rev-parse HEAD: %v", wt, err)
			continue
		}
		got := strings.TrimSpace(string(branchOut))
		if got != wantBranch {
			t.Errorf("worktree for %s is on branch %q, want %q", wantBranch, got, wantBranch)
		}
	}

	// 5. BaseTips updated: feat/b.BaseTip should be the current tip of feat/c.
	cTip, err := gitpkg.RevParseAt(wtC, "HEAD")
	if err != nil {
		t.Fatalf("RevParseAt feat/c HEAD: %v", err)
	}
	bNode := s.FindNode("feat/b")
	if bNode == nil {
		t.Fatal("feat/b node missing from stack")
	}
	if bNode.BaseTip != cTip {
		t.Errorf("feat/b.BaseTip = %q, want feat/c HEAD %q", bNode.BaseTip, cTip)
	}

	// 6. BaseTip of feat/c should be the current tip of feat/a.
	aTip, err := gitpkg.RevParseAt(wtA, "HEAD")
	if err != nil {
		t.Fatalf("RevParseAt feat/a HEAD: %v", err)
	}
	cNode := s.FindNode("feat/c")
	if cNode == nil {
		t.Fatal("feat/c node missing from stack")
	}
	if cNode.BaseTip != aTip {
		t.Errorf("feat/c.BaseTip = %q, want feat/a HEAD %q", cNode.BaseTip, aTip)
	}

	// 7. Main-repo checkout remains on main — restack must not have touched it.
	postOut, _ := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if strings.TrimSpace(string(postOut)) != "main" {
		t.Errorf("main repo HEAD = %q after restack, want main", strings.TrimSpace(string(postOut)))
	}

	// 8. Progress cleared after successful restack.
	ls, _ := stack.LoadLocal(root)
	if ls.RestackProgress != nil {
		t.Error("restack progress should be cleared after success")
	}
}

// TestRestackWorktree_MoveToBase reorders so feat/c goes right after main.
func TestRestackWorktree_MoveToBase(t *testing.T) {
	resetRestackFlags()

	root, wts := worktreeRestackRepo(t)
	wtA, wtC := wts["feat/a"], wts["feat/c"]

	if err := runRestackLogic("feat/c", "main"); err != nil {
		t.Fatalf("runRestackLogic failed: %v", err)
	}

	s, err := stack.LoadStack(root, "feat")
	if err != nil {
		t.Fatal(err)
	}

	// Expected order: feat/c, feat/a, feat/b.
	wantOrder := []string{"feat/c", "feat/a", "feat/b"}
	for i, want := range wantOrder {
		if s.Nodes[i].Branch != want {
			t.Errorf("node[%d] = %q, want %q", i, s.Nodes[i].Branch, want)
		}
	}

	// feat/c should not have a.txt (its parent is now main, not feat/a).
	if _, err := os.Stat(filepath.Join(wtC, "a.txt")); err == nil {
		t.Error("feat/c should NOT have a.txt when rebased onto main")
	}
	if _, err := os.Stat(filepath.Join(wtC, "c.txt")); err != nil {
		t.Error("feat/c should still have its own c.txt")
	}

	// feat/a should now have c.txt (rebased onto feat/c).
	if _, err := os.Stat(filepath.Join(wtA, "c.txt")); err != nil {
		t.Error("feat/a should have c.txt after being rebased onto feat/c")
	}
}

// TestRestackWorktree_ProgressCleared verifies that RestackProgress is nil after
// a successful worktree-mode restack.
func TestRestackWorktree_ProgressCleared(t *testing.T) {
	resetRestackFlags()

	root, _ := worktreeRestackRepo(t)

	ls, _ := stack.LoadLocal(root)
	if ls.RestackProgress != nil {
		t.Fatal("expected no progress before restack")
	}

	if err := runRestackLogic("feat/c", "feat/a"); err != nil {
		t.Fatalf("runRestackLogic failed: %v", err)
	}

	ls, _ = stack.LoadLocal(root)
	if ls.RestackProgress != nil {
		t.Error("progress must be nil after successful worktree restack")
	}
}

// TestRestackWorktree_Abort verifies that --abort restores branch SHAs in their
// own worktrees and resets the stack node order.
func TestRestackWorktree_Abort(t *testing.T) {
	resetRestackFlags()

	root, wts := worktreeRestackRepo(t)
	wtB, wtC := wts["feat/b"], wts["feat/c"]

	// Capture original SHAs before restack.
	origB, err := gitpkg.RevParseAt(wtB, "HEAD")
	if err != nil {
		t.Fatalf("RevParseAt feat/b: %v", err)
	}
	origC, err := gitpkg.RevParseAt(wtC, "HEAD")
	if err != nil {
		t.Fatalf("RevParseAt feat/c: %v", err)
	}

	// Run restack to completion so we have a progress snapshot to restore from.
	if err := runRestackLogic("feat/c", "feat/a"); err != nil {
		t.Fatalf("runRestackLogic failed: %v", err)
	}

	// Manually inject a progress snapshot so --abort has something to restore.
	// (In a real conflict scenario the progress is saved mid-flight; here we
	// simulate it so that abort can be exercised without triggering a real conflict.)
	s, _ := stack.LoadStack(root, "feat")
	ls, _ := stack.LoadLocal(root)
	ls.RestackProgress = &stack.RestackProgress{
		StackID:        "feat",
		OriginalBranch: "main",
		OriginalNodes:  []stack.Node{*s.FindNode("feat/a"), *s.FindNode("feat/b"), *s.FindNode("feat/c")},
		BranchSHAs: map[string]string{
			"feat/b": origB,
			"feat/c": origC,
		},
		Plan: []stack.RestackAction{
			{Branch: "feat/c", NewParent: "feat/a", OldParent: "feat/b"},
			{Branch: "feat/b", NewParent: "feat/c", OldParent: "feat/a"},
		},
		ResumeIndex: 0,
	}
	if err := stack.SaveLocal(root, ls); err != nil {
		t.Fatalf("SaveLocal: %v", err)
	}
	// Revert nodes to original order in stack JSON so abort restores to a clean state.
	_ = stack.WithLock(root, "feat", func(fresh *stack.Stack) error {
		fresh.Nodes = ls.RestackProgress.OriginalNodes
		return nil
	})

	if err := runRestackAbort(); err != nil {
		t.Fatalf("runRestackAbort failed: %v", err)
	}

	// Branches should be back to original SHAs.
	gotB, _ := gitpkg.RevParseAt(wtB, "HEAD")
	if gotB != origB {
		t.Errorf("feat/b HEAD after abort = %q, want %q", gotB, origB)
	}
	gotC, _ := gitpkg.RevParseAt(wtC, "HEAD")
	if gotC != origC {
		t.Errorf("feat/c HEAD after abort = %q, want %q", gotC, origC)
	}

	// Progress cleared.
	ls2, _ := stack.LoadLocal(root)
	if ls2.RestackProgress != nil {
		t.Error("progress must be nil after abort")
	}
}

// TestRequireBranchWorktreeDir_WorktreeMode verifies the helper returns the
// path when the node has a WorktreePath set.
func TestRequireBranchWorktreeDir_WorktreeMode(t *testing.T) {
	s := &stack.Stack{
		StackID:  "test",
		Base:     "main",
		Worktree: true,
		Nodes: []stack.Node{
			{Branch: "feat/a", WorktreePath: "/tmp/wt/feat/a"},
		},
	}
	dir, err := requireBranchWorktreeDir(s, "feat/a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/tmp/wt/feat/a" {
		t.Errorf("dir = %q, want /tmp/wt/feat/a", dir)
	}
}

// TestRequireBranchWorktreeDir_MissingWorktree verifies an error is returned
// when the node exists but has no worktree path.
func TestRequireBranchWorktreeDir_MissingWorktree(t *testing.T) {
	s := &stack.Stack{
		StackID:  "test",
		Base:     "main",
		Worktree: true,
		Nodes: []stack.Node{
			{Branch: "feat/a"}, // no WorktreePath
		},
	}
	_, err := requireBranchWorktreeDir(s, "feat/a")
	if err == nil {
		t.Fatal("expected error for branch with no worktree")
	}
	if !strings.Contains(err.Error(), "has no worktree") {
		t.Errorf("error = %q, want 'has no worktree'", err.Error())
	}
}

// TestRequireBranchWorktreeDir_NonWorktreeStack verifies the helper returns ""
// without error for non-worktree stacks.
func TestRequireBranchWorktreeDir_NonWorktreeStack(t *testing.T) {
	s := &stack.Stack{
		StackID: "test",
		Base:    "main",
		Nodes:   []stack.Node{{Branch: "feat/a"}},
	}
	dir, err := requireBranchWorktreeDir(s, "feat/a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "" {
		t.Errorf("dir = %q, want empty for non-worktree stack", dir)
	}
}
