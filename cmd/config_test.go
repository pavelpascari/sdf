package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
)

// configTestDir creates a temp directory with .sdf/stacks/ so FindRoot works,
// chdir's into it, and returns the path.
func configTestDir(t *testing.T) string {
	t.Helper()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".sdf", "stacks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })
	return dir
}

// loadConfigFile reads and unmarshals the repo config file.
func loadConfigFile(t *testing.T, dir string) cfgpkg.Config {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".sdf", "config.json"))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	var cfg cfgpkg.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parsing config: %v", err)
	}
	return cfg
}

func TestConfigSet_BranchPrefixEnabled(t *testing.T) {
	dir := configTestDir(t)

	rootCmd.SetArgs([]string{"config", "set", "branch_prefix.enabled", "false"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	cfg := loadConfigFile(t, dir)
	if cfg.BranchPrefix.Enabled == nil || *cfg.BranchPrefix.Enabled {
		t.Error("expected branch_prefix.enabled = false")
	}
}

func TestConfigSet_BranchPrefixEnabled_True(t *testing.T) {
	dir := configTestDir(t)

	rootCmd.SetArgs([]string{"config", "set", "branch_prefix.enabled", "true"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	cfg := loadConfigFile(t, dir)
	if cfg.BranchPrefix.Enabled == nil || !*cfg.BranchPrefix.Enabled {
		t.Error("expected branch_prefix.enabled = true")
	}
}

func TestConfigSet_BranchPrefixScope(t *testing.T) {
	dir := configTestDir(t)

	rootCmd.SetArgs([]string{"config", "set", "branch_prefix.scope", "my-team"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	cfg := loadConfigFile(t, dir)
	if cfg.BranchPrefix.Scope != "my-team" {
		t.Errorf("expected scope 'my-team', got %q", cfg.BranchPrefix.Scope)
	}
}

func TestConfigSet_BranchPrefixSeparator(t *testing.T) {
	dir := configTestDir(t)

	rootCmd.SetArgs([]string{"config", "set", "branch_prefix.separator", "-"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	cfg := loadConfigFile(t, dir)
	if cfg.BranchPrefix.Separator != "-" {
		t.Errorf("expected separator '-', got %q", cfg.BranchPrefix.Separator)
	}
}

func TestConfigSet_ConventionalCommits(t *testing.T) {
	dir := configTestDir(t)

	rootCmd.SetArgs([]string{"config", "set", "pr_title.conventional_commits", "false"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	cfg := loadConfigFile(t, dir)
	if cfg.PRTitle.ConventionalCommits == nil || *cfg.PRTitle.ConventionalCommits {
		t.Error("expected pr_title.conventional_commits = false")
	}
}

func TestConfigSet_ConventionalCommits_True(t *testing.T) {
	dir := configTestDir(t)

	rootCmd.SetArgs([]string{"config", "set", "pr_title.conventional_commits", "true"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	cfg := loadConfigFile(t, dir)
	if cfg.PRTitle.ConventionalCommits == nil || !*cfg.PRTitle.ConventionalCommits {
		t.Error("expected pr_title.conventional_commits = true")
	}
}

func TestConfigSet_TicketPattern(t *testing.T) {
	dir := configTestDir(t)

	rootCmd.SetArgs([]string{"config", "set", "pr_title.ticket_pattern", `[A-Z]+-\d+`})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	cfg := loadConfigFile(t, dir)
	if cfg.PRTitle.TicketPattern != `[A-Z]+-\d+` {
		t.Errorf("expected ticket_pattern '[A-Z]+-\\d+', got %q", cfg.PRTitle.TicketPattern)
	}
}

func TestConfigSet_SyncWithContent(t *testing.T) {
	dir := configTestDir(t)

	rootCmd.SetArgs([]string{"config", "set", "sync.with_content", "true"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	cfg := loadConfigFile(t, dir)
	if cfg.Sync.WithContent == nil || !*cfg.Sync.WithContent {
		t.Error("expected sync.with_content = true")
	}
}

func TestConfigSet_UnknownKey(t *testing.T) {
	_ = configTestDir(t)

	rootCmd.SetArgs([]string{"config", "set", "nonexistent.key", "value"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestConfigSet_PreservesExistingValues(t *testing.T) {
	dir := configTestDir(t)

	// Set scope first
	rootCmd.SetArgs([]string{"config", "set", "branch_prefix.scope", "api"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	// Set separator — scope should be preserved
	rootCmd.SetArgs([]string{"config", "set", "branch_prefix.separator", "-"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	cfg := loadConfigFile(t, dir)
	if cfg.BranchPrefix.Scope != "api" {
		t.Errorf("scope should be preserved, got %q", cfg.BranchPrefix.Scope)
	}
	if cfg.BranchPrefix.Separator != "-" {
		t.Errorf("expected separator '-', got %q", cfg.BranchPrefix.Separator)
	}
}

func TestConfigSet_BoolCaseInsensitive(t *testing.T) {
	dir := configTestDir(t)

	rootCmd.SetArgs([]string{"config", "set", "branch_prefix.enabled", "TRUE"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	cfg := loadConfigFile(t, dir)
	if cfg.BranchPrefix.Enabled == nil || !*cfg.BranchPrefix.Enabled {
		t.Error("expected branch_prefix.enabled = true (case insensitive)")
	}
}
