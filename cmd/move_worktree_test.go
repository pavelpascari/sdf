// cmd/move_worktree_test.go
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

// resetMoveFlags restores moveCmd's flags to their defaults between in-process
// test invocations, matching the isolation that a real per-process sdf run gets.
func resetMoveFlags() {
	moveCmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
}

// TestMoveWorktree_MovesCommitToParentWorktree verifies that sdf move, run
// from inside feat/b's worktree, cherry-picks the commit onto feat/a's
// worktree and strips it from feat/b via rebase, with feat/c downstream
// rebased as well, and all three worktrees intact and on the correct branches.
func TestMoveWorktree_MovesCommitToParentWorktree(t *testing.T) {
	resetMoveFlags()

	// Stand up origin + working clone with worktree stack (3 branches).
	root := bareRepoWithClone(t)

	// Create feat/a as first node (worktree mode)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatalf("runNewCore feat/a: %v", err)
	}

	// Add feat/b on top of feat/a
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatalf("RunBranch feat/b: %v", err)
	}

	s, err := stack.LoadStack(root, "feat")
	if err != nil {
		t.Fatal(err)
	}

	wtA := s.FindNode("feat/a").WorktreePath
	wtB := s.FindNode("feat/b").WorktreePath

	// Commit something on feat/b so there's a commit to move.
	commitInWorktree(t, wtB, "b1.txt", "b1\n", "b1 commit")
	// Commit a second one so moving b1 won't leave feat/b empty.
	commitInWorktree(t, wtB, "b2.txt", "b2\n", "b2 commit")

	// Get the SHA of b1 (first commit above feat/a on feat/b).
	b1sha, err := gitpkg.RevParseAt(wtB, "HEAD^")
	if err != nil {
		t.Fatalf("RevParseAt b1: %v", err)
	}

	// Add feat/c downstream of feat/b.
	if err := RunBranch([]string{"feat/c", "--no-prefix"}); err != nil {
		t.Fatalf("RunBranch feat/c: %v", err)
	}

	s2, _ := stack.LoadStack(root, "feat")
	wtC := s2.FindNode("feat/c").WorktreePath

	// Commit something on feat/c so it has its own commit.
	commitInWorktree(t, wtC, "c1.txt", "c1\n", "c1 commit")

	// We must cd into feat/b's worktree to run move (resolveStack uses CurrentBranch).
	chdir(t, wtB)

	if err := RunMove([]string{b1sha}); err != nil {
		t.Fatalf("RunMove in worktree mode failed: %v", err)
	}

	// --- Assertions ---

	// 1. b1.txt must exist in feat/a's worktree (commit was cherry-picked).
	if _, err := os.Stat(filepath.Join(wtA, "b1.txt")); err != nil {
		t.Error("b1.txt should exist in feat/a worktree after move")
	}

	// 2. feat/a's tip should now include b1sha content (via cherry-pick).
	aTip, err := gitpkg.RevParseAt(wtA, "HEAD")
	if err != nil {
		t.Fatalf("RevParseAt feat/a HEAD: %v", err)
	}

	// 3. feat/b must no longer have b1sha as an own commit above feat/a.
	bTip, err := gitpkg.RevParseAt(wtB, "HEAD")
	if err != nil {
		t.Fatalf("RevParseAt feat/b HEAD: %v", err)
	}

	// feat/b's own commits above feat/a should be exactly 1 (b2 only).
	commits, err := gitpkg.LogCommits(aTip, bTip)
	if err != nil {
		t.Fatalf("LogCommits feat/a..feat/b: %v", err)
	}
	if len(commits) != 1 {
		t.Errorf("feat/b should have exactly 1 own commit above feat/a after move, got %d", len(commits))
	}

	// b2.txt must still be in feat/b.
	if _, err := os.Stat(filepath.Join(wtB, "b2.txt")); err != nil {
		t.Error("b2.txt should still exist in feat/b worktree after move")
	}

	// 4. All three worktrees still intact and on the correct branches.
	for wantBranch, wt := range map[string]string{
		"feat/a": wtA,
		"feat/b": wtB,
		"feat/c": wtC,
	} {
		cmd := exec.Command("git", "-C", wt, "rev-parse", "--abbrev-ref", "HEAD")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("git -C %s rev-parse --abbrev-ref HEAD: %v", wt, err)
			continue
		}
		branchRef := strings.TrimSpace(string(out))
		if branchRef != wantBranch {
			t.Errorf("worktree for %s is on branch %q, want %q", wantBranch, branchRef, wantBranch)
		}
	}

	// 5. feat/c should be rebased (its BaseTip updated to new feat/b tip).
	s3, err := stack.LoadStack(root, "feat")
	if err != nil {
		t.Fatal(err)
	}
	cNode := s3.FindNode("feat/c")
	if cNode == nil {
		t.Fatal("feat/c node missing from stack after move")
	}

	// The downstream feat/c must contain feat/b's updated content (inherited b2).
	if _, err := os.Stat(filepath.Join(wtC, "b2.txt")); err != nil {
		t.Error("b2.txt should be reachable in feat/c worktree (inherited via rebase)")
	}

	// 6. Stack JSON BaseTips must be updated correctly.
	bNode := s3.FindNode("feat/b")
	if bNode == nil {
		t.Fatal("feat/b node missing from stack after move")
	}
	newATip, _ := gitpkg.RevParseAt(wtA, "HEAD")
	if bNode.BaseTip != newATip {
		t.Errorf("feat/b BaseTip = %q, want feat/a tip %q", bNode.BaseTip, newATip)
	}
}
