package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

// testRepo sets up a temporary git repository with an SDF stack and returns
// the repo path plus a cleanup function. The repo has:
//
//	main (base) ← branchA [a1, a2] ← branchB [b1, b2, b3]
//
// The caller is chdir'd into the repo. cleanup restores the original directory.
func testRepo(t *testing.T) (repoDir string, shas map[string]string) {
	t.Helper()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		os.Chdir(origDir)
	})

	shas = make(map[string]string)

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	writeFile := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Initialize repo
	git("init", "-b", "main")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "Test")
	git("config", "commit.gpgsign", "false")

	// Initial commit on main
	writeFile("README.md", "# test\n")
	writeFile(".gitignore", ".sdf/\n")
	sdfDir := filepath.Join(dir, ".sdf")
	os.MkdirAll(filepath.Join(sdfDir, "stacks"), 0755)
	git("add", "README.md", ".gitignore")
	git("commit", "-m", "initial")

	mainTip := git("rev-parse", "HEAD")

	// Create branchA with 2 commits
	git("checkout", "-b", "branchA")
	writeFile("a1.txt", "a1\n")
	git("add", "a1.txt")
	git("commit", "-m", "a1")
	shas["a1"] = git("rev-parse", "HEAD")

	writeFile("a2.txt", "a2\n")
	git("add", "a2.txt")
	git("commit", "-m", "a2")
	shas["a2"] = git("rev-parse", "HEAD")

	branchATip := shas["a2"]

	// Create branchB with 3 commits
	git("checkout", "-b", "branchB")
	writeFile("b1.txt", "b1\n")
	git("add", "b1.txt")
	git("commit", "-m", "b1")
	shas["b1"] = git("rev-parse", "HEAD")

	writeFile("b2.txt", "b2\n")
	git("add", "b2.txt")
	git("commit", "-m", "b2")
	shas["b2"] = git("rev-parse", "HEAD")

	writeFile("b3.txt", "b3\n")
	git("add", "b3.txt")
	git("commit", "-m", "b3")
	shas["b3"] = git("rev-parse", "HEAD")

	// Write SDF stack definition (local state, not committed)
	s := &stack.Stack{
		StackID: "test-stack",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "branchA", Status: "open", BaseTip: mainTip},
			{Branch: "branchB", Status: "open", BaseTip: branchATip},
		},
	}
	if err := stack.Save(dir, s); err != nil {
		t.Fatal(err)
	}

	return dir, shas
}

func TestRunMove_SingleCommit(t *testing.T) {
	dir, shas := testRepo(t)

	// We're on branchB. Move b1 to branchA.
	if err := RunMove([]string{shas["b1"]}); err != nil {
		t.Fatalf("RunMove failed: %v", err)
	}

	// Verify we're back on branchB
	branch, _ := gitpkg.CurrentBranch()
	if branch != "branchB" {
		t.Errorf("expected to be on branchB, got %s", branch)
	}

	// Verify branchA now has the b1 commit's content
	if err := gitpkg.Checkout("branchA"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b1.txt")); err != nil {
		t.Error("b1.txt should exist on branchA after move")
	}

	// Verify branchB no longer has b1 as a separate commit but still has b2, b3
	if err := gitpkg.Checkout("branchB"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b2.txt")); err != nil {
		t.Error("b2.txt should still exist on branchB")
	}
	if _, err := os.Stat(filepath.Join(dir, "b3.txt")); err != nil {
		t.Error("b3.txt should still exist on branchB")
	}
	// b1.txt should still be reachable (it's in the parent now)
	if _, err := os.Stat(filepath.Join(dir, "b1.txt")); err != nil {
		t.Error("b1.txt should be reachable on branchB (inherited from parent)")
	}

	// Verify stack was updated
	s, err := stack.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	parentTip, _ := gitpkg.RevParse("branchA")
	if s.Nodes[1].BaseTip != parentTip {
		t.Errorf("branchB BaseTip not updated: got %s, want %s", s.Nodes[1].BaseTip, parentTip)
	}
}

