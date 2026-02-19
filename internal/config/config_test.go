package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()

	if cfg.BranchPrefix.Enabled == nil || !*cfg.BranchPrefix.Enabled {
		t.Error("default Enabled should be true")
	}
	if cfg.BranchPrefix.Separator != "/" {
		t.Errorf("default Separator should be '/', got %q", cfg.BranchPrefix.Separator)
	}
	if cfg.BranchPrefix.Prefix != "" {
		t.Errorf("default Prefix should be empty, got %q", cfg.BranchPrefix.Prefix)
	}
}

func TestIsEnabled_Nil(t *testing.T) {
	cfg := Config{}
	if !cfg.IsEnabled() {
		t.Error("IsEnabled should default to true when Enabled is nil")
	}
}

func TestIsEnabled_True(t *testing.T) {
	cfg := Config{BranchPrefix: BranchPrefix{Enabled: boolPtr(true)}}
	if !cfg.IsEnabled() {
		t.Error("IsEnabled should return true")
	}
}

func TestIsEnabled_False(t *testing.T) {
	cfg := Config{BranchPrefix: BranchPrefix{Enabled: boolPtr(false)}}
	if cfg.IsEnabled() {
		t.Error("IsEnabled should return false")
	}
}

func TestEffectivePrefix_Empty(t *testing.T) {
	cfg := Config{}
	if got := cfg.EffectivePrefix("my-stack"); got != "my-stack" {
		t.Errorf("expected 'my-stack', got %q", got)
	}
}

func TestEffectivePrefix_Custom(t *testing.T) {
	cfg := Config{BranchPrefix: BranchPrefix{Prefix: "feat"}}
	if got := cfg.EffectivePrefix("my-stack"); got != "feat" {
		t.Errorf("expected 'feat', got %q", got)
	}
}

func TestEffectiveSeparator_Empty(t *testing.T) {
	cfg := Config{}
	if got := cfg.EffectiveSeparator(); got != "/" {
		t.Errorf("expected '/', got %q", got)
	}
}

func TestEffectiveSeparator_Custom(t *testing.T) {
	cfg := Config{BranchPrefix: BranchPrefix{Separator: "-"}}
	if got := cfg.EffectiveSeparator(); got != "-" {
		t.Errorf("expected '-', got %q", got)
	}
}

func TestMerge_RepoOverridesGlobal(t *testing.T) {
	global := Config{
		BranchPrefix: BranchPrefix{
			Enabled:   boolPtr(true),
			Prefix:    "global-prefix",
			Separator: "/",
		},
	}
	repo := Config{
		BranchPrefix: BranchPrefix{
			Enabled:   boolPtr(false),
			Prefix:    "repo-prefix",
			Separator: "-",
		},
	}

	result := merge(global, repo)

	if result.BranchPrefix.Enabled == nil || *result.BranchPrefix.Enabled {
		t.Error("repo Enabled=false should override global Enabled=true")
	}
	if result.BranchPrefix.Prefix != "repo-prefix" {
		t.Errorf("expected 'repo-prefix', got %q", result.BranchPrefix.Prefix)
	}
	if result.BranchPrefix.Separator != "-" {
		t.Errorf("expected '-', got %q", result.BranchPrefix.Separator)
	}
}

func TestMerge_GlobalFallback(t *testing.T) {
	global := Config{
		BranchPrefix: BranchPrefix{
			Enabled:   boolPtr(true),
			Prefix:    "global-prefix",
			Separator: "-",
		},
	}
	repo := Config{} // all zero values

	result := merge(global, repo)

	if result.BranchPrefix.Enabled == nil || !*result.BranchPrefix.Enabled {
		t.Error("should fall back to global Enabled=true")
	}
	if result.BranchPrefix.Prefix != "global-prefix" {
		t.Errorf("should fall back to global prefix, got %q", result.BranchPrefix.Prefix)
	}
	if result.BranchPrefix.Separator != "-" {
		t.Errorf("should fall back to global separator, got %q", result.BranchPrefix.Separator)
	}
}

func TestMerge_BothEmpty(t *testing.T) {
	result := merge(Config{}, Config{})
	applyDefaults(&result)

	if result.BranchPrefix.Enabled == nil || !*result.BranchPrefix.Enabled {
		t.Error("defaults should set Enabled=true")
	}
	if result.BranchPrefix.Separator != "/" {
		t.Errorf("defaults should set Separator='/', got %q", result.BranchPrefix.Separator)
	}
}

func TestLoadFile_Missing(t *testing.T) {
	cfg, err := loadFile("/nonexistent/path/config.json")
	if err != nil {
		t.Fatalf("missing file should not be an error, got: %v", err)
	}
	if cfg.BranchPrefix.Enabled != nil {
		t.Error("missing file should return zero Config")
	}
}

func TestLoadFile_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte("not json{"), 0644)

	_, err := loadFile(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadFile_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte(`{"branch_prefix":{"enabled":false,"prefix":"feat","separator":"-"}}`), 0644)

	cfg, err := loadFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BranchPrefix.Enabled == nil || *cfg.BranchPrefix.Enabled {
		t.Error("expected Enabled=false")
	}
	if cfg.BranchPrefix.Prefix != "feat" {
		t.Errorf("expected prefix 'feat', got %q", cfg.BranchPrefix.Prefix)
	}
	if cfg.BranchPrefix.Separator != "-" {
		t.Errorf("expected separator '-', got %q", cfg.BranchPrefix.Separator)
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.json")

	cfg := Defaults()
	cfg.BranchPrefix.Prefix = "my-prefix"

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := loadFile(path)
	if err != nil {
		t.Fatalf("loadFile failed: %v", err)
	}

	if loaded.BranchPrefix.Prefix != "my-prefix" {
		t.Errorf("expected 'my-prefix', got %q", loaded.BranchPrefix.Prefix)
	}
	if loaded.BranchPrefix.Enabled == nil || !*loaded.BranchPrefix.Enabled {
		t.Error("expected Enabled=true after round-trip")
	}
}
