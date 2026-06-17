// cmd/merge_worktree_test.go
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
)

func newTestBus() *render.Bus {
	return render.NewBus(os.Stdout, os.Stderr, render.Options{})
}

// TestPostMergeWorktreeCleanupRetainsUntrackedWorktree verifies that
// cleanupMergedWorktree does NOT call git worktree remove (which would exit 128)
// when the worktree contains untracked files and force=false. Instead it should
// retain the WorktreePath with a warning.
func TestPostMergeWorktreeCleanupRetainsUntrackedWorktree(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	s, _ := stack.LoadStack(root, "feat")
	nodeA := s.FindNode("feat/a")
	wtA := nodeA.WorktreePath

	// Drop an untracked file — this is exactly the scenario that caused exit 128.
	if err := os.WriteFile(filepath.Join(wtA, "scratch.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate the head PR having merged.
	nodeA.Status = "merged"

	bus := newTestBus()
	cleanupMergedWorktree(root, s, nodeA, false, bus)

	// The worktree directory must still exist (not removed).
	if _, err := os.Stat(wtA); err != nil {
		t.Errorf("worktree with untracked files should be retained, got err: %v", err)
	}
	// WorktreePath must be preserved so re-add won't collide with a cleared path.
	if nodeA.WorktreePath == "" {
		t.Errorf("WorktreePath should be retained when worktree is not removable")
	}
}

func TestPostMergeWorktreeCleanupRemovesWorktree(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatal(err)
	}
	s, _ := stack.LoadStack(root, "feat")
	nodeA := s.FindNode("feat/a")
	wtA := nodeA.WorktreePath

	// Simulate the head PR (feat/a) having merged.
	nodeA.Status = "merged"

	bus := newTestBus()
	cleanupMergedWorktree(root, s, nodeA, false, bus)

	if _, err := os.Stat(wtA); !os.IsNotExist(err) {
		t.Errorf("merged worktree should be removed")
	}
	if nodeA.WorktreePath != "" {
		t.Errorf("WorktreePath should be cleared after removal")
	}
}
