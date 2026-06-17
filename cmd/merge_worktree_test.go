// cmd/merge_worktree_test.go
package cmd

import (
	"os"
	"testing"

	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
)

func newTestBus() *render.Bus {
	return render.NewBus(os.Stdout, os.Stderr, render.Options{})
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
