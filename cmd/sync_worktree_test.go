// cmd/sync_worktree_test.go
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func commitInWorktree(t *testing.T, wt, file, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(wt, file), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", msg}} {
		cmd := exec.Command("git", append([]string{"-C", wt}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestWorktreeSyncRebasesOntoAdvancedParent(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatal(err)
	}
	s, _ := stack.LoadStack(root, "feat")
	wtA := s.FindNode("feat/a").WorktreePath
	wtB := s.FindNode("feat/b").WorktreePath

	// Advance feat/a with a new commit (its agent did work and committed).
	commitInWorktree(t, wtA, "a.txt", "from a\n", "a work")
	aTip, _ := gitpkg.RevParseAt(wtA, "HEAD")

	// feat/b's recorded BaseTip is now stale → run sync from feat/b's worktree.
	chdir(t, wtB) // chdir helper is in cmd/... ; if absent, inline os.Chdir
	if err := RunSync(nil); err != nil {
		t.Fatalf("worktree sync: %v", err)
	}

	// feat/b must now contain feat/a's commit, and BaseTip updated.
	s2, _ := stack.LoadStack(root, "feat")
	if got := s2.FindNode("feat/b").BaseTip; got != aTip {
		t.Errorf("feat/b BaseTip = %q, want feat/a tip %q", got, aTip)
	}
	if !gitpkg.IsAncestor(aTip, "feat/b") {
		t.Errorf("feat/a tip should be an ancestor of feat/b after rebase")
	}
}

func TestWorktreeSyncRejectsDirtyWorktree(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatal(err)
	}
	s, _ := stack.LoadStack(root, "feat")
	wtA := s.FindNode("feat/a").WorktreePath
	wtB := s.FindNode("feat/b").WorktreePath
	commitInWorktree(t, wtA, "a.txt", "from a\n", "a work")

	// Leave an uncommitted change in feat/b.
	os.WriteFile(filepath.Join(wtB, "f.txt"), []byte("dirty\n"), 0644)

	chdir(t, wtB)
	err := RunSync(nil)
	if err == nil {
		t.Fatalf("expected sync to refuse a dirty worktree")
	}
}
