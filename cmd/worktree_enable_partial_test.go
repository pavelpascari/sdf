// cmd/worktree_enable_partial_test.go
package cmd

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	"github.com/pavelpascari/sdf/internal/stack"
)

// TestWorktreeEnablePartialFailurePersists verifies that when `sdf worktree enable`
// fails mid-loop (because a branch is already checked out in another worktree),
// the worktrees that WERE successfully created are recorded in the stack JSON —
// no orphaned worktrees, and a re-run is idempotent for the already-materialized nodes.
func TestWorktreeEnablePartialFailurePersists(t *testing.T) {
	root := bareRepoWithClone(t)

	// Build a non-worktree stack with two branches.
	if _, err := runNewCore("feat", "main", "feat/a", false, false); err != nil {
		t.Fatalf("runNewCore feat/a: %v", err)
	}
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatalf("RunBranch feat/b: %v", err)
	}

	// Pre-create a worktree for feat/b in a different path so that
	// `git worktree add <canonical-path> feat/b` will fail.
	cfg := cfgpkg.Defaults()
	blockedPath := cfg.WorktreePathFor(root, "feat/b")

	// First switch the main repo off feat/b so we can create a worktree for it.
	if out, err := exec.Command("git", "-C", root, "checkout", "main").CombinedOutput(); err != nil {
		t.Fatalf("checkout main: %v\n%s", err, out)
	}

	// Use a sibling path for the pre-created worktree — git won't allow
	// the same branch to be checked out in two worktrees simultaneously.
	otherPath := blockedPath + ".other"
	if err := os.MkdirAll(otherPath[:strings.LastIndex(otherPath, "/")], 0755); err != nil {
		t.Fatalf("mkdir for other worktree: %v", err)
	}
	out, err := exec.Command("git", "-C", root, "worktree", "add", otherPath, "feat/b").CombinedOutput()
	if err != nil {
		t.Fatalf("pre-create worktree for feat/b: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		exec.Command("git", "-C", root, "worktree", "remove", "--force", otherPath).Run()
	})

	// Run worktree enable — should fail because feat/b is already checked out.
	enableErr := RunWorktree([]string{"enable", "--stack", "feat"})
	if enableErr == nil {
		t.Fatal("expected an error when a branch is already checked out elsewhere, got nil")
	}
	// Error must mention the conflicting branch.
	if !strings.Contains(enableErr.Error(), "feat/b") {
		t.Errorf("error should name the conflicting branch 'feat/b', got: %v", enableErr)
	}

	// Stack JSON must be persisted with Worktree=true and feat/a's WorktreePath recorded.
	s, err := stack.LoadStack(root, "feat")
	if err != nil {
		t.Fatalf("LoadStack after partial failure: %v", err)
	}
	if !s.Worktree {
		t.Errorf("stack.Worktree should be true even after partial failure")
	}
	nodeA := s.FindNode("feat/a")
	if nodeA == nil {
		t.Fatal("node feat/a missing from stack")
	}
	wantA := cfg.WorktreePathFor(root, "feat/a")
	if nodeA.WorktreePath != wantA {
		t.Errorf("feat/a WorktreePath = %q, want %q (orphaned on partial failure)", nodeA.WorktreePath, wantA)
	}

	// feat/b should NOT have a WorktreePath recorded (its canonical add failed).
	nodeB := s.FindNode("feat/b")
	if nodeB == nil {
		t.Fatal("node feat/b missing from stack")
	}
	if nodeB.WorktreePath == cfg.WorktreePathFor(root, "feat/b") {
		t.Errorf("feat/b should not have canonical WorktreePath recorded — the add failed")
	}

	// Idempotency: re-running enable should not error for feat/a (already materialized),
	// but may still fail for feat/b (still checked out elsewhere). Either way,
	// what we care about is that feat/a is not re-added (which would fail git).
	_ = RunWorktree([]string{"enable", "--stack", "feat"})

	// feat/a's worktree must still exist on disk.
	if _, err := os.Stat(wantA); err != nil {
		t.Errorf("feat/a worktree should still exist on disk after re-run: %v", err)
	}

	// Verify no double-add: git worktree list --porcelain has one stanza for feat/a.
	wtOut, err := exec.Command("git", "-C", root, "worktree", "list", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("git worktree list: %v\n%s", err, wtOut)
	}
	// Each worktree stanza starts with "worktree <path>". Count stanzas for feat/a path.
	lineCount := 0
	for _, line := range strings.Split(string(wtOut), "\n") {
		if strings.HasPrefix(line, "worktree ") && strings.Contains(line, "feat/a") &&
			!strings.Contains(line, "feat/a.") {
			lineCount++
		}
	}
	if lineCount > 1 {
		t.Errorf("feat/a worktree stanza appears %d times (expected 1):\n%s", lineCount, wtOut)
	}
}
