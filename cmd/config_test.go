package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	"github.com/pavelpascari/sdf/internal/stack"
)

func TestConfigSet_WarnsPrefixMismatch(t *testing.T) {
	dir := initTestRepo(t)

	// Create a stack with a branch using "/" separator (the default)
	if err := RunInit([]string{"--base", "main", "--branch", "add-logging", "mystack"}); err != nil {
		t.Fatal(err)
	}

	// Verify the branch is stored with "/" separator
	s, err := stack.LoadStack(dir, "mystack")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(s.Nodes))
	}
	if s.Nodes[0].Branch != "mystack/add-logging" {
		t.Fatalf("expected branch 'mystack/add-logging', got %q", s.Nodes[0].Branch)
	}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Change the separator to "-"
	err = RunConfig([]string{"set", "branch_prefix.separator", "-"})

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	stderrOutput := buf.String()

	if err != nil {
		t.Fatalf("config set failed: %v", err)
	}

	// Should warn about mismatched branches
	if !strings.Contains(stderrOutput, "Warning") {
		t.Errorf("expected warning on stderr, got: %q", stderrOutput)
	}
	if !strings.Contains(stderrOutput, "mystack") {
		t.Errorf("expected stack name in warning, got: %q", stderrOutput)
	}
	if !strings.Contains(stderrOutput, "only applies to branches created from now on") {
		t.Errorf("expected guidance message in warning, got: %q", stderrOutput)
	}
}

func TestConfigSet_NoWarningWhenPrefixMatches(t *testing.T) {
	initTestRepo(t)

	// Create a stack with default "/" separator
	if err := RunInit([]string{"--base", "main", "--branch", "add-logging", "mystack"}); err != nil {
		t.Fatal(err)
	}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Set the prefix to the same stack ID (effectively no change to branch naming)
	err := RunConfig([]string{"set", "branch_prefix.prefix", "mystack"})

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	stderrOutput := buf.String()

	if err != nil {
		t.Fatalf("config set failed: %v", err)
	}

	// Should NOT warn — branches already match
	if strings.Contains(stderrOutput, "Warning") {
		t.Errorf("expected no warning when prefix matches, got: %q", stderrOutput)
	}
}

func TestConfigSet_NoWarningForNonPrefixKeys(t *testing.T) {
	initTestRepo(t)

	if err := RunInit([]string{"--base", "main", "--branch", "add-logging", "mystack"}); err != nil {
		t.Fatal(err)
	}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Change a non-prefix config key
	err := RunConfig([]string{"set", "pr_title.conventional_commits", "true"})

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	stderrOutput := buf.String()

	if err != nil {
		t.Fatalf("config set failed: %v", err)
	}

	if strings.Contains(stderrOutput, "Warning") {
		t.Errorf("expected no warning for non-prefix key, got: %q", stderrOutput)
	}
}

func TestConfigSet_NoWarningWhenNoStacks(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	// Set up minimal sdf structure (config file but no stacks)
	sdfDir := filepath.Join(dir, ".sdf")
	os.MkdirAll(filepath.Join(sdfDir, "stacks"), 0755)

	// Write a minimal config so FindRoot can locate the repo
	cfgpkg.Save(filepath.Join(sdfDir, "config.json"), cfgpkg.Defaults())

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	err = RunConfig([]string{"set", "branch_prefix.separator", "-"})

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	stderrOutput := buf.String()

	if err != nil {
		t.Fatalf("config set failed: %v", err)
	}

	if strings.Contains(stderrOutput, "Warning") {
		t.Errorf("expected no warning when no stacks exist, got: %q", stderrOutput)
	}
}

func TestConfigSet_WarnsOnPrefixChange(t *testing.T) {
	initTestRepo(t)

	// Create a stack — branches will get default prefix "mystack/"
	if err := RunInit([]string{"--base", "main", "--branch", "add-logging", "mystack"}); err != nil {
		t.Fatal(err)
	}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Change the prefix to something different
	err := RunConfig([]string{"set", "branch_prefix.prefix", "team-x"})

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	stderrOutput := buf.String()

	if err != nil {
		t.Fatalf("config set failed: %v", err)
	}

	// Should warn — branch "mystack/add-logging" doesn't match prefix "team-x/"
	if !strings.Contains(stderrOutput, "Warning") {
		t.Errorf("expected warning on prefix change, got: %q", stderrOutput)
	}
	if !strings.Contains(stderrOutput, "mystack/add-logging") {
		t.Errorf("expected branch name in warning, got: %q", stderrOutput)
	}
}
