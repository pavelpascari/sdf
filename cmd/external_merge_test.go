// cmd/external_merge_test.go
// Test: J8 — external merge propagation.
// When a human merges a PR on GitHub, a subsequent sdf status must show the
// merged node as status:"merged" and its immediate child as
// sync_state:"needs_sync", so that flow's cascade triggers correctly.
//
// We simulate gh's state by directly writing the merged status into the stack
// JSON (just as ReconcileFromPRs would), advance main past the merged node's
// tip, then run status --json and assert both invariants.
package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
)

// commitOnMain commits a file on the main branch inside the repo at root
// using git -C (no checkout change required; main must already be the HEAD
// or reachable). Since in a worktree-mode repo the main repo's HEAD is on
// main, this is safe to call from the clone root.
func commitOnMain(t *testing.T, root, file, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, file), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", file},
		{"-c", "user.email=t@t.com", "-c", "user.name=T", "commit", "-m", msg},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// captureStatusJSON runs RunStatus(["--json"]) and returns its stdout bytes.
// Flags are reset before the call; the caller must have chdir'd to the repo root.
func captureStatusJSON(t *testing.T) []byte {
	t.Helper()
	resetStatusFlags()
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
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatalf("status --json: %v", runErr)
	}
	return buf.Bytes()
}

// TestExternalMergePropagatesNeedsSync asserts J8: when feat/a is externally
// merged (status set to "merged") and main advances past its tip (simulating
// the squash-merge commit), status reports feat/a as merged and feat/b as
// needs_sync (because ParentBranch skips feat/a → feat/b's effective parent is
// main, which has moved beyond feat/b's recorded BaseTip).
func TestExternalMergePropagatesNeedsSync(t *testing.T) {
	// Build: main ← feat/a (worktree) ← feat/b (worktree)
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatal(err)
	}

	// Simulate feat/a being merged on GitHub: write merged status directly into
	// the stack JSON (this is exactly what ReconcileFromPRs + ApplyRoutineChange
	// would do when gh reports state:"MERGED").
	s, err := stack.LoadStack(root, "feat")
	if err != nil {
		t.Fatal(err)
	}
	nodeA := s.FindNode("feat/a")
	if nodeA == nil {
		t.Fatal("precondition: feat/a not found in stack")
	}
	nodeA.Status = "merged"
	if err := stack.Save(root, s); err != nil {
		t.Fatal(err)
	}

	// Advance main past feat/a's tip (simulates the squash-merge commit that
	// GitHub pushes to main when the PR is merged).
	// The main repo's HEAD is on main (worktree mode leaves main repo on base).
	commitOnMain(t, root, "merged.txt", "m\n", "merge of feat/a")

	// Run status --json from the main repo root.
	chdir(t, root)
	out := captureStatusJSON(t)

	var sr StatusResult
	if err := json.Unmarshal(out, &sr); err != nil {
		t.Fatalf("unmarshal status result: %v\nraw: %s", err, out)
	}

	// Assert both invariants.
	var gotStatusA, gotSyncStateB string
	for _, n := range sr.Nodes {
		switch n.Branch {
		case "feat/a":
			gotStatusA = n.Status
		case "feat/b":
			gotSyncStateB = n.SyncState
		}
	}
	if gotStatusA != "merged" {
		t.Errorf("feat/a status = %q, want \"merged\"", gotStatusA)
	}
	if gotSyncStateB != "needs_sync" {
		t.Errorf("feat/b sync_state = %q, want \"needs_sync\"", gotSyncStateB)
	}
}
