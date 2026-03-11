package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
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

func TestPruneContextDirs(t *testing.T) {
	dir := newTestRepo(t)

	// Create context directories: one matching a kept stack, one orphaned.
	contextDir := filepath.Join(dir, ".sdf", "context")
	os.MkdirAll(filepath.Join(contextDir, "active-stack"), 0755)
	os.WriteFile(filepath.Join(contextDir, "active-stack", "notes.md"), []byte("keep"), 0644)
	os.MkdirAll(filepath.Join(contextDir, "old-stack"), 0755)
	os.WriteFile(filepath.Join(contextDir, "old-stack", "notes.md"), []byte("remove"), 0644)

	keep := map[string]bool{"active-stack": true}

	// Dry-run: should report but not delete.
	result := PruneResult{}
	pruneContextDirs(dir, keep, false, &result)
	if len(result.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d: %v", len(result.Actions), result.Actions)
	}
	if _, err := os.Stat(filepath.Join(contextDir, "old-stack")); err != nil {
		t.Fatal("dry-run should not delete directory")
	}

	// Apply: should delete orphaned directory.
	result = PruneResult{}
	pruneContextDirs(dir, keep, true, &result)
	if len(result.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(result.Actions))
	}
	if _, err := os.Stat(filepath.Join(contextDir, "old-stack")); !os.IsNotExist(err) {
		t.Fatal("apply should delete orphaned context directory")
	}
	if _, err := os.Stat(filepath.Join(contextDir, "active-stack", "notes.md")); err != nil {
		t.Fatal("kept stack context should still exist")
	}
}
