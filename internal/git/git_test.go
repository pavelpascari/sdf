package git

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// setupTestRepo creates a temporary git repository with an initial commit and
// changes the working directory into it. The original working directory is
// restored automatically via t.Cleanup.
func setupTestRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	// Save the original working directory so we can restore it after the test.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current directory: %v", err)
	}

	// Change into the temp dir. All git helpers use os/exec which inherits cwd.
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}
	t.Cleanup(func() {
		os.Chdir(origDir)
	})

	// Initialise a git repo with "main" as the default branch.
	mustRun(t, dir, "git", "init", "-b", "main")

	// Configure user identity (required for commits).
	mustRun(t, dir, "git", "config", "user.name", "Test User")
	mustRun(t, dir, "git", "config", "user.email", "test@example.com")

	// Disable commit signing (the environment may have it enabled globally).
	mustRun(t, dir, "git", "config", "commit.gpgsign", "false")

	// Create an initial commit so HEAD exists.
	writeFile(t, dir, "README.md", "# test repo\n")
	mustRun(t, dir, "git", "add", "README.md")
	mustRun(t, dir, "git", "commit", "-m", "initial commit")

	return dir
}

// mustRun runs an external command in the given directory and fails the test on error.
func mustRun(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %q failed: %v\noutput: %s", append([]string{name}, args...), err, string(out))
	}
	return strings.TrimSpace(string(out))
}

// writeFile creates or overwrites a file inside dir.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := dir + "/" + name
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCurrentBranch(t *testing.T) {
	setupTestRepo(t)

	branch, err := CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch() error: %v", err)
	}
	if branch != "main" {
		t.Errorf("expected branch 'main', got %q", branch)
	}
}

func TestIsClean(t *testing.T) {
	dir := setupTestRepo(t)

	// Repo should be clean right after the initial commit.
	clean, err := IsClean()
	if err != nil {
		t.Fatalf("IsClean() error: %v", err)
	}
	if !clean {
		t.Error("expected repo to be clean after initial commit")
	}

	// Modify a tracked file — repo should be dirty.
	writeFile(t, dir, "README.md", "# modified\n")
	clean, err = IsClean()
	if err != nil {
		t.Fatalf("IsClean() error after modification: %v", err)
	}
	if clean {
		t.Error("expected repo to be dirty after modifying a tracked file")
	}

	// Stage and commit the change — repo should be clean again.
	mustRun(t, dir, "git", "add", "README.md")
	mustRun(t, dir, "git", "commit", "-m", "update readme")
	clean, err = IsClean()
	if err != nil {
		t.Fatalf("IsClean() error after committing: %v", err)
	}
	if !clean {
		t.Error("expected repo to be clean after committing the change")
	}
}

func TestCreateBranchAndCheckout(t *testing.T) {
	setupTestRepo(t)

	// Create a new branch — should switch to it.
	if err := CreateBranch("feature-x"); err != nil {
		t.Fatalf("CreateBranch() error: %v", err)
	}
	branch, err := CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch() error: %v", err)
	}
	if branch != "feature-x" {
		t.Errorf("expected branch 'feature-x', got %q", branch)
	}

	// Checkout back to main.
	if err := Checkout("main"); err != nil {
		t.Fatalf("Checkout('main') error: %v", err)
	}
	branch, err = CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch() error: %v", err)
	}
	if branch != "main" {
		t.Errorf("expected branch 'main' after checkout, got %q", branch)
	}
}

func TestBranchExists(t *testing.T) {
	setupTestRepo(t)

	// "main" must exist.
	if !BranchExists("main") {
		t.Error("expected BranchExists('main') to be true")
	}

	// A nonexistent branch should not exist.
	if BranchExists("nonexistent-branch-xyz") {
		t.Error("expected BranchExists('nonexistent-branch-xyz') to be false")
	}

	// Create a branch and verify it exists.
	if err := CreateBranch("new-branch"); err != nil {
		t.Fatalf("CreateBranch() error: %v", err)
	}
	if !BranchExists("new-branch") {
		t.Error("expected BranchExists('new-branch') to be true after creation")
	}
}

func TestRevParse(t *testing.T) {
	setupTestRepo(t)

	sha, err := RevParse("HEAD")
	if err != nil {
		t.Fatalf("RevParse('HEAD') error: %v", err)
	}

	// A full SHA-1 hash is exactly 40 hex characters.
	matched, _ := regexp.MatchString(`^[0-9a-f]{40}$`, sha)
	if !matched {
		t.Errorf("expected a 40-char hex SHA, got %q", sha)
	}
}

