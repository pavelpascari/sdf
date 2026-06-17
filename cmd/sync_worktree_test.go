// cmd/sync_worktree_test.go
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/spf13/pflag"
)

// mustRun executes a command (optionally in dir) and fails the test on error.
func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// originOf returns the remote URL that the clone at root uses for "origin".
func originOf(t *testing.T, root string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "remote", "get-url", "origin").CombinedOutput()
	if err != nil {
		t.Fatalf("git remote get-url origin: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// commitAndPush makes a commit in dir on branch and pushes it to origin.
func commitAndPush(t *testing.T, dir, branch, file, content, msg string) {
	t.Helper()
	mustRun(t, dir, "git", "checkout", branch)
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "add", ".")
	mustRun(t, dir, "git", "-c", "user.email=t@t.com", "-c", "user.name=T", "commit", "-m", msg)
	mustRun(t, dir, "git", "push", "origin", branch)
}

// resetSyncFlags restores syncCmd's flags to their defaults so that reusing the
// package-level rootCmd across in-process test invocations does not leak flag
// state (e.g. a prior --continue). A real `sdf` run is process-isolated; this
// reproduces that isolation for tests.
func resetSyncFlags() {
	syncCmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func commitInWorktree(t *testing.T, wt, file, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(wt, file), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", msg}} {
		cmd := exec.Command("git", append([]string{"-C", wt}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestWorktreeSyncRebasesOntoAdvancedParent(t *testing.T) {
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
	wtB := s.FindNode("feat/b").WorktreePath

	// Advance feat/a with a new commit (its agent did work and committed).
	commitInWorktree(t, wtA, "a.txt", "from a\n", "a work")
	aTip, _ := gitpkg.RevParseAt(wtA, "HEAD")

	// feat/b's recorded BaseTip is now stale → run sync from feat/b's worktree.
	chdir(t, wtB) // chdir helper is in cmd/... ; if absent, inline os.Chdir
	if err := RunSync(nil); err != nil {
		t.Fatalf("worktree sync: %v", err)
	}

	// feat/b must now contain feat/a's commit, and BaseTip updated.
	s2, _ := stack.LoadStack(root, "feat")
	if got := s2.FindNode("feat/b").BaseTip; got != aTip {
		t.Errorf("feat/b BaseTip = %q, want feat/a tip %q", got, aTip)
	}
	if !gitpkg.IsAncestor(aTip, "feat/b") {
		t.Errorf("feat/a tip should be an ancestor of feat/b after rebase")
	}
}

func TestWorktreeSyncRejectsDirtyWorktree(t *testing.T) {
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
	wtB := s.FindNode("feat/b").WorktreePath
	commitInWorktree(t, wtA, "a.txt", "from a\n", "a work")

	// Leave an uncommitted change in feat/b.
	os.WriteFile(filepath.Join(wtB, "f.txt"), []byte("dirty\n"), 0644)

	chdir(t, wtB)
	err := RunSync(nil)
	if err == nil {
		t.Fatalf("expected sync to refuse a dirty worktree")
	}
}

// TestWorktreeSyncFetchesMovedBaseFromOrigin verifies that running sdf sync in a
// downstream worktree fetches origin and fast-forwards the base branch so that a
// base tip advanced by an external merge is visible and gets integrated.
func TestWorktreeSyncFetchesMovedBaseFromOrigin(t *testing.T) {
	resetSyncFlags()
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	s, _ := stack.LoadStack(root, "feat")
	wtA := s.FindNode("feat/a").WorktreePath

	// Advance origin/main from a separate clone (simulates a sibling PR merge).
	other := t.TempDir()
	mustRun(t, "", "git", "clone", originOf(t, root), other)
	mustRun(t, other, "git", "config", "user.email", "t@t.com")
	mustRun(t, other, "git", "config", "user.name", "T")
	commitAndPush(t, other, "main", "ext.txt", "ext\n", "external")

	// feat/a's BaseTip still equals the OLD local main; running sync in wtA must
	// fetch, fast-forward main, and rebase feat/a onto the new tip.
	resetSyncFlags()
	chdir(t, wtA)
	if err := RunSync(nil); err != nil {
		t.Fatalf("sync: %v", err)
	}
	localMain, _ := gitpkg.RevParse("main")
	if !gitpkg.IsAncestor(localMain, "feat/a") {
		t.Errorf("feat/a did not integrate the moved base")
	}
}
