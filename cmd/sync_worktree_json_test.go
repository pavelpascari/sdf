// cmd/sync_worktree_json_test.go
package cmd

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
)

// captureSyncJSON runs `sdf sync --json`, parses SyncResult, returns branches[0].
func captureSyncJSON(t *testing.T) BranchResult {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	_ = RunSync([]string{"--json"})
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	var sr SyncResult
	if err := json.Unmarshal(out, &sr); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	if len(sr.Branches) == 0 {
		t.Fatalf("no branches in result: %s", out)
	}
	return sr.Branches[0]
}

func TestWorktreeSyncJSONCleanAndNoop(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatal(err)
	}
	s, _ := stack.LoadStack(root, "feat")
	commitInWorktree(t, s.FindNode("feat/a").WorktreePath, "a.txt", "x\n", "a")
	chdir(t, s.FindNode("feat/b").WorktreePath)

	// First sync: clean rebase.
	resetSyncFlags()
	br := captureSyncJSON(t)
	if br.Status != "clean" || !br.Pushed {
		t.Errorf("want clean+pushed, got %+v", br)
	}

	// Second sync: noop.
	resetSyncFlags()
	br = captureSyncJSON(t)
	if br.Status != "noop" {
		t.Errorf("want noop, got %+v", br)
	}
}

func TestWorktreeSyncJSONConflictIsNotError(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatal(err)
	}
	s, _ := stack.LoadStack(root, "feat")
	commitInWorktree(t, s.FindNode("feat/a").WorktreePath, "shared.txt", "A\n", "a")
	commitInWorktree(t, s.FindNode("feat/b").WorktreePath, "shared.txt", "B\n", "b")
	chdir(t, s.FindNode("feat/b").WorktreePath)

	resetSyncFlags()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := RunSync([]string{"--json"})
	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("conflict must not be a process error: %v", err)
	}
	out, _ := io.ReadAll(r)
	var sr SyncResult
	json.Unmarshal(out, &sr)
	if sr.Error != "" {
		t.Errorf("top-level error must be empty on conflict, got %q", sr.Error)
	}
	if len(sr.Branches) != 1 || sr.Branches[0].Status != "conflicted" {
		t.Errorf("want branches[0].status=conflicted, got %+v", sr.Branches)
	}
	if len(sr.Branches[0].Conflicts) == 0 {
		t.Errorf("conflicts must list paths")
	}
}
