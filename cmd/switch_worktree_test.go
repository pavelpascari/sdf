// cmd/switch_worktree_test.go
package cmd

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/spf13/pflag"
)

// resetSwitchFlags restores switchCmd's flags to their defaults so that reusing
// the package-level rootCmd across in-process test invocations does not leak
// flag state (e.g. a prior --path-only). A real `sdf` run is process-isolated;
// this reproduces that isolation for tests.
func resetSwitchFlags() {
	switchCmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
}

func TestSwitchWorktreePrintsPathAndDoesNotCheckout(t *testing.T) {
	resetSwitchFlags()
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	s, _ := stack.LoadStack(root, "feat")
	wantPath := s.FindNode("feat/a").WorktreePath

	chdir(t, root)
	headBefore, _ := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()

	if err := RunSwitch([]string{"feat/a"}); err != nil {
		t.Fatalf("switch: %v", err)
	}

	// Main repo HEAD must be unchanged (no checkout in worktree mode).
	headAfter, _ := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if string(headBefore) != string(headAfter) {
		t.Errorf("switch must not check out in worktree mode (HEAD changed %q→%q)", headBefore, headAfter)
	}

	// Verify --path-only prints exactly the worktree path.
	resetSwitchFlags()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := RunSwitch([]string{"feat/a", "--path-only"})
	_ = w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatalf("switch --path-only: %v", err)
	}
	out, _ := io.ReadAll(r)
	if got := strings.TrimSpace(string(out)); got != wantPath {
		t.Errorf("--path-only printed %q, want %q", got, wantPath)
	}
}
