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

// branchTestRepo sets up a temporary git repository with an SDF stack.
// The repo has: main (base) ← branchA [a1] ← branchB [b1]
// If thirdBranch is true, also: ← branchC [c1]
// HEAD is left on the last branch.
func branchTestRepo(t *testing.T, thirdBranch bool) string {
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
	os.MkdirAll(filepath.Join(dir, ".sdf", "stacks"), 0755)
	git("add", "README.md")
	git("commit", "-m", "initial")

	mainTip := git("rev-parse", "HEAD")

	// branchA
	git("checkout", "-b", "branchA")
	writeFile("a1.txt", "a1\n")
	git("add", "a1.txt")
	git("commit", "-m", "a1")
	branchATip := git("rev-parse", "HEAD")

	// branchB
	git("checkout", "-b", "branchB")
	writeFile("b1.txt", "b1\n")
	git("add", "b1.txt")
	git("commit", "-m", "b1")
	branchBTip := git("rev-parse", "HEAD")

	nodes := []stack.Node{
		{Branch: "branchA", Status: "open", BaseTip: mainTip},
		{Branch: "branchB", Status: "open", BaseTip: branchATip},
	}

	if thirdBranch {
		git("checkout", "-b", "branchC")
		writeFile("c1.txt", "c1\n")
		git("add", "c1.txt")
		git("commit", "-m", "c1")

		nodes = append(nodes, stack.Node{
			Branch:  "branchC",
			Status:  "open",
			BaseTip: branchBTip,
		})
	}

	s := &stack.Stack{
		StackID: "test-stack",
		Base:    "main",
		Nodes:   nodes,
	}
	if err := stack.Save(dir, s); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestRunBranch_InsertMiddle(t *testing.T) {
	dir := branchTestRepo(t, true) // [A, B, C], HEAD on C

	// Switch to branchB — insert point
	gitpkg.Checkout("branchB")

	// Insert "newbranch" after B
	if err := RunBranch([]string{"--no-prefix", "newbranch"}); err != nil {
		t.Fatalf("RunBranch failed: %v", err)
	}

	// Load stack and verify order
	s, err := stack.LoadStack(dir, "test-stack")
	if err != nil {
		t.Fatal(err)
	}

	if len(s.Nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(s.Nodes))
	}

	expected := []string{"branchA", "branchB", "newbranch", "branchC"}
	for i, want := range expected {
		if s.Nodes[i].Branch != want {
			t.Errorf("node %d: expected %s, got %s", i, want, s.Nodes[i].Branch)
		}
	}

	// Verify git branch exists
	if !gitpkg.BranchExists("newbranch") {
		t.Error("git branch 'newbranch' does not exist")
	}

	// Verify we're on the new branch
	current, _ := gitpkg.CurrentBranch()
	if current != "newbranch" {
		t.Errorf("expected to be on newbranch, got %s", current)
	}
}

func TestRunBranch_InsertAfterFirst(t *testing.T) {
	dir := branchTestRepo(t, false) // [A, B], HEAD on B

	// Switch to branchA — insert between A and B
	gitpkg.Checkout("branchA")

	if err := RunBranch([]string{"--no-prefix", "inserted"}); err != nil {
		t.Fatalf("RunBranch failed: %v", err)
	}

	s, err := stack.LoadStack(dir, "test-stack")
	if err != nil {
		t.Fatal(err)
	}

	if len(s.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(s.Nodes))
	}

	expected := []string{"branchA", "inserted", "branchB"}
	for i, want := range expected {
		if s.Nodes[i].Branch != want {
			t.Errorf("node %d: expected %s, got %s", i, want, s.Nodes[i].Branch)
		}
	}
}

func TestRunBranch_AppendOnLast(t *testing.T) {
	dir := branchTestRepo(t, false) // [A, B], HEAD on B

	// Already on branchB (the last node) — should append
	if err := RunBranch([]string{"--no-prefix", "appended"}); err != nil {
		t.Fatalf("RunBranch failed: %v", err)
	}

	s, err := stack.LoadStack(dir, "test-stack")
	if err != nil {
		t.Fatal(err)
	}

	if len(s.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(s.Nodes))
	}

	expected := []string{"branchA", "branchB", "appended"}
	for i, want := range expected {
		if s.Nodes[i].Branch != want {
			t.Errorf("node %d: expected %s, got %s", i, want, s.Nodes[i].Branch)
		}
	}
}

func TestRunBranch_AppendOnBase(t *testing.T) {
	dir := branchTestRepo(t, false) // [A, B], HEAD on B

	// Switch to main (the base) — should append to end
	gitpkg.Checkout("main")

	if err := RunBranch([]string{"--no-prefix", "frombase"}); err != nil {
		t.Fatalf("RunBranch failed: %v", err)
	}

	s, err := stack.LoadStack(dir, "test-stack")
	if err != nil {
		t.Fatal(err)
	}

	if len(s.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(s.Nodes))
	}

	expected := []string{"branchA", "branchB", "frombase"}
	for i, want := range expected {
		if s.Nodes[i].Branch != want {
			t.Errorf("node %d: expected %s, got %s", i, want, s.Nodes[i].Branch)
		}
	}
}

func TestRunBranch_DuplicateNameIsIdempotent(t *testing.T) {
	dir := branchTestRepo(t, false) // [branchA, branchB]

	// Re-adding an existing branch is idempotent: no error, no duplicate node.
	if err := RunBranch([]string{"--no-prefix", "branchA"}); err != nil {
		t.Fatalf("re-adding an existing branch must be idempotent, got %v", err)
	}

	s, err := stack.LoadStack(dir, "test-stack")
	if err != nil {
		t.Fatal(err)
	}

	// Verify exactly one branchA node (no duplicate)
	count := 0
	for _, n := range s.Nodes {
		if n.Branch == "branchA" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("branchA appears %d times, want 1 (no duplicate)", count)
	}
}
