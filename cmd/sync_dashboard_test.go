// cmd/sync_dashboard_test.go
package cmd

import (
	"os"
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
)

func TestDashboardReportsReadinessWithoutRebasing(t *testing.T) {
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
	commitInWorktree(t, wtA, "a.txt", "x\n", "a work")
	bTipBefore := s.FindNode("feat/b").BaseTip

	// Run from the main repo (on main) — must not rebase anything.
	chdir(t, root)
	if err := RunSync(nil); err != nil {
		t.Fatalf("dashboard sync: %v", err)
	}

	s2, _ := stack.LoadStack(root, "feat")
	if s2.FindNode("feat/b").BaseTip != bTipBefore {
		t.Errorf("dashboard must not modify BaseTip; got %q want %q",
			s2.FindNode("feat/b").BaseTip, bTipBefore)
	}
	_ = os.Stdout
}
