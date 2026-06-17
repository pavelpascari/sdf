// cmd/doctor_worktree_test.go
package cmd

import (
	"os"
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
)

func TestCheckWorktreesFlagsMissingPath(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	s, _ := stack.LoadStack(root, "feat")
	// Corrupt: delete the worktree directory behind sdf's back.
	_ = os.RemoveAll(s.FindNode("feat/a").WorktreePath)

	problems := checkWorktrees(root)
	if len(problems) == 0 {
		t.Errorf("expected doctor to flag the missing worktree directory")
	}
}

func TestCheckWorktreesCleanWhenConsistent(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	if problems := checkWorktrees(root); len(problems) != 0 {
		t.Errorf("expected no problems for a consistent worktree stack, got %v", problems)
	}
}
