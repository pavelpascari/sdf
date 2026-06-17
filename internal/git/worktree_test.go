// internal/git/worktree_test.go
package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestIsCleanAtCountsUntracked(t *testing.T) {
	repo := initTestRepo(t)
	chdir(t, repo)
	wt := filepath.Join(t.TempDir(), "w")
	if err := WorktreeAdd(wt, "w", "main"); err != nil {
		t.Fatal(err)
	}
	// Drop an untracked file into the worktree.
	if err := os.WriteFile(filepath.Join(wt, "scratch.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// IsCleanAt ignores untracked files (documented behavior — needed for sync).
	clean, err := IsCleanAt(wt)
	if err != nil {
		t.Fatalf("IsCleanAt: %v", err)
	}
	if !clean {
		t.Errorf("IsCleanAt should ignore untracked files; got dirty")
	}

	// IsWorktreeRemovable must count untracked files — matches what git worktree remove checks.
	removable, err := IsWorktreeRemovable(wt)
	if err != nil {
		t.Fatalf("IsWorktreeRemovable: %v", err)
	}
	if removable {
		t.Errorf("IsWorktreeRemovable must report false when untracked files are present")
	}
}

func TestIsWorktreeRemovableCleanWorktree(t *testing.T) {
	repo := initTestRepo(t)
	chdir(t, repo)
	wt := filepath.Join(t.TempDir(), "w2")
	if err := WorktreeAdd(wt, "w2", "main"); err != nil {
		t.Fatal(err)
	}

	removable, err := IsWorktreeRemovable(wt)
	if err != nil {
		t.Fatalf("IsWorktreeRemovable: %v", err)
	}
	if !removable {
		t.Errorf("IsWorktreeRemovable should report true for a clean worktree")
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

func TestCherryPickAtAndResetHardAt(t *testing.T) {
	repo := initTestRepo(t)
	chdir(t, repo)

	// Create a worktree on a new branch "wt-branch" based on main.
	wtPath := filepath.Join(t.TempDir(), "wt-branch")
	if err := WorktreeAdd(wtPath, "wt-branch", "main"); err != nil {
		t.Fatalf("WorktreeAdd: %v", err)
	}

	// Record the worktree's HEAD before cherry-pick.
	beforeSHA, err := RevParseAt(wtPath, "HEAD")
	if err != nil {
		t.Fatalf("RevParseAt before: %v", err)
	}

	// Make a commit on a side branch "side" from main (in the main repo dir).
	mustGit(t, repo, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(repo, "side.txt"), []byte("side\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "add", "side.txt")
	mustGit(t, repo, "commit", "-m", "side commit")

	// Get the SHA of that commit.
	sideSHACmd := exec.Command("git", "-C", repo, "rev-parse", "HEAD")
	sideSHAOut, err := sideSHACmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse side HEAD: %v\n%s", err, sideSHAOut)
	}
	sideSHA := strings.TrimSpace(string(sideSHAOut))

	// Cherry-pick the side commit into the worktree.
	if err := CherryPickAt(wtPath, sideSHA); err != nil {
		t.Fatalf("CherryPickAt: %v", err)
	}

	// The worktree HEAD should now differ from beforeSHA.
	afterPickSHA, err := RevParseAt(wtPath, "HEAD")
	if err != nil {
		t.Fatalf("RevParseAt after cherry-pick: %v", err)
	}
	if afterPickSHA == beforeSHA {
		t.Errorf("HEAD did not change after CherryPickAt; still %s", beforeSHA)
	}

	// ResetHardAt should move HEAD back to beforeSHA.
	if err := ResetHardAt(wtPath, beforeSHA); err != nil {
		t.Fatalf("ResetHardAt: %v", err)
	}
	afterResetSHA, err := RevParseAt(wtPath, "HEAD")
	if err != nil {
		t.Fatalf("RevParseAt after reset: %v", err)
	}
	if afterResetSHA != beforeSHA {
		t.Errorf("after ResetHardAt: HEAD = %s, want %s", afterResetSHA, beforeSHA)
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
