package cmd

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
)

func gitRun(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
	}
	return strings.TrimSpace(string(out))
}

func TestPruneMissingNodes(t *testing.T) {
	newTestRepo(t)

	// Create one real branch and one missing branch in stack metadata.
	gitRun(t, "checkout", "-b", "feat/existing")
	gitRun(t, "checkout", "main")

	s := &stack.Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "feat/existing", Status: "open"},
			{Branch: "feat/missing", Status: "open"},
		},
	}

	removed := pruneMissingNodes(s)
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if len(s.Nodes) != 1 || s.Nodes[0].Branch != "feat/existing" {
		t.Fatalf("unexpected remaining nodes: %+v", s.Nodes)
	}
}

func TestShouldDeleteStack(t *testing.T) {
	if !shouldDeleteStack(&stack.Stack{Nodes: nil}) {
		t.Fatal("empty stack should be deletable")
	}
	if !shouldDeleteStack(&stack.Stack{Nodes: []stack.Node{{Status: "merged"}, {Status: "closed"}}}) {
		t.Fatal("fully merged stack should be deletable")
	}
	if shouldDeleteStack(&stack.Stack{Nodes: []stack.Node{{Status: "open"}, {Status: "merged"}}}) {
		t.Fatal("stack with open node should not be deletable")
	}
}

func TestPruneLocalState(t *testing.T) {
	newTestRepo(t)
	local := &stack.LocalState{
		SplitSessions: map[string]string{
			"keep":   "s1",
			"remove": "s2",
		},
		SyncProgress: &stack.SyncProgress{
			PausedAt: "missing-branch",
		},
	}
	keep := map[string]bool{"keep": true}

	changed := pruneLocalState(local, keep)
	if !changed {
		t.Fatal("expected local state to change")
	}
	if _, ok := local.SplitSessions["remove"]; ok {
		t.Fatal("expected stale split session to be removed")
	}
	if local.SyncProgress != nil {
		t.Fatal("expected stale sync progress to be removed")
	}
}
