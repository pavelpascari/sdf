// internal/git/worktree_test.go
package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initTestRepo creates a git repo with one commit on "main" and returns its path.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "init", "-b", "main")
	mustGit(t, dir, "config", "user.email", "test@test.com")
	mustGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", "init")
	return dir
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestWorktreeAddRemoveAndList(t *testing.T) {
	repo := initTestRepo(t)
	chdir(t, repo)

	wtPath := filepath.Join(t.TempDir(), "feat-a")
	if err := WorktreeAdd(wtPath, "feat-a", "main"); err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "f.txt")); err != nil {
		t.Fatalf("worktree files missing: %v", err)
	}

	list, err := WorktreeList()
	if err != nil {
		t.Fatalf("WorktreeList: %v", err)
	}
	found := false
	for _, w := range list {
		if w.Branch == "feat-a" {
			found = true
		}
	}
	if !found {
		t.Errorf("feat-a not in worktree list: %+v", list)
	}

	if err := WorktreeRemove(wtPath, false); err != nil {
		t.Fatalf("WorktreeRemove: %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree dir should be gone")
	}
}

func TestIsCleanAtAndRevParseAt(t *testing.T) {
	repo := initTestRepo(t)
	chdir(t, repo)
	wtPath := filepath.Join(t.TempDir(), "feat-b")
	if err := WorktreeAdd(wtPath, "feat-b", "main"); err != nil {
		t.Fatal(err)
	}

	clean, err := IsCleanAt(wtPath)
	if err != nil || !clean {
		t.Fatalf("expected clean worktree, got clean=%v err=%v", clean, err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "f.txt"), []byte("changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	clean, _ = IsCleanAt(wtPath)
	if clean {
		t.Errorf("expected dirty worktree after edit")
	}

	sha, err := RevParseAt(wtPath, "HEAD")
	if err != nil || len(sha) != 40 {
		t.Fatalf("RevParseAt HEAD: sha=%q err=%v", sha, err)
	}
}

func TestMainWorktreeRootFromLinkedWorktree(t *testing.T) {
	repo := initTestRepo(t)
	chdir(t, repo)
	wtPath := filepath.Join(t.TempDir(), "feat-c")
	if err := WorktreeAdd(wtPath, "feat-c", "main"); err != nil {
		t.Fatal(err)
	}
	chdir(t, wtPath)

	root, err := MainWorktreeRoot()
	if err != nil {
		t.Fatalf("MainWorktreeRoot: %v", err)
	}
	// Resolve symlinks because macOS /var -> /private/var.
	gotResolved, _ := filepath.EvalSymlinks(root)
	wantResolved, _ := filepath.EvalSymlinks(repo)
	if gotResolved != wantResolved {
		t.Errorf("MainWorktreeRoot = %q, want %q", gotResolved, wantResolved)
	}
}

// chdir changes to dir and restores the cwd on cleanup.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}
