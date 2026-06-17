// cmd/worktree_enable_test.go
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	"github.com/pavelpascari/sdf/internal/stack"
)

func TestWorktreeEnableMaterializesOpenNodes(t *testing.T) {
	root := bareRepoWithClone(t)
	// Build a normal (non-worktree) stack with two branches.
	if _, err := runNewCore("feat", "main", "feat/a", false, false); err != nil {
		t.Fatal(err)
	}
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatal(err)
	}

	if err := RunWorktree([]string{"enable", "--stack", "feat"}); err != nil {
		t.Fatalf("worktree enable: %v", err)
	}

	s, _ := stack.LoadStack(root, "feat")
	if !s.Worktree {
		t.Errorf("stack should be worktree-enabled")
	}
	cfg := cfgpkg.Defaults()
	for _, n := range s.Nodes {
		if n.WorktreePath != cfg.WorktreePathFor(root, n.Branch) {
			t.Errorf("node %s missing worktree path: %q", n.Branch, n.WorktreePath)
		}
		if _, err := os.Stat(filepath.Join(n.WorktreePath, "f.txt")); err != nil {
			t.Errorf("worktree for %s not materialized: %v", n.Branch, err)
		}
	}

	// Idempotent: re-running must not error.
	if err := RunWorktree([]string{"enable", "--stack", "feat"}); err != nil {
		t.Errorf("re-enable should be idempotent, got %v", err)
	}
}
