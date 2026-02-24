package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
)

// initTestRepo sets up a minimal git repo with a main branch for testing
// RunInit. The caller is chdir'd into the repo.
func initTestRepo(t *testing.T) string {
	t.Helper()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
	}

	git("init", "-b", "main")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "Test")
	git("config", "commit.gpgsign", "false")

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git("add", "README.md")
	git("commit", "-m", "initial")

	return dir
}

// currentBranch returns the current git branch in the given directory.
func currentBranch(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse: %s", string(out))
	}
	return strings.TrimSpace(string(out))
}

func TestInit_CreatesBranchWithStackName(t *testing.T) {
	dir := initTestRepo(t)

	if err := RunInit([]string{"--base", "main", "my-feature"}); err != nil {
		t.Fatal(err)
	}

	// Should be on the new branch, not main
	branch := currentBranch(t, dir)
	if branch == "main" {
		t.Error("expected to be on a new branch, still on main")
	}

	// Branch name should have prefix applied (default: stack-id/stack-id)
	if branch != "my-feature/my-feature" {
		t.Errorf("expected branch my-feature/my-feature, got %s", branch)
	}

	// Stack should have one node
	s, err := stack.LoadStack(dir, "my-feature")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(s.Nodes))
	}
	if s.Nodes[0].Branch != "my-feature/my-feature" {
		t.Errorf("expected node branch my-feature/my-feature, got %s", s.Nodes[0].Branch)
	}
	if s.Nodes[0].Status != "open" {
		t.Errorf("expected status open, got %s", s.Nodes[0].Status)
	}
	if s.Nodes[0].BaseTip == "" {
		t.Error("expected BaseTip to be set")
	}
}

func TestInit_CreatesBranchWithCustomName(t *testing.T) {
	dir := initTestRepo(t)

	if err := RunInit([]string{"--base", "main", "--branch", "db-schema", "my-feature"}); err != nil {
		t.Fatal(err)
	}

	branch := currentBranch(t, dir)
	// With prefix: my-feature/db-schema
	if branch != "my-feature/db-schema" {
		t.Errorf("expected branch my-feature/db-schema, got %s", branch)
	}

	s, err := stack.LoadStack(dir, "my-feature")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(s.Nodes))
	}
	if s.Nodes[0].Branch != "my-feature/db-schema" {
		t.Errorf("expected node branch my-feature/db-schema, got %s", s.Nodes[0].Branch)
	}
}

func TestInit_RejectsExistingStack(t *testing.T) {
	initTestRepo(t)

	if err := RunInit([]string{"--base", "main", "my-feature"}); err != nil {
		t.Fatal(err)
	}

	// Second init with same name should fail
	err := RunInit([]string{"--base", "main", "my-feature"})
	if err == nil {
		t.Fatal("expected error for duplicate stack name")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %s", err)
	}
}

func TestInit_JSONOutput(t *testing.T) {
	initTestRepo(t)

	output, err := RunInitWithOutput([]string{"--base", "main", "--json", "my-feature"})
	if err != nil {
		t.Fatal(err)
	}

	var result InitResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("expected valid JSON, got parse error: %v\noutput: %s", err, output)
	}

	if result.Stack != "my-feature" {
		t.Errorf("expected stack my-feature, got %s", result.Stack)
	}
	if result.Base != "main" {
		t.Errorf("expected base main, got %s", result.Base)
	}
	if result.Branch != "my-feature/my-feature" {
		t.Errorf("expected branch my-feature/my-feature, got %s", result.Branch)
	}
}

func TestInit_JSONOutputWithCustomBranch(t *testing.T) {
	initTestRepo(t)

	output, err := RunInitWithOutput([]string{"--base", "main", "--branch", "db-schema", "--json", "my-feature"})
	if err != nil {
		t.Fatal(err)
	}

	var result InitResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("expected valid JSON, got parse error: %v\noutput: %s", err, output)
	}

	if result.Branch != "my-feature/db-schema" {
		t.Errorf("expected branch my-feature/db-schema, got %s", result.Branch)
	}
}

func TestInit_PreservesExistingConfig(t *testing.T) {
	dir := initTestRepo(t)

	// Pre-create .sdf directory and a custom config before running init.
	sdfDir := filepath.Join(dir, ".sdf")
	if err := os.MkdirAll(sdfDir, 0755); err != nil {
		t.Fatal(err)
	}
	customCfg := `{
  "branch_prefix": {
    "enabled": false,
    "scope": "custom",
    "separator": "-"
  }
}
`
	cfgPath := filepath.Join(sdfDir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(customCfg), 0644); err != nil {
		t.Fatal(err)
	}

	// Run init via runInitCore directly (avoids cobra flag state leaking
	// between tests via the global rootCmd).
	if _, err := runInitCore("my-feature", "main", "", false); err != nil {
		t.Fatal(err)
	}

	// Read back the config and verify it was preserved.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("cannot read config after init: %v", err)
	}

	var got struct {
		BranchPrefix struct {
			Enabled   *bool  `json:"enabled"`
			Scope     string `json:"scope"`
			Separator string `json:"separator"`
		} `json:"branch_prefix"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("cannot parse config after init: %v", err)
	}

	if got.BranchPrefix.Enabled == nil || *got.BranchPrefix.Enabled != false {
		t.Error("expected enabled=false to be preserved, got overwritten")
	}
	if got.BranchPrefix.Scope != "custom" {
		t.Errorf("expected scope 'custom' to be preserved, got %q", got.BranchPrefix.Scope)
	}
	if got.BranchPrefix.Separator != "-" {
		t.Errorf("expected separator '-' to be preserved, got %q", got.BranchPrefix.Separator)
	}

	// With prefix disabled, branch name should be bare (no prefix applied).
	branch := currentBranch(t, dir)
	if branch != "my-feature" {
		t.Errorf("expected bare branch 'my-feature' (prefix disabled), got %s", branch)
	}
}

func TestInit_JSONOutputNotPushed(t *testing.T) {
	// Test repo has no origin, so pushed should be false
	initTestRepo(t)

	output, err := RunInitWithOutput([]string{"--base", "main", "--json", "my-feature"})
	if err != nil {
		t.Fatal(err)
	}

	var result InitResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("expected valid JSON: %v", err)
	}

	if result.Pushed {
		t.Error("expected pushed=false for repo without origin")
	}
}
