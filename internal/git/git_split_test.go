package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestRepo creates a temp git repo with a main branch and a feature branch.
// Feature branch has changes in two files across two directories.
// Returns the repo dir. Caller must os.Chdir back.
func setupTestRepo(t *testing.T) string {
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
	git("add", ".")
	git("commit", "-m", "add src and lib files")

	return dir
}

func TestDiffNameOnly(t *testing.T) {
	setupTestRepo(t)

	files, err := DiffNameOnly("main", "feature")
	if err != nil {
		t.Fatalf("DiffNameOnly: %v", err)
	}

	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d: %v", len(files), files)
	}

	want := map[string]bool{"src/foo.go": true, "src/bar.go": true, "lib/util.go": true}
	for _, f := range files {
		if !want[f] {
			t.Errorf("unexpected file: %s", f)
		}
	}
}

func TestDiffFiles(t *testing.T) {
	setupTestRepo(t)

	// Get diff for only src/ files
	patch, err := DiffFiles("main", "feature", []string{"src/foo.go", "src/bar.go"})
	if err != nil {
		t.Fatalf("DiffFiles: %v", err)
	}

	if !strings.Contains(patch, "src/foo.go") {
		t.Error("patch should contain src/foo.go")
	}
	if !strings.Contains(patch, "src/bar.go") {
		t.Error("patch should contain src/bar.go")
	}
	if strings.Contains(patch, "lib/util.go") {
		t.Error("patch should NOT contain lib/util.go")
	}
}

func TestApplyPatch(t *testing.T) {
	setupTestRepo(t)

	// Get patch for src/ files
	patch, err := DiffFiles("main", "feature", []string{"src/foo.go"})
	if err != nil {
		t.Fatalf("DiffFiles: %v", err)
	}

	// Go back to main and apply
	if err := Checkout("main"); err != nil {
		t.Fatalf("checkout main: %v", err)
	}

	if err := ApplyPatch(patch); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}

	// Verify file exists
	content, err := os.ReadFile("src/foo.go")
	if err != nil {
		t.Fatalf("src/foo.go should exist after apply: %v", err)
	}
	if !strings.Contains(string(content), "Foo") {
		t.Error("src/foo.go should contain Foo()")
	}
}
