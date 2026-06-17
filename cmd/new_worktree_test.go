// cmd/new_worktree_test.go
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	"github.com/pavelpascari/sdf/internal/stack"
)

// bareRepoWithClone sets up an origin + working clone on main, chdirs into the
// clone, and returns the clone root. Push operations succeed against origin.
func bareRepoWithClone(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	work := filepath.Join(base, "work")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if out, err := exec.Command("git", "init", "--bare", "-b", "main", origin).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "clone", origin, work).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	run(work, "config", "user.email", "t@t.com")
	run(work, "config", "user.name", "T")
	os.WriteFile(filepath.Join(work, "f.txt"), []byte("x\n"), 0644)
	run(work, "add", ".")
	run(work, "commit", "-m", "init")
	run(work, "push", "-u", "origin", "main")

	// Resolve symlinks so that paths returned here match what git and os.Getwd
	// return (on macOS /var is a symlink to /private/var).
	resolved, err := filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	orig, _ := os.Getwd()
	if err := os.Chdir(resolved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	return resolved
}

func TestNewWorktreeModeCreatesWorktree(t *testing.T) {
	root := bareRepoWithClone(t)

	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatalf("runNewCore worktree: %v", err)
	}

	s, err := stack.LoadStack(root, "feat")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Worktree {
		t.Errorf("stack should be worktree-enabled")
	}
	if len(s.Nodes) != 1 || s.Nodes[0].WorktreePath == "" {
		t.Fatalf("expected one node with a worktree path, got %+v", s.Nodes)
	}
	want := cfgpkg.Defaults().WorktreePathFor(root, "feat/a")
	if s.Nodes[0].WorktreePath != want {
		t.Errorf("WorktreePath = %q, want %q", s.Nodes[0].WorktreePath, want)
	}
	if _, err := os.Stat(filepath.Join(want, "f.txt")); err != nil {
		t.Errorf("worktree checkout missing: %v", err)
	}

	// Main repo must remain on the base branch, not the new branch.
	out, _ := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if got := string(out); got != "main\n" {
		t.Errorf("main repo HEAD = %q, want main", got)
	}
}
