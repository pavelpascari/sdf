// internal/git/worktree.go
package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WorktreeInfo describes one entry from `git worktree list --porcelain`.
type WorktreeInfo struct {
	Path   string
	Branch string // short branch name, "" if detached
	Head   string // commit SHA
}

// runAt executes a git command in the given directory and records it.
func runAt(dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command(Binary, full...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	exitCode := 0
	if err != nil {
		exitCode = 1
	}
	recordRun(full, output, exitCode)
	if err != nil {
		return output, fmt.Errorf("git %s: %s", strings.Join(full, " "), output)
	}
	return output, nil
}

// GitCommonDir returns the absolute path to the shared .git directory,
// which is identical across all worktrees of the same repo.
func GitCommonDir() (string, error) {
	return run("rev-parse", "--path-format=absolute", "--git-common-dir")
}

// MainWorktreeRoot returns the working-tree root of the main worktree
// (the parent of the shared .git directory), discoverable from any worktree.
func MainWorktreeRoot() (string, error) {
	common, err := GitCommonDir()
	if err != nil {
		return "", err
	}
	// common is ".../<mainroot>/.git"
	return filepath.Dir(common), nil
}

// WorktreeAdd creates a worktree at path. If createFrom is non-empty, a new
// branch is created from that ref; otherwise the existing branch is checked out.
func WorktreeAdd(path, branch, createFrom string) error {
	var err error
	if createFrom != "" {
		_, err = run("worktree", "add", "-b", branch, path, createFrom)
	} else {
		_, err = run("worktree", "add", path, branch)
	}
	return err
}

// WorktreeRemove removes the worktree at path.
func WorktreeRemove(path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := run(args...)
	return err
}

// WorktreeList returns all worktrees of the current repo.
func WorktreeList() ([]WorktreeInfo, error) {
	out, err := run("worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var list []WorktreeInfo
	var cur WorktreeInfo
	flush := func() {
		if cur.Path != "" {
			list = append(list, cur)
		}
		cur = WorktreeInfo{}
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	return list, nil
}

// IsCleanAt reports whether the worktree at dir has no uncommitted tracked changes.
// It intentionally ignores untracked files (--untracked-files=no), which is the
// right check for sync: a worktree with only untracked scratch files can still rebase.
func IsCleanAt(dir string) (bool, error) {
	out, err := runAt(dir, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return false, err
	}
	return out == "", nil
}

// IsWorktreeRemovable reports whether the worktree at dir has no uncommitted
// changes AND no untracked files — i.e. `git worktree remove` will succeed
// without --force. Unlike IsCleanAt, this counts untracked files.
func IsWorktreeRemovable(dir string) (bool, error) {
	out, err := runAt(dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out == "", nil
}

// RevParseAt resolves a ref to a SHA within the worktree at dir.
func RevParseAt(dir, ref string) (string, error) {
	return runAt(dir, "rev-parse", ref)
}

// RebaseOntoAt runs `git rebase --onto newBase oldBase branch` inside dir.
func RebaseOntoAt(dir, newBase, oldBase, branch string) error {
	_, err := runAt(dir, "rebase", "--onto", newBase, oldBase, branch)
	return err
}

// PushAt force-pushes a branch from within the worktree at dir.
func PushAt(dir, branch string) error {
	_, err := runAt(dir, "push", "--force-with-lease", "origin", branch)
	return err
}

// RebaseContinueAt continues a paused rebase inside dir with a no-op editor.
func RebaseContinueAt(dir string) error {
	cmd := exec.Command(Binary, "-C", dir, "rebase", "--continue")
	cmd.Env = append(cmd.Environ(), "GIT_EDITOR=true")
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	exitCode := 0
	if err != nil {
		exitCode = 1
	}
	recordRun([]string{"-C", dir, "rebase", "--continue"}, output, exitCode)
	if err != nil {
		return fmt.Errorf("git -C %s rebase --continue: %s", dir, output)
	}
	return nil
}

// RebaseAbortAt aborts a paused rebase inside dir.
func RebaseAbortAt(dir string) error {
	_, err := runAt(dir, "rebase", "--abort")
	return err
}

// ConflictedFilesAt lists files with merge conflicts in the worktree at dir.
func ConflictedFilesAt(dir string) ([]string, error) {
	out, err := runAt(dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// IsRebaseInProgressAt reports whether a rebase is paused in the worktree at dir.
func IsRebaseInProgressAt(dir string) (bool, error) {
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		p, err := runAt(dir, "rev-parse", "--git-path", name)
		if err != nil {
			return false, err
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if _, statErr := os.Stat(p); statErr == nil {
			return true, nil
		}
	}
	return false, nil
}
