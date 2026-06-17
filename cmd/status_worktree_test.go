// cmd/status_worktree_test.go
package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/spf13/pflag"

	"github.com/pavelpascari/sdf/internal/stack"
)

func resetStatusFlags() {
	statusCmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
}

func TestStatusJSONIncludesWorktreePath(t *testing.T) {
	resetStatusFlags()
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	s, err := stack.LoadStack(root, "feat")
	if err != nil {
		t.Fatal(err)
	}
	if s.FindNode("feat/a").WorktreePath == "" {
		t.Fatal("precondition: node should have a worktree path")
	}
	wantPath := s.FindNode("feat/a").WorktreePath
	chdir(t, root)
	resetStatusFlags()

	// Capture stdout to inspect JSON output.
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	runErr := RunStatus([]string{"--json"})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	if runErr != nil {
		t.Fatalf("status --json: %v", runErr)
	}

	var result StatusResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal status result: %v\nraw: %s", err, buf.String())
	}
	if len(result.Nodes) == 0 {
		t.Fatalf("expected at least one node in status result")
	}
	if result.Nodes[0].WorktreePath != wantPath {
		t.Errorf("Nodes[0].WorktreePath = %q, want %q", result.Nodes[0].WorktreePath, wantPath)
	}
}