func TestAddAndCommit(t *testing.T) {
	dir := setupTestRepo(t)

	// Create a new file.
	writeFile(t, dir, "hello.txt", "hello world\n")

	// Repo should be dirty (untracked file).
	clean, err := IsClean()
	if err != nil {
		t.Fatalf("IsClean() error: %v", err)
	}
	if clean {
		t.Error("expected repo to be dirty with an untracked file")
	}

	// Add and commit.
	if err := Add("hello.txt"); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if err := Commit("add hello"); err != nil {
		t.Fatalf("Commit() error: %v", err)
	}

	// Repo should be clean again.
	clean, err = IsClean()
	if err != nil {
		t.Fatalf("IsClean() error after commit: %v", err)
	}
	if !clean {
		t.Error("expected repo to be clean after Add + Commit")
	}
}

func TestMergeBase(t *testing.T) {
	dir := setupTestRepo(t)

	// Record the initial commit SHA — this will be the common ancestor.
	initialSHA := mustRun(t, dir, "git", "rev-parse", "HEAD")

	// Create branch-a from main with one commit.
	if err := CreateBranch("branch-a"); err != nil {
		t.Fatalf("CreateBranch('branch-a') error: %v", err)
	}
	writeFile(t, dir, "a.txt", "a\n")
	mustRun(t, dir, "git", "add", "a.txt")
	mustRun(t, dir, "git", "commit", "-m", "commit on branch-a")

	// Go back to main and create branch-b with one commit.
	if err := Checkout("main"); err != nil {
		t.Fatalf("Checkout('main') error: %v", err)
	}
	if err := CreateBranch("branch-b"); err != nil {
		t.Fatalf("CreateBranch('branch-b') error: %v", err)
	}
	writeFile(t, dir, "b.txt", "b\n")
	mustRun(t, dir, "git", "add", "b.txt")
	mustRun(t, dir, "git", "commit", "-m", "commit on branch-b")

	// The merge base of branch-a and branch-b should be the initial commit.
	base, err := MergeBase("branch-a", "branch-b")
	if err != nil {
		t.Fatalf("MergeBase() error: %v", err)
	}
	if base != initialSHA {
		t.Errorf("expected merge base %q, got %q", initialSHA, base)
	}
}

func TestCommitCount(t *testing.T) {
	dir := setupTestRepo(t)

	// Record the initial HEAD.
	initialSHA := mustRun(t, dir, "git", "rev-parse", "HEAD")

	// Make three additional commits.
	for i := 1; i <= 3; i++ {
		writeFile(t, dir, "file.txt", strings.Repeat("x", i)+"\n")
		mustRun(t, dir, "git", "add", "file.txt")
		mustRun(t, dir, "git", "commit", "-m", "commit "+string(rune('0'+i)))
	}

	count, err := CommitCount(initialSHA, "HEAD")
	if err != nil {
		t.Fatalf("CommitCount() error: %v", err)
	}
	if count != "3" {
		t.Errorf("expected commit count '3', got %q", count)
	}
}

func TestRebaseOnto(t *testing.T) {
	dir := setupTestRepo(t)

	// Record the initial commit.
	initialSHA := mustRun(t, dir, "git", "rev-parse", "HEAD")

	// Add a commit on main that will be the new base.
	writeFile(t, dir, "main-file.txt", "main work\n")
	mustRun(t, dir, "git", "add", "main-file.txt")
	mustRun(t, dir, "git", "commit", "-m", "main work")
	newBaseSHA := mustRun(t, dir, "git", "rev-parse", "HEAD")

	// Create a feature branch from the initial commit with two commits.
	mustRun(t, dir, "git", "checkout", "-b", "feature", initialSHA)
	writeFile(t, dir, "feat1.txt", "feature 1\n")
	mustRun(t, dir, "git", "add", "feat1.txt")
	mustRun(t, dir, "git", "commit", "-m", "feature commit 1")

	writeFile(t, dir, "feat2.txt", "feature 2\n")
	mustRun(t, dir, "git", "add", "feat2.txt")
	mustRun(t, dir, "git", "commit", "-m", "feature commit 2")

	// Rebase feature onto main: git rebase --onto <newBase> <oldBase> feature
	// oldBase = initialSHA (the point where feature diverged from main)
	err := RebaseOnto(newBaseSHA, initialSHA, "feature")
	if err != nil {
		t.Fatalf("RebaseOnto() error: %v", err)
	}

	// After rebase, feature should be checked out.
	branch, err := CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch() error: %v", err)
	}
	if branch != "feature" {
		t.Errorf("expected to be on branch 'feature' after rebase, got %q", branch)
	}

	// The feature branch should now have 2 commits on top of newBaseSHA.
	count, err := CommitCount(newBaseSHA, "HEAD")
	if err != nil {
		t.Fatalf("CommitCount() error: %v", err)
	}
	if count != "2" {
		t.Errorf("expected 2 commits after rebase, got %q", count)
	}

	// main-file.txt should be present (inherited from newBase).
	if _, statErr := os.Stat(dir + "/main-file.txt"); statErr != nil {
		t.Error("expected main-file.txt to exist after rebase onto main")
	}

	// feat1.txt and feat2.txt should both be present.
	if _, statErr := os.Stat(dir + "/feat1.txt"); statErr != nil {
		t.Error("expected feat1.txt to exist after rebase")
	}
	if _, statErr := os.Stat(dir + "/feat2.txt"); statErr != nil {
		t.Error("expected feat2.txt to exist after rebase")
	}
}