func TestRunMove_MultipleContiguous(t *testing.T) {
	dir, shas := testRepo(t)

	// Move b1 and b2 from branchB to branchA
	if err := RunMove([]string{shas["b1"], shas["b2"]}); err != nil {
		t.Fatalf("RunMove failed: %v", err)
	}

	// Verify branchA has both files
	if err := gitpkg.Checkout("branchA"); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"b1.txt", "b2.txt"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s should exist on branchA after move", f)
		}
	}

	// branchB should still have b3 and inherited b1,b2
	if err := gitpkg.Checkout("branchB"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b3.txt")); err != nil {
		t.Error("b3.txt should still exist on branchB")
	}

	// Verify commit count: branchB should have at least 1 own commit above branchA
	parentTip, _ := gitpkg.RevParse("branchA")
	commits, err := gitpkg.LogCommits(parentTip, "branchB")
	if err != nil {
		t.Fatal(err)
	}
	ownCommits := 0
	for range commits {
		ownCommits++
	}
	if ownCommits < 1 {
		t.Errorf("branchB should have at least 1 commit above branchA, got %d", ownCommits)
	}
}

func TestRunMove_ErrorAllCommits(t *testing.T) {
	_, shas := testRepo(t)

	// branchB has commits: b1, b2, b3 above branchA.
	// Moving all of them should fail.
	err := RunMove([]string{shas["b1"], shas["b2"], shas["b3"]})
	if err == nil {
		t.Fatal("expected error when moving all commits, got nil")
	}
	if !strings.Contains(err.Error(), "would become empty") {
		t.Errorf("expected 'would become empty' error, got: %v", err)
	}
}

func TestRunMove_ErrorCommitNotOnBranch(t *testing.T) {
	_, shas := testRepo(t)

	// Try to move a1 (which is on branchA, not branchB's own commits)
	err := RunMove([]string{shas["a1"]})
	if err == nil {
		t.Fatal("expected error for commit not on branch, got nil")
	}
	if !strings.Contains(err.Error(), "is not on branch") {
		t.Errorf("expected 'is not on branch' error, got: %v", err)
	}
}

func TestRunMove_ErrorNonContiguous(t *testing.T) {
	_, shas := testRepo(t)

	// Try to move b1 and b3 (skipping b2) — should fail
	err := RunMove([]string{shas["b1"], shas["b3"]})
	if err == nil {
		t.Fatal("expected error for non-contiguous commits, got nil")
	}
	if !strings.Contains(err.Error(), "contiguous") {
		t.Errorf("expected contiguous error, got: %v", err)
	}
}

func TestRunMove_ErrorDirtyWorkingTree(t *testing.T) {
	dir, shas := testRepo(t)

	// Dirty the working tree by modifying a tracked file
	os.WriteFile(filepath.Join(dir, "a1.txt"), []byte("modified\n"), 0644)

	err := RunMove([]string{shas["b1"]})
	if err == nil {
		t.Fatal("expected error for dirty working tree, got nil")
	}
	if !strings.Contains(err.Error(), "not clean") {
		t.Errorf("expected 'not clean' error, got: %v", err)
	}
}

func TestRunMove_CascadeRebase(t *testing.T) {
	dir, shas := testRepo(t)

	// Add a third branch (branchC) on top of branchB
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	branchBTip := git("rev-parse", "HEAD")

	git("checkout", "-b", "branchC")
	os.WriteFile(filepath.Join(dir, "c1.txt"), []byte("c1\n"), 0644)
	git("add", "c1.txt")
	git("commit", "-m", "c1")

	// Update stack to include branchC (local state only)
	s, _ := stack.Load(dir)
	s.Nodes = append(s.Nodes, stack.Node{
		Branch:  "branchC",
		Status:  "open",
		BaseTip: branchBTip,
	})
	stack.Save(dir, s)

	// Switch back to branchB for the move
	git("checkout", "branchB")

	// Move b1 from branchB to branchA
	if err := RunMove([]string{shas["b1"]}); err != nil {
		t.Fatalf("RunMove with cascade failed: %v", err)
	}

	// Verify stack still has branchC and is consistent
	s, err := stack.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cNode := s.FindNode("branchC")
	if cNode == nil {
		t.Fatal("branchC should still be in stack")
	}

	// If branchB's tip changed after the move, branchC's BaseTip should
	// have been updated by the cascade rebase. If the rebase was a no-op
	// (e.g. cherry-pick produced an identical commit), no cascade is needed.
	newBranchBTip, _ := gitpkg.RevParse("branchB")
	if newBranchBTip != branchBTip && cNode.BaseTip == branchBTip {
		t.Error("branchB tip changed but branchC BaseTip was not updated by cascade rebase")
	}

	// Verify branchC still has c1.txt and all inherited files
	if err := gitpkg.Checkout("branchC"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "c1.txt")); err != nil {
		t.Error("c1.txt should still exist on branchC after cascade rebase")
	}
	if _, err := os.Stat(filepath.Join(dir, "b1.txt")); err != nil {
		t.Error("b1.txt should be reachable on branchC (inherited through parent chain)")
	}
}
