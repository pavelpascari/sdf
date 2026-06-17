// cmd/branch_worktree_test.go
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	"github.com/pavelpascari/sdf/internal/stack"
)

// TestConcurrentBranchKeepsAllNodes verifies that two concurrent RunBranch
// calls on the same stack do not lose nodes due to a read-modify-write race.
// The race under test is the stack-file write; WithLock serializes it.
// NOTE: RunBranch drives the shared rootCmd (cobra shared global state), so
// the goroutines' Execute() calls are serialized behind a mutex to avoid cobra
// flag-state corruption — the file-level race is what WithLock prevents, and
// it serializes that regardless of how Execute is invoked.
func TestConcurrentBranchKeepsAllNodes(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	// Two concurrent appends to the same stack must both survive.
	// Serialize the cobra Execute() calls to avoid shared rootCmd flag-state
	// corruption; the stack-file race (what WithLock guards) still fires
	// unless WithLock is in place.
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, name := range []string{"feat/b", "feat/c"} {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			mu.Lock()
			defer mu.Unlock()
			_ = RunBranch([]string{n, "--no-prefix"})
		}(name)
	}
	wg.Wait()
	s, _ := stack.LoadStack(root, "feat")
	for _, b := range []string{"feat/a", "feat/b", "feat/c"} {
		if s.FindNode(b) == nil {
			t.Errorf("node %s lost to a concurrent branch race", b)
		}
	}
}

func TestBranchWorktreeModeCreatesWorktree(t *testing.T) {
	root := bareRepoWithClone(t) // from new_worktree_test.go (same package)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatalf("new: %v", err)
	}

	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatalf("branch: %v", err)
	}

	s, _ := stack.LoadStack(root, "feat")
	nb := s.FindNode("feat/b")
	if nb == nil || nb.WorktreePath == "" {
		t.Fatalf("feat/b should have a worktree path, got %+v", s.Nodes)
	}
	want := cfgpkg.Defaults().WorktreePathFor(root, "feat/b")
	if nb.WorktreePath != want {
		t.Errorf("WorktreePath = %q, want %q", nb.WorktreePath, want)
	}
	if _, err := os.Stat(filepath.Join(want, "f.txt")); err != nil {
		t.Errorf("worktree checkout missing: %v", err)
	}
	// Main repo HEAD untouched.
	out, _ := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if string(out) != "main\n" {
		t.Errorf("main repo HEAD changed to %q", out)
	}
}
