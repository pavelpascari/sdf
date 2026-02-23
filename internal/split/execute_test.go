package split

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

// testRepoForExec creates a temp repo with main branch and a feature branch
// that has changes in 3 directories: src/ (2 files), lib/ (1 file), cmd/ (1 file).
func testRepoForExec(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), out)
		}
	}

	write := func(name, content string) {
		t.Helper()
		full := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(full), 0755)
		os.WriteFile(full, []byte(content), 0644)
	}

	git("init", "-b", "main")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "Test")
	git("config", "commit.gpgsign", "false")

	write("README.md", "# test\n")
	git("add", ".")
	git("commit", "-m", "initial")

	git("checkout", "-b", "feature")

	write("src/foo.go", "package src\nfunc Foo() {}\n")
	write("src/bar.go", "package src\nfunc Bar() {}\n")
	write("lib/util.go", "package lib\nfunc Util() {}\n")
	write("cmd/main.go", "package main\nfunc main() {}\n")
	git("add", ".")
	git("commit", "-m", "add all files")

	return dir
}

func TestExecute_ThreeLayers(t *testing.T) {
	dir := testRepoForExec(t)

	plan := &Plan{
		Layers: []Layer{
			{Name: "core-lib", Description: "Core library utilities", Files: []string{"lib/util.go"}},
			{Name: "src-logic", Description: "Source logic", Files: []string{"src/foo.go", "src/bar.go"}},
			{Name: "cli", Description: "CLI entry point", Files: []string{"cmd/main.go"}},
		},
	}

	branches, err := Execute(plan, "test-stack", "main", "feature", dir)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(branches) != 3 {
		t.Fatalf("expected 3 branches, got %d", len(branches))
	}

	// Tree identity: last branch should match feature
	lastBranch := branches[len(branches)-1]
	diff, err := gitpkg.DiffFull("feature", lastBranch)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if diff != "" {
		t.Errorf("tree identity failed — diff:\n%s", diff)
	}

	// Stack topology
	s, err := stack.LoadStack(dir, "test-stack")
	if err != nil {
		t.Fatalf("load stack: %v", err)
	}

	if len(s.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(s.Nodes))
	}
	if s.Base != "main" {
		t.Errorf("stack base: got %q, want %q", s.Base, "main")
	}
	if s.ParentBranch(s.Nodes[0].Branch) != "main" {
		t.Errorf("node 0 parent: got %q, want %q", s.ParentBranch(s.Nodes[0].Branch), "main")
	}
	if s.ParentBranch(s.Nodes[1].Branch) != s.Nodes[0].Branch {
		t.Errorf("node 1 parent: got %q, want %q", s.ParentBranch(s.Nodes[1].Branch), s.Nodes[0].Branch)
	}
	if s.ParentBranch(s.Nodes[2].Branch) != s.Nodes[1].Branch {
		t.Errorf("node 2 parent: got %q, want %q", s.ParentBranch(s.Nodes[2].Branch), s.Nodes[1].Branch)
	}
}

func TestExecute_TreeIdentity(t *testing.T) {
	testRepoForExec(t)

	// All files in one layer — trivial case
	plan := &Plan{
		Layers: []Layer{
			{Name: "everything", Description: "All changes", Files: []string{
				"src/foo.go", "src/bar.go", "lib/util.go", "cmd/main.go",
			}},
		},
	}

	branches, err := Execute(plan, "single-stack", "main", "feature", ".")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	diff, err := gitpkg.DiffFull("feature", branches[0])
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if diff != "" {
		t.Errorf("single-layer tree identity failed:\n%s", diff)
	}
}

// testRepoForHunks creates a temp repo where one file has two independent
// regions of change, suitable for hunk-level splitting.
func testRepoForHunks(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), out)
		}
	}

	write := func(name, content string) {
		t.Helper()
		full := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(full), 0755)
		os.WriteFile(full, []byte(content), 0644)
	}

	git("init", "-b", "main")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "Test")
	git("config", "commit.gpgsign", "false")

	// Create a file with two functions separated by enough context for 2 hunks.
	// Git uses 3 lines of context, so we need >6 unchanged lines between changes.
	write("shared.go", `package main

func Alpha() string {
	return "alpha"
}

// line 1 of separator
// line 2 of separator
// line 3 of separator
// line 4 of separator
// line 5 of separator
// line 6 of separator
// line 7 of separator
// line 8 of separator

func Beta() string {
	return "beta"
}
`)
	write("only-a.go", "package main\n")
	write("only-b.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "initial")

	git("checkout", "-b", "feature")

	// Modify two separate regions of shared.go + modify only-a.go and only-b.go
	write("shared.go", `package main

func Alpha() string {
	return "alpha-modified"
}

// line 1 of separator
// line 2 of separator
// line 3 of separator
// line 4 of separator
// line 5 of separator
// line 6 of separator
// line 7 of separator
// line 8 of separator

func Beta() string {
	return "beta-modified"
}
`)
	write("only-a.go", "package main\nfunc A() {}\n")
	write("only-b.go", "package main\nfunc B() {}\n")
	git("add", ".")
	git("commit", "-m", "modify all")

	return dir
}

func TestExecute_WithPartialFiles(t *testing.T) {
	dir := testRepoForHunks(t)

	// First, parse the diff to know hunk indices
	diff, err := gitpkg.DiffFiles("main", "feature", []string{"shared.go"})
	if err != nil {
		t.Fatalf("DiffFiles: %v", err)
	}
	fileDiffs := ParseDiff(diff)
	if len(fileDiffs) != 1 {
		t.Fatalf("expected 1 file diff, got %d", len(fileDiffs))
	}
	if len(fileDiffs[0].Hunks) < 2 {
		t.Fatalf("expected at least 2 hunks, got %d", len(fileDiffs[0].Hunks))
	}

	plan := &Plan{
		Layers: []Layer{
			{
				Name:        "layer-a",
				Description: "Alpha changes",
				Files:       []string{"only-a.go"},
				PartialFiles: []PartialFile{
					{Path: "shared.go", Hunks: []int{0}},
				},
			},
			{
				Name:        "layer-b",
				Description: "Beta changes",
				Files:       []string{"only-b.go"},
				PartialFiles: []PartialFile{
					{Path: "shared.go", Hunks: []int{1}},
				},
			},
		},
	}

	branches, err := Execute(plan, "hunk-stack", "main", "feature", dir)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(branches) != 2 {
		t.Fatalf("expected 2 branches, got %d", len(branches))
	}

	// Tree identity: last branch should match feature
	lastBranch := branches[len(branches)-1]
	treeErr := ValidateTree("feature", lastBranch)
	if treeErr != nil {
		diffOut, _ := gitpkg.DiffFull("feature", lastBranch)
		t.Fatalf("tree identity failed: %v\ndiff:\n%s", treeErr, diffOut)
	}
}

func TestValidateTree(t *testing.T) {
	testRepoForExec(t)

	// Same ref should pass
	if err := ValidateTree("feature", "feature"); err != nil {
		t.Errorf("same ref should pass: %v", err)
	}

	// Different refs should fail
	if err := ValidateTree("main", "feature"); err == nil {
		t.Error("different refs should fail")
	}
}
