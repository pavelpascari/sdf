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
