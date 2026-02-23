package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/testutil"
)

// resetSplitFlags resets cobra flag state between test runs.
func resetSplitFlags() {
	splitCmd.Flags().Set("from", "")
	splitCmd.Flags().Set("stack", "")
	splitCmd.Flags().Set("base", "")
	splitCmd.Flags().Set("dry-run", "false")
	splitCmd.Flags().Set("yes", "false")
	splitCmd.Flags().Set("no-push", "false")
}

// splitTestRepo sets up a temp git repo with a feature branch.
func splitTestRepo(t *testing.T) string {
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
		full := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(full), 0755)
		os.WriteFile(full, []byte(content), 0644)
	}

	git("init", "-b", "main")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "Test")
	git("config", "commit.gpgsign", "false")

	writeFile("README.md", "# test\n")
	git("add", ".")
	git("commit", "-m", "initial")

	git("checkout", "-b", "big-feature")

	writeFile("internal/git/helpers.go", "package git\nfunc Help() {}\n")
	writeFile("internal/stack/topology.go", "package stack\nfunc Topo() {}\n")
	git("add", ".")
	git("commit", "-m", "add helpers and topology")

	return dir
}

func TestSplitRequiresClaude(t *testing.T) {
	resetSplitFlags()
	splitTestRepo(t)

	testutil.SetBinary(t, &claudepkg.Binary, "/nonexistent/claude")

	err := RunSplit([]string{"--from", "big-feature", "--stack", "test", "--base", "main"})
	if err == nil {
		t.Fatal("expected error when Claude is not available")
	}
	if !strings.Contains(err.Error(), "AI agent") {
		t.Errorf("error should mention AI agent, got: %v", err)
	}
}

func TestSplitMissingFlags(t *testing.T) {
	resetSplitFlags()
	splitTestRepo(t)

	// Missing --from
	err := RunSplit([]string{"--stack", "test"})
	if err == nil {
		t.Fatal("expected error when --from is missing")
	}

	// Missing --stack
	resetSplitFlags()
	err = RunSplit([]string{"--from", "big-feature"})
	if err == nil {
		t.Fatal("expected error when --stack is missing")
	}
}

func TestSplitBranchNotExists(t *testing.T) {
	resetSplitFlags()
	splitTestRepo(t)

	// Use a fake Claude so the "requires AI" check passes
	testutil.SetBinary(t, &claudepkg.Binary, "true")

	err := RunSplit([]string{"--from", "nonexistent", "--stack", "test", "--base", "main"})
	if err == nil {
		t.Fatal("expected error for nonexistent branch")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error should mention branch not existing, got: %v", err)
	}
}

func TestSplitStackExists(t *testing.T) {
	resetSplitFlags()
	dir := splitTestRepo(t)

	testutil.SetBinary(t, &claudepkg.Binary, "true")

	// Create a stack with the same name
	stack.Init(dir, "test", "main")

	err := RunSplit([]string{"--from", "big-feature", "--stack", "test", "--base", "main"})
	if err == nil {
		t.Fatal("expected error for existing stack")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention stack exists, got: %v", err)
	}
}

func TestBuildSplitPRBody(t *testing.T) {
	s := &stack.Stack{
		StackID: "my-feature",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "my-feature/1-schema", Status: "open"},
			{Branch: "my-feature/2-api", Status: "open"},
			{Branch: "my-feature/3-ui", Status: "open"},
		},
	}

	body := buildSplitPRBody(s, 1, "big-feature")

	if !strings.Contains(body, "my-feature") {
		t.Error("body should contain stack name")
	}
	if !strings.Contains(body, "big-feature") {
		t.Error("body should reference original branch")
	}
	if !strings.Contains(body, "PR 2 of 3") {
		t.Errorf("body should say PR 2 of 3, got:\n%s", body)
	}
}

func TestSanitizeBranchComponent(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"git", "git"},
		{"My Feature", "my-feature"},
		{"internal/git", "internal-git"},
		{"", "changes"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeBranchComponent(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeBranchComponent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
