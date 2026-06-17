// cmd/branch_worktree_test.go
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	"github.com/pavelpascari/sdf/internal/stack"
)

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
