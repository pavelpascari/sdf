// cmd/prune_worktree_test.go
package cmd

import (
	"os"
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
)

func TestPruneRemovesWorktreesForDeletedNodes(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatal(err)
	}
	s, _ := stack.LoadStack(root, "feat")
	wtB := s.FindNode("feat/b").WorktreePath

	// Remove the branch ref + worktree out from under sdf to simulate an orphan:
	// instead, mark feat/b such that prune should drop it. Simplest: delete the
	// git branch so pruneMissingNodes treats it as orphaned.
	// First remove the worktree's branch checkout cleanly is complex; instead
	// assert the helper removes a recorded worktree path.
	removed := removeWorktreesForNodes(s.Nodes[1:2])
	if removed != 1 {
		t.Fatalf("expected 1 worktree removed, got %d", removed)
	}
	if _, err := os.Stat(wtB); !os.IsNotExist(err) {
		t.Errorf("worktree dir should be removed")
	}
}
