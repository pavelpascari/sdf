// Package git provides shell-out helpers for git operations.
package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Binary is the name (or path) of the git executable.
// Tests can override this to point at a fake binary.
var Binary = "git"

// run executes a git command and returns its trimmed stdout.
func run(args ...string) (string, error) {
	return runIn("", args...)
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
	out, err := run("status", "--porcelain", "--untracked-files=no")
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

// ResetHead resets the index to HEAD, unstaging any staged changes
// without modifying the working tree.
func ResetHead() error {
	_, err := run("reset", "HEAD")
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

// FastForward updates a local branch to match its remote tracking branch.
func FastForward(branch string) error {
	remote := "origin/" + branch
	remoteTip, err := RevParse(remote)
	if err != nil {
		return err
	}
	localTip, err := RevParse(branch)
	if err != nil {
		return err
	}
	if localTip == remoteTip {
		return nil
	}
	if !IsAncestor(localTip, remoteTip) {
		return fmt.Errorf("%s has diverged from %s", branch, remote)
	}
	worktrees, err := WorktreeList()
	if err != nil {
		return err
	}
	var branchWorktree *WorktreeInfo
	for i := range worktrees {
		worktree := &worktrees[i]
		if worktree.Branch != branch {
			continue
		}
		if branchWorktree != nil {
			return fmt.Errorf("%s is checked out in multiple worktrees: %s and %s", branch, branchWorktree.Path, worktree.Path)
		}
		branchWorktree = worktree
	}
	if branchWorktree != nil {
		clean, err := IsCleanAt(branchWorktree.Path)
		if err != nil {
			return err
		}
		if !clean {
			return fmt.Errorf("%s is checked out at %s with uncommitted changes", branch, branchWorktree.Path)
		}
		_, err = runAt(branchWorktree.Path, "merge", "--ff-only", remoteTip)
		return err
	}
	_, err = run("branch", "--force", branch, remoteTip)
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

// ResetHard resets the current branch to the given ref, discarding all changes.
func ResetHard(ref string) error {
	_, err := run("reset", "--hard", ref)
	return err
}

// RebaseAbort aborts an in-progress rebase.
func RebaseAbort() error {
	_, err := run("rebase", "--abort")
	return err
}

// RebaseContinue continues a rebase after conflicts are resolved.
func RebaseContinue() error {
	return rebaseContinueIn("")
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

// LogSubjects returns the subject lines of commits between two refs.
func LogSubjects(from, to string) ([]string, error) {
	out, err := run("log", "--format=%s", from+".."+to)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// MergeBase returns the merge base between two refs.
func MergeBase(a, b string) (string, error) {
	return run("merge-base", a, b)
}

// IsAncestor returns true if commit a is an ancestor of commit b.
func IsAncestor(a, b string) bool {
	_, err := run("merge-base", "--is-ancestor", a, b)
	return err == nil
}

// CherryPick applies the given commits onto the current branch.
// Mirrors `git cherry-pick <commit>...`.
func CherryPick(commits ...string) error {
	args := append([]string{"cherry-pick"}, commits...)
	_, err := run(args...)
	return err
}

// CherryPickAbort aborts an in-progress cherry-pick.
func CherryPickAbort() error {
	_, err := run("cherry-pick", "--abort")
	return err
}

// LogCommits returns the SHAs of commits between from (exclusive) and to
// (inclusive), in chronological order (oldest first).
func LogCommits(from, to string) ([]string, error) {
	out, err := run("rev-list", "--reverse", from+".."+to)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// BranchExists returns true if the branch exists locally.
func BranchExists(branch string) bool {
	_, err := run("rev-parse", "--verify", "refs/heads/"+branch)
	return err == nil
}

// CheckoutRemote creates a local branch tracking a remote branch.
func CheckoutRemote(branch string) error {
	_, err := run("checkout", "-b", branch, "origin/"+branch)
	return err
}

// DefaultBranch returns the default branch of the remote origin
// (e.g. "main" or "master") by inspecting the remote HEAD.
func DefaultBranch() (string, error) {
	out, err := run("symbolic-ref", "refs/remotes/origin/HEAD")
	if err != nil {
		// Fallback: try common defaults
		for _, candidate := range []string{"main", "master"} {
			if BranchExists("origin/" + candidate) {
				return candidate, nil
			}
		}
		return "", fmt.Errorf("cannot determine default branch: %w", err)
	}
	// out is like "refs/remotes/origin/main"
	parts := strings.Split(out, "/")
	return parts[len(parts)-1], nil
}

// CommitCount returns the number of commits between two refs.
func CommitCount(from, to string) (string, error) {
	return run("rev-list", "--count", from+".."+to)
}

// LSRemoteRef returns the SHA that origin currently has for a given ref,
// without fetching any objects. This is the cheapest possible remote check.
func LSRemoteRef(ref string) (string, error) {
	out, err := run("ls-remote", "--exit-code", "origin", ref)
	if err != nil {
		return "", err
	}
	// Output format: "<sha>\t<refname>"
	parts := strings.Fields(out)
	if len(parts) == 0 {
		return "", fmt.Errorf("no output from ls-remote for %s", ref)
	}
	return parts[0], nil
}

// FetchBranch fetches a single branch from origin.
func FetchBranch(branch string) error {
	_, err := run("fetch", "origin", branch)
	return err
}

// ShowFiles returns the files changed in a single commit.
func ShowFiles(sha string) ([]string, error) {
	out, err := run("diff-tree", "--no-commit-id", "--name-only", "-r", sha)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// ShowSubject returns the subject line of a single commit.
func ShowSubject(sha string) (string, error) {
	return run("log", "-1", "--format=%s", sha)
}

// DeleteBranch force-deletes a local branch.
func DeleteBranch(name string) error {
	_, err := run("branch", "-D", name)
	return err
}

// IsRebaseInProgress returns true if a rebase is currently paused
// (e.g. waiting for conflict resolution).
func IsRebaseInProgress() bool {
	root, err := RepoRoot()
	if err != nil {
		return false
	}
	// Git stores rebase state in .git/rebase-merge or .git/rebase-apply
	for _, dir := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(root, ".git", dir)); err == nil {
			return true
		}
	}
	return false
}

// DiffNameOnly returns the list of files changed between two refs.
func DiffNameOnly(from, to string) ([]string, error) {
	out, err := run("diff", "--name-only", from+"..."+to)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// DiffFiles returns the diff between two refs for a specific set of files.
// The result is a patch suitable for git apply.
func DiffFiles(from, to string, files []string) (string, error) {
	args := []string{"diff", from + "..." + to, "--"}
	args = append(args, files...)
	out, err := run(args...)
	if err != nil {
		return "", err
	}
	// run() trims trailing whitespace; restore the trailing newline
	// that git diff produces, which git apply requires.
	if out != "" {
		out += "\n"
	}
	return out, nil
}

// LocalBranches returns the names of all local branches.
func LocalBranches() ([]string, error) {
	out, err := run("branch", "--format", "%(refname:short)")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// ApplyPatch applies a patch string using git apply --3way.
// The patch is written to a temp file and applied.
func ApplyPatch(patch string) error {
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

	_, err = run("apply", "--3way", f.Name())
	return err
}
