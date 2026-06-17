// cmd/sync_continue_worktree_test.go
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

func TestWorktreeSyncContinueAfterManualResolve(t *testing.T) {
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

	// Create a conflicting edit to the same file on both branches.
	commitInWorktree(t, wtA, "shared.txt", "from A\n", "a edits shared")
	commitInWorktree(t, wtB, "shared.txt", "from B\n", "b edits shared")

	chdir(t, wtB)
	if err := RunSync(nil); err == nil {
		t.Fatalf("expected a conflict on first sync")
	}
	// Sanity: rebase is paused in wtB.
	if inProg, _ := gitpkg.IsRebaseInProgressAt(wtB); !inProg {
		t.Fatalf("expected paused rebase in feat/b worktree")
	}

	// Resolve manually: take B's content, stage it.
	os.WriteFile(filepath.Join(wtB, "shared.txt"), []byte("resolved\n"), 0644)
	cmd := exec.Command("git", "-C", wtB, "add", "shared.txt")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if err := RunSync([]string{"--continue"}); err != nil {
		t.Fatalf("sync --continue: %v", err)
	}

	s2, _ := stack.LoadStack(root, "feat")
	aTip, _ := gitpkg.RevParseAt(wtA, "HEAD")
	if s2.FindNode("feat/b").BaseTip != aTip {
		t.Errorf("feat/b BaseTip not updated after continue")
	}
	local, _ := stack.LoadLocal(root)
	if local.SyncProgress != nil {
		t.Errorf("SyncProgress should be cleared after continue")
	}
}
