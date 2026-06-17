// internal/stack/findroot_worktree_test.go
package stack

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFindRootFromLinkedWorktree(t *testing.T) {
	repo := t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git(repo, "init", "-b", "main")
	git(repo, "config", "user.email", "t@t.com")
	git(repo, "config", "user.name", "T")
	os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x\n"), 0644)
	git(repo, "add", ".")
	git(repo, "commit", "-m", "init")

	// Create the .sdf marker in the main repo only.
	if err := Init(repo, "feat", "main"); err != nil {
		t.Fatal(err)
	}

	wt := filepath.Join(t.TempDir(), "feat-a")
	git(repo, "worktree", "add", "-b", "feat-a", wt, "main")

	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	if err := os.Chdir(wt); err != nil {
		t.Fatal(err)
	}

	got, err := FindRoot()
	if err != nil {
		t.Fatalf("FindRoot from worktree: %v", err)
	}
	gotR, _ := filepath.EvalSymlinks(got)
	wantR, _ := filepath.EvalSymlinks(repo)
	if gotR != wantR {
		t.Errorf("FindRoot = %q, want main repo %q", gotR, wantR)
	}
}
