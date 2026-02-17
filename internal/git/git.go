// Package git provides shell-out helpers for git operations.
package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// run executes a git command and returns its trimmed stdout.
func run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		return output, fmt.Errorf("git %s: %s", strings.Join(args, " "), output)
	}
	return output, nil
}

// CurrentBranch returns the current checked-out branch name.
func CurrentBranch() (string, error) {
	return run("rev-parse", "--abbrev-ref", "HEAD")
}

// RepoRoot returns the top-level directory of the git repository.
func RepoRoot() (string, error) {
	return run("rev-parse", "--show-toplevel")
}

// IsClean returns true if the working tree has no uncommitted changes.
func IsClean() (bool, error) {
	out, err := run("status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out == "", nil
}

// CreateBranch creates a new branch from the current HEAD.
func CreateBranch(name string) error {
	_, err := run("checkout", "-b", name)
	return err
}

// Checkout switches to the given branch.
func Checkout(branch string) error {
	_, err := run("checkout", branch)
	return err
}

// Push pushes a branch to origin, with force-with-lease for safety.
func Push(branch string) error {
	_, err := run("push", "--force-with-lease", "origin", branch)
	return err
}

// PushNew pushes a new branch to origin and sets up tracking.
func PushNew(branch string) error {
	_, err := run("push", "-u", "origin", branch)
	return err
}

// FetchAll fetches from origin.
func FetchAll() error {
	_, err := run("fetch", "origin")
	return err
}

// RevParse returns the SHA for a given ref.
func RevParse(ref string) (string, error) {
	return run("rev-parse", ref)
}

// RebaseOnto performs: git rebase --onto <newBase> <oldBase> <branch>
func RebaseOnto(newBase, oldBase, branch string) error {
	_, err := run("rebase", "--onto", newBase, oldBase, branch)
	return err
}

// RebaseAbort aborts an in-progress rebase.
func RebaseAbort() error {
	_, err := run("rebase", "--abort")
	return err
}

// RebaseContinue continues a rebase after conflicts are resolved.
func RebaseContinue() error {
	cmd := exec.Command("git", "rebase", "--continue")
	cmd.Env = append(cmd.Environ(), "GIT_EDITOR=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git rebase --continue: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// ConflictedFiles returns the list of files with merge conflicts.
func ConflictedFiles() ([]string, error) {
	out, err := run("diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// Add stages files.
func Add(paths ...string) error {
	args := append([]string{"add"}, paths...)
	_, err := run(args...)
	return err
}

// Commit creates a commit with the given message.
func Commit(message string) error {
	_, err := run("commit", "-m", message)
	return err
}

// DiffSummary returns a short summary of changes between two refs.
func DiffSummary(from, to string) (string, error) {
	return run("diff", "--stat", from+".."+to)
}

// DiffFull returns the full diff between two refs.
func DiffFull(from, to string) (string, error) {
	return run("diff", from+".."+to)
}

// Log returns the oneline log between two refs.
func Log(from, to string) (string, error) {
	return run("log", "--oneline", from+".."+to)
}

// MergeBase returns the merge base between two refs.
func MergeBase(a, b string) (string, error) {
	return run("merge-base", a, b)
}

// BranchExists returns true if the branch exists locally.
func BranchExists(branch string) bool {
	_, err := run("rev-parse", "--verify", branch)
	return err == nil
}

// CommitCount returns the number of commits between two refs.
func CommitCount(from, to string) (string, error) {
	return run("rev-list", "--count", from+".."+to)
}
