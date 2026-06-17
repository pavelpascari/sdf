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
	resetSyncFlags()
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
	// Worktree continues use WorktreeProgress, not the monolithic SyncProgress.
	// Verify the per-branch entry for feat/b is cleared after a successful continue.
	if local.WorktreeProgress != nil && local.WorktreeProgress["feat/b"] != nil {
		t.Errorf("WorktreeProgress[feat/b] should be cleared after continue")
	}
}

// TestWorktreeContinueFromSubdir is a regression test for the exact-cwd match bug:
// when `sdf sync --continue` is run from a SUBDIRECTORY of the worktree (as an
// agent often does after cd-ing into a sub-path), the old code compared
// prog.WorktreePath == os.Getwd() and failed because the cwd was deeper than the
// worktree root. The fix uses gitpkg.RepoRoot() instead, which always returns the
// worktree root regardless of the current subdirectory.
func TestWorktreeContinueFromSubdir(t *testing.T) {
	resetSyncFlags()
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

	// Trigger the conflict from wtB root (the normal path).
	chdir(t, wtB)
	if err := RunSync(nil); err == nil {
		t.Fatalf("expected a conflict on first sync")
	}
	if inProg, _ := gitpkg.IsRebaseInProgressAt(wtB); !inProg {
		t.Fatalf("expected paused rebase in feat/b worktree")
	}

	// Resolve manually.
	os.WriteFile(filepath.Join(wtB, "shared.txt"), []byte("resolved\n"), 0644)
	mustRun(t, wtB, "git", "add", "shared.txt")

	// Create a subdirectory inside wtB and cd into it — simulating an agent
	// that navigated deeper into the worktree during conflict resolution.
	subdir := filepath.Join(wtB, "sub")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	chdir(t, subdir)

	// This must succeed: --continue should find feat/b's WorktreeProgress by
	// matching the worktree root (via git rev-parse --show-toplevel), not the cwd.
	resetSyncFlags()
	if err := RunSync([]string{"--continue"}); err != nil {
		t.Fatalf("sync --continue from subdir: %v (expected to resume feat/b)", err)
	}

	s2, _ := stack.LoadStack(root, "feat")
	aTip, _ := gitpkg.RevParseAt(wtA, "HEAD")
	if s2.FindNode("feat/b").BaseTip != aTip {
		t.Errorf("feat/b BaseTip not updated after continue from subdir")
	}
	local, _ := stack.LoadLocal(root)
	if local.WorktreeProgress != nil && local.WorktreeProgress["feat/b"] != nil {
		t.Errorf("WorktreeProgress[feat/b] should be cleared after continue")
	}
}

// TestWorktreeContinueResumesOwnBranch verifies that when two worktrees both
// have paused rebases (feat/b and feat/c both conflict against the advanced
// feat/a), running `sdf sync --continue` inside feat/b's worktree resumes
// feat/b only and leaves feat/c's progress entry intact.
func TestWorktreeContinueResumesOwnBranch(t *testing.T) {
	resetSyncFlags()
	root := bareRepoWithClone(t)

	// Build stack: main ← feat/a ← feat/b ← feat/c (all in worktree mode).
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatal(err)
	}
	if err := RunBranch([]string{"feat/c", "--no-prefix"}); err != nil {
		t.Fatal(err)
	}

	s, _ := stack.LoadStack(root, "feat")
	wtA := s.FindNode("feat/a").WorktreePath
	wtB := s.FindNode("feat/b").WorktreePath
	wtC := s.FindNode("feat/c").WorktreePath

	// Advance feat/a with a commit that will conflict with B and C's commits.
	commitInWorktree(t, wtA, "shared.txt", "from A\n", "a edits shared")

	// Both feat/b and feat/c independently edit the same file (conflicting with A).
	commitInWorktree(t, wtB, "shared.txt", "from B\n", "b edits shared")
	commitInWorktree(t, wtC, "shared.txt", "from C\n", "c edits shared")

	// Run sync in feat/b's worktree → conflict → writes WorktreeProgress["feat/b"].
	resetSyncFlags()
	chdir(t, wtB)
	if err := RunSync(nil); err == nil {
		t.Fatalf("expected conflict syncing feat/b")
	}
	if inProg, _ := gitpkg.IsRebaseInProgressAt(wtB); !inProg {
		t.Fatalf("expected paused rebase in feat/b worktree after first sync")
	}

	// Run sync in feat/c's worktree → conflict → writes WorktreeProgress["feat/c"].
	resetSyncFlags()
	chdir(t, wtC)
	if err := RunSync(nil); err == nil {
		t.Fatalf("expected conflict syncing feat/c")
	}
	if inProg, _ := gitpkg.IsRebaseInProgressAt(wtC); !inProg {
		t.Fatalf("expected paused rebase in feat/c worktree after second sync")
	}

	// Verify both progress entries are present before continuing.
	localBefore, _ := stack.LoadLocal(root)
	if localBefore.WorktreeProgress["feat/b"] == nil {
		t.Fatalf("expected WorktreeProgress[feat/b] to be set")
	}
	if localBefore.WorktreeProgress["feat/c"] == nil {
		t.Fatalf("expected WorktreeProgress[feat/c] to be set")
	}

	// Capture feat/c's tip before continuing feat/b — it must NOT change.
	cTipBefore, _ := gitpkg.RevParseAt(wtC, "HEAD")

	// Resolve feat/b's conflict manually and continue from wtB.
	os.WriteFile(filepath.Join(wtB, "shared.txt"), []byte("resolved by B\n"), 0644)
	mustRun(t, wtB, "git", "add", "shared.txt")

	resetSyncFlags()
	chdir(t, wtB)
	if err := RunSync([]string{"--continue"}); err != nil {
		t.Fatalf("sync --continue in wtB: %v", err)
	}

	// feat/b's BaseTip must now equal feat/a's tip.
	s2, _ := stack.LoadStack(root, "feat")
	aTip, _ := gitpkg.RevParseAt(wtA, "HEAD")
	if got := s2.FindNode("feat/b").BaseTip; got != aTip {
		t.Errorf("feat/b BaseTip = %q, want feat/a tip %q", got, aTip)
	}

	// feat/c's progress must still be present (untouched by feat/b's continue).
	localAfter, _ := stack.LoadLocal(root)
	if localAfter.WorktreeProgress["feat/c"] == nil {
		t.Errorf("WorktreeProgress[feat/c] was incorrectly cleared when continuing feat/b")
	}

	// feat/b's progress must be gone.
	if localAfter.WorktreeProgress["feat/b"] != nil {
		t.Errorf("WorktreeProgress[feat/b] should be cleared after continue")
	}

	// feat/c's tip must be unchanged (we did not touch its worktree).
	cTipAfter, _ := gitpkg.RevParseAt(wtC, "HEAD")
	if cTipBefore != cTipAfter {
		t.Errorf("feat/c tip changed after continuing feat/b: was %s, now %s", cTipBefore, cTipAfter)
	}
}
