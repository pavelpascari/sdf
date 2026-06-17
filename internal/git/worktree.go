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

// runIn executes a git command, optionally in the given directory.
// When dir is empty the command runs in the current working directory;
// when dir is non-empty "-C dir" is prepended to the arguments.
// The recordRun call mirrors what run() and runAt() would have recorded:
//   - no "-C dir" prefix when dir == ""
//   - "-C dir" prefix when dir != ""
func runIn(dir string, args ...string) (string, error) {
	var cmdArgs []string
	var recorded []string
	if dir == "" {
		cmdArgs = args
		recorded = args
	} else {
		cmdArgs = append([]string{"-C", dir}, args...)
		recorded = cmdArgs
	}
	cmd := exec.Command(Binary, cmdArgs...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	exitCode := 0
	if err != nil {
		exitCode = 1
	}
	recordRun(recorded, output, exitCode)
	if err != nil {
		return output, fmt.Errorf("git %s: %s", strings.Join(cmdArgs, " "), output)
	}
	return output, nil
}

// runAt executes a git command in the given directory and records it.
func runAt(dir string, args ...string) (string, error) {
	return runIn(dir, args...)
}

// rebaseContinueIn continues a paused rebase, optionally inside dir.
// GIT_EDITOR=true suppresses the commit-message editor.
// When dir is empty the command runs in the current working directory.
func rebaseContinueIn(dir string) error {
	var cmdArgs []string
	var recorded []string
	if dir == "" {
		cmdArgs = []string{"rebase", "--continue"}
		recorded = cmdArgs
	} else {
		cmdArgs = []string{"-C", dir, "rebase", "--continue"}
		recorded = cmdArgs
	}
	cmd := exec.Command(Binary, cmdArgs...)
	cmd.Env = append(cmd.Environ(), "GIT_EDITOR=true")
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	exitCode := 0
	if err != nil {
		exitCode = 1
	}
	recordRun(recorded, output, exitCode)
	if err != nil {
		if dir == "" {
			return fmt.Errorf("git rebase --continue: %s", output)
		}
		return fmt.Errorf("git -C %s rebase --continue: %s", dir, output)
	}
	return nil
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
	return rebaseContinueIn(dir)
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

// CherryPickAt runs `git cherry-pick <commits...>` inside the worktree at dir.
func CherryPickAt(dir string, commits ...string) error {
	_, err := runAt(dir, append([]string{"cherry-pick"}, commits...)...)
	return err
}

// CherryPickAbortAt aborts a paused cherry-pick inside the worktree at dir.
func CherryPickAbortAt(dir string) error {
	_, err := runAt(dir, "cherry-pick", "--abort")
	return err
}

// ResetHardAt runs `git reset --hard <ref>` inside the worktree at dir.
func ResetHardAt(dir, ref string) error {
	_, err := runAt(dir, "reset", "--hard", ref)
	return err
}

// AddAt runs `git add <paths...>` inside the worktree at dir.
func AddAt(dir string, paths ...string) error {
	_, err := runAt(dir, append([]string{"add"}, paths...)...)
	return err
}

// ApplyPatchAt applies a patch string inside the worktree at dir using git apply --3way.
func ApplyPatchAt(dir, patch string) error {
	f, err := os.CreateTemp("", "sdf-patch-*.patch")
	if err != nil {
		return fmt.Errorf("cannot create temp file: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.WriteString(patch); err != nil {
		f.Close()
		return fmt.Errorf("cannot write patch: %w", err)
	}
	f.Close()
	_, err = runAt(dir, "apply", "--3way", f.Name())
	return err
}

// CommitAt creates a commit with the given message inside the worktree at dir.
func CommitAt(dir, message string) error {
	_, err := runAt(dir, "commit", "-m", message)
	return err
}

// CheckoutAt runs `git checkout <branch>` inside the worktree at dir.
func CheckoutAt(dir, branch string) error {
	_, err := runAt(dir, "checkout", branch)
	return err
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
