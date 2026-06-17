// cmd/branch_worktree_test.go
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

// The concurrent-append contract (WithLock serializes concurrent stack writes)
// is tested directly at the stack layer in
// internal/stack/TestWithLockSerializesConcurrentAppends. That test is
// genuinely concurrent (20 goroutines, no serializing mutex) and is the
// load-bearing guard for the race that branch.go relies on.

func TestBranchWorktreeModeCreatesWorktree(t *testing.T) {
	root := bareRepoWithClone(t) // from new_worktree_test.go (same package)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatalf("new: %v", err)
	}

	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatalf("branch: %v", err)
	}

	s, _ := stack.LoadStack(root, "feat")
	nb := s.FindNode("feat/b")
	if nb == nil || nb.WorktreePath == "" {
		t.Fatalf("feat/b should have a worktree path, got %+v", s.Nodes)
	}
	want := cfgpkg.Defaults().WorktreePathFor(root, "feat/b")
	if nb.WorktreePath != want {
		t.Errorf("WorktreePath = %q, want %q", nb.WorktreePath, want)
	}
	if _, err := os.Stat(filepath.Join(want, "f.txt")); err != nil {
		t.Errorf("worktree checkout missing: %v", err)
	}
	// Main repo HEAD untouched.
	out, _ := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if string(out) != "main\n" {
		t.Errorf("main repo HEAD changed to %q", out)
	}
}

func TestBranchIsIdempotent(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatal(err)
	}
	// Re-running the same branch must not error and must not duplicate the node.
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatalf("re-add must be idempotent, got %v", err)
	}
	s, _ := stack.LoadStack(root, "feat")
	count := 0
	for _, n := range s.Nodes {
		if n.Branch == "feat/b" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("feat/b appears %d times, want 1", count)
	}
}

// TestBranchWorktree_GitBranchExistsNodeAbsent exercises the concurrent LOSER
// path in worktree mode: the git branch already exists but the stack node does
// not. addWorktreeForNode will fail because the branch exists; the fix probes
// BranchExists and falls through to WithLock which inserts the node cleanly.
func TestBranchWorktree_GitBranchExistsNodeAbsent(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatalf("new: %v", err)
	}

	// Create the git branch directly without creating a worktree or stack node,
	// simulating the winner's git-side work before it persisted the node.
	if err := exec.Command("git", "-C", root, "branch", "feat/b", "main").Run(); err != nil {
		t.Fatalf("pre-create git branch feat/b: %v", err)
	}

	// Stack JSON must have NO node for feat/b.
	s0, err := stack.LoadStack(root, "feat")
	if err != nil {
		t.Fatal(err)
	}
	if s0.FindNode("feat/b") != nil {
		t.Fatal("test setup error: feat/b node must not exist yet")
	}

	// RunBranch must not hard-error; it must fall through and insert the node.
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatalf("RunBranch must succeed when git branch exists but node absent (worktree mode), got: %v", err)
	}

	s, err := stack.LoadStack(root, "feat")
	if err != nil {
		t.Fatal(err)
	}
	node := s.FindNode("feat/b")
	if node == nil {
		t.Fatal("feat/b node not found after RunBranch")
	}
	count := 0
	for _, n := range s.Nodes {
		if n.Branch == "feat/b" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("feat/b appears %d times, want 1", count)
	}
	// In worktree mode the node must carry a worktree path.
	cfg := cfgpkg.Defaults()
	want := cfg.WorktreePathFor(root, "feat/b")
	if node.WorktreePath != want {
		t.Errorf("WorktreePath = %q, want %q", node.WorktreePath, want)
	}
	// The git branch must still exist.
	if !gitpkg.BranchExists("feat/b") {
		t.Error("git branch feat/b should still exist")
	}
}