func TestConflictedFilesAndRebaseAbort(t *testing.T) {
	dir := setupTestRepo(t)

	// Record the initial commit SHA.
	initialSHA := mustRun(t, dir, "git", "rev-parse", "HEAD")

	// On main, modify README.md one way.
	writeFile(t, dir, "README.md", "main version\n")
	mustRun(t, dir, "git", "add", "README.md")
	mustRun(t, dir, "git", "commit", "-m", "main change to README")

	// Create a feature branch from the initial commit and modify README.md differently.
	mustRun(t, dir, "git", "checkout", "-b", "conflict-branch", initialSHA)
	writeFile(t, dir, "README.md", "feature version\n")
	mustRun(t, dir, "git", "add", "README.md")
	mustRun(t, dir, "git", "commit", "-m", "feature change to README")

	// Attempt to rebase conflict-branch onto main — this should produce a conflict.
	err := RebaseOnto("main", initialSHA, "conflict-branch")
	if err == nil {
		t.Fatal("expected RebaseOnto to fail due to conflict, but it succeeded")
	}

	// ConflictedFiles should report README.md.
	files, err := ConflictedFiles()
	if err != nil {
		t.Fatalf("ConflictedFiles() error: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected at least one conflicted file, got none")
	}

	found := false
	for _, f := range files {
		if f == "README.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'README.md' in conflicted files, got %v", files)
	}

	// Abort the rebase so the repo is left in a clean state.
	if err := RebaseAbort(); err != nil {
		t.Fatalf("RebaseAbort() error: %v", err)
	}

	// After abort we should be back on conflict-branch.
	branch, err := CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch() error: %v", err)
	}
	if branch != "conflict-branch" {
		t.Errorf("expected to be on 'conflict-branch' after abort, got %q", branch)
	}
}

func TestRepoRoot(t *testing.T) {
	dir := setupTestRepo(t)

	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot() error: %v", err)
	}

	// Resolve symlinks for both paths so the comparison is reliable
	// (t.TempDir() may return a path through /tmp which is a symlink on some
	// systems, e.g. macOS /tmp -> /private/tmp).
	resolvedDir, err := resolveReal(dir)
	if err != nil {
		t.Fatalf("failed to resolve test dir: %v", err)
	}
	resolvedRoot, err := resolveReal(root)
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}

	if resolvedRoot != resolvedDir {
		t.Errorf("expected repo root %q, got %q", resolvedDir, resolvedRoot)
	}
}

// resolveReal resolves symlinks so that paths through /tmp symlinks compare equal.
func resolveReal(path string) (string, error) {
	out, err := exec.Command("readlink", "-f", path).Output()
	if err != nil {
		return path, err
	}
	return strings.TrimSpace(string(out)), nil
}

func TestDiffSummary(t *testing.T) {
	dir := setupTestRepo(t)

	fromSHA := mustRun(t, dir, "git", "rev-parse", "HEAD")

	writeFile(t, dir, "newfile.txt", "some content\n")
	mustRun(t, dir, "git", "add", "newfile.txt")
	mustRun(t, dir, "git", "commit", "-m", "add newfile")

	toSHA := mustRun(t, dir, "git", "rev-parse", "HEAD")

	summary, err := DiffSummary(fromSHA, toSHA)
	if err != nil {
		t.Fatalf("DiffSummary() error: %v", err)
	}
	if !strings.Contains(summary, "newfile.txt") {
		t.Errorf("expected DiffSummary to mention 'newfile.txt', got %q", summary)
	}
}

func TestLog(t *testing.T) {
	dir := setupTestRepo(t)

	fromSHA := mustRun(t, dir, "git", "rev-parse", "HEAD")

	writeFile(t, dir, "log-test.txt", "data\n")
	mustRun(t, dir, "git", "add", "log-test.txt")
	mustRun(t, dir, "git", "commit", "-m", "log test commit")

	logOutput, err := Log(fromSHA, "HEAD")
	if err != nil {
		t.Fatalf("Log() error: %v", err)
	}
	if !strings.Contains(logOutput, "log test commit") {
		t.Errorf("expected log to contain 'log test commit', got %q", logOutput)
	}
}
