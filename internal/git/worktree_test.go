// internal/git/worktree_test.go
package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pavelpascari/sdf/internal/testutil"
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

func advanceRemoteMain(t *testing.T, repo string) string {
	t.Helper()
	mustGit(t, repo, "branch", "remote-main")
	remoteWorktree := filepath.Join(t.TempDir(), "remote-main")
	mustGit(t, repo, "worktree", "add", remoteWorktree, "remote-main")
	if err := os.WriteFile(filepath.Join(remoteWorktree, "remote.txt"), []byte("remote\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, remoteWorktree, "add", "remote.txt")
	mustGit(t, remoteWorktree, "commit", "-m", "advance remote main")
	remoteTip, err := RevParseAt(remoteWorktree, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "update-ref", "refs/remotes/origin/main", remoteTip)
	return remoteTip
}

func TestFastForward(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	t.Run("current worktree", func(t *testing.T) {
		repo := initTestRepo(t)
		chdir(t, repo)
		remoteTip := advanceRemoteMain(t, repo)

		if err := FastForward("main"); err != nil {
			t.Fatalf("FastForward: %v", err)
		}
		mainTip, err := RevParseAt(repo, "main")
		if err != nil {
			t.Fatal(err)
		}
		if mainTip != remoteTip {
			t.Errorf("main tip = %s, want %s", mainTip, remoteTip)
		}
		if _, err := os.Stat(filepath.Join(repo, "remote.txt")); err != nil {
			t.Errorf("fast-forwarded file missing: %v", err)
		}
		clean, err := IsCleanAt(repo)
		if err != nil || !clean {
			t.Errorf("current worktree should be clean, clean=%v err=%v", clean, err)
		}
	})

	t.Run("branch in multiple worktrees", func(t *testing.T) {
		repo := initTestRepo(t)
		chdir(t, repo)
		duplicateWorktree := filepath.Join(t.TempDir(), "duplicate-main")
		mustGit(t, repo, "worktree", "add", "--force", duplicateWorktree, "main")
		initialTip, err := RevParseAt(repo, "main")
		if err != nil {
			t.Fatal(err)
		}
		advanceRemoteMain(t, repo)

		err = FastForward("main")
		if err == nil || !strings.Contains(err.Error(), "multiple worktrees") {
			t.Fatalf("FastForward error = %v, want multiple-worktrees error", err)
		}
		mainTip, err := RevParseAt(repo, "main")
		if err != nil {
			t.Fatal(err)
		}
		if mainTip != initialTip {
			t.Errorf("main moved from %s to %s", initialTip, mainTip)
		}
	})

	t.Run("linked worktree", func(t *testing.T) {
		repo := initTestRepo(t)
		chdir(t, repo)
		featureWorktree := filepath.Join(t.TempDir(), "feature")
		mustGit(t, repo, "worktree", "add", "-b", "feature", featureWorktree, "main")
		remoteTip := advanceRemoteMain(t, repo)
		chdir(t, featureWorktree)

		if err := FastForward("main"); err != nil {
			t.Fatalf("FastForward: %v", err)
		}
		mainTip, err := RevParseAt(repo, "main")
		if err != nil {
			t.Fatal(err)
		}
		if mainTip != remoteTip {
			t.Errorf("main tip = %s, want %s", mainTip, remoteTip)
		}
		if _, err := os.Stat(filepath.Join(repo, "remote.txt")); err != nil {
			t.Errorf("fast-forwarded file missing: %v", err)
		}
		clean, err := IsCleanAt(repo)
		if err != nil || !clean {
			t.Errorf("base worktree should be clean, clean=%v err=%v", clean, err)
		}
	})

	t.Run("dirty linked worktree", func(t *testing.T) {
		repo := initTestRepo(t)
		chdir(t, repo)
		featureWorktree := filepath.Join(t.TempDir(), "feature")
		mustGit(t, repo, "worktree", "add", "-b", "feature", featureWorktree, "main")
		initialTip, err := RevParseAt(repo, "main")
		if err != nil {
			t.Fatal(err)
		}
		advanceRemoteMain(t, repo)
		if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("dirty\n"), 0644); err != nil {
			t.Fatal(err)
		}
		chdir(t, featureWorktree)

		if err := FastForward("main"); err == nil {
			t.Fatal("FastForward should reject a dirty checked-out branch")
		}
		mainTip, err := RevParseAt(repo, "main")
		if err != nil {
			t.Fatal(err)
		}
		if mainTip != initialTip {
			t.Errorf("main moved from %s to %s", initialTip, mainTip)
		}
		contents, err := os.ReadFile(filepath.Join(repo, "f.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(contents) != "dirty\n" {
			t.Errorf("dirty contents = %q, want %q", contents, "dirty\\n")
		}
	})

	t.Run("untracked file collision", func(t *testing.T) {
		repo := initTestRepo(t)
		chdir(t, repo)
		initialTip, err := RevParseAt(repo, "main")
		if err != nil {
			t.Fatal(err)
		}
		advanceRemoteMain(t, repo)
		if err := os.WriteFile(filepath.Join(repo, "remote.txt"), []byte("local\n"), 0644); err != nil {
			t.Fatal(err)
		}

		if err := FastForward("main"); err == nil {
			t.Fatal("FastForward should reject an untracked file collision")
		}
		mainTip, err := RevParseAt(repo, "main")
		if err != nil {
			t.Fatal(err)
		}
		if mainTip != initialTip {
			t.Errorf("main moved from %s to %s", initialTip, mainTip)
		}
		contents, err := os.ReadFile(filepath.Join(repo, "remote.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(contents) != "local\n" {
			t.Errorf("untracked contents = %q, want %q", contents, "local\\n")
		}
	})

	t.Run("stale registered worktree", func(t *testing.T) {
		repo := initTestRepo(t)
		chdir(t, repo)
		initialTip, err := RevParseAt(repo, "main")
		if err != nil {
			t.Fatal(err)
		}
		mustGit(t, repo, "checkout", "--detach", initialTip)
		mainWorktree := filepath.Join(t.TempDir(), "main")
		mustGit(t, repo, "worktree", "add", mainWorktree, "main")
		advanceRemoteMain(t, repo)
		if err := os.RemoveAll(mainWorktree); err != nil {
			t.Fatal(err)
		}

		if err := FastForward("main"); err == nil {
			t.Fatal("FastForward should reject a stale registered worktree")
		}
		mainTip, err := RevParseAt(repo, "main")
		if err != nil {
			t.Fatal(err)
		}
		if mainTip != initialTip {
			t.Errorf("main moved from %s to %s", initialTip, mainTip)
		}
	})

	t.Run("branch without worktree", func(t *testing.T) {
		repo := initTestRepo(t)
		chdir(t, repo)
		initialTip, err := RevParseAt(repo, "main")
		if err != nil {
			t.Fatal(err)
		}
		remoteTip := advanceRemoteMain(t, repo)
		mustGit(t, repo, "checkout", "--detach", initialTip)

		if err := FastForward("main"); err != nil {
			t.Fatalf("FastForward: %v", err)
		}
		mainTip, err := RevParseAt(repo, "main")
		if err != nil {
			t.Fatal(err)
		}
		if mainTip != remoteTip {
			t.Errorf("main tip = %s, want %s", mainTip, remoteTip)
		}
		headTip, err := RevParseAt(repo, "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		if headTip != initialTip {
			t.Errorf("detached HEAD moved from %s to %s", initialTip, headTip)
		}
	})

	t.Run("diverged branch", func(t *testing.T) {
		repo := initTestRepo(t)
		chdir(t, repo)
		advanceRemoteMain(t, repo)
		if err := os.WriteFile(filepath.Join(repo, "local.txt"), []byte("local\n"), 0644); err != nil {
			t.Fatal(err)
		}
		mustGit(t, repo, "add", "local.txt")
		mustGit(t, repo, "commit", "-m", "advance local main")
		localTip, err := RevParseAt(repo, "main")
		if err != nil {
			t.Fatal(err)
		}

		if err := FastForward("main"); err == nil {
			t.Fatal("FastForward should reject a diverged branch")
		}
		mainTip, err := RevParseAt(repo, "main")
		if err != nil {
			t.Fatal(err)
		}
		if mainTip != localTip {
			t.Errorf("diverged main moved from %s to %s", localTip, mainTip)
		}
	})

	t.Run("pinned remote tip ancestry", func(t *testing.T) {
		dir := t.TempDir()
		fakeGit := testutil.FakeBin(t, dir, "git", map[string]string{
			"rev-parse origin/main":                         "remote-tip",
			"rev-parse main":                                "local-tip",
			"merge-base --is-ancestor local-tip remote-tip": "",
			"worktree list --porcelain":                     "",
			"branch --force main remote-tip":                "",
		})
		testutil.SetBinary(t, &Binary, fakeGit)

		if err := FastForward("main"); err != nil {
			t.Fatalf("FastForward: %v", err)
		}
		calls := strings.Join(testutil.ReadLog(t, dir, "git"), "\n")
		if !strings.Contains(calls, "merge-base --is-ancestor local-tip remote-tip") {
			t.Errorf("ancestry check did not use pinned remote tip:\n%s", calls)
		}
	})

	t.Run("already current", func(t *testing.T) {
		repo := initTestRepo(t)
		chdir(t, repo)
		mainTip, err := RevParseAt(repo, "main")
		if err != nil {
			t.Fatal(err)
		}
		mustGit(t, repo, "update-ref", "refs/remotes/origin/main", mainTip)

		if err := FastForward("main"); err != nil {
			t.Fatalf("FastForward: %v", err)
		}
		currentTip, err := RevParseAt(repo, "main")
		if err != nil {
			t.Fatal(err)
		}
		if currentTip != mainTip {
			t.Errorf("current main moved from %s to %s", mainTip, currentTip)
		}
	})
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
