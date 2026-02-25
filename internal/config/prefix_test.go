package config

import "testing"

func TestApplyPrefix_BasicCase(t *testing.T) {
	cfg := Defaults()
	got := ApplyPrefix(cfg, "users-feature", "db-schema")
	if got != "users-feature/db-schema" {
		t.Errorf("expected 'users-feature/db-schema', got %q", got)
	}
}

func TestApplyPrefix_AlreadyPrefixed(t *testing.T) {
	cfg := Defaults()
	got := ApplyPrefix(cfg, "users-feature", "users-feature/db-schema")
	if got != "users-feature/db-schema" {
		t.Errorf("should not double-prefix, got %q", got)
	}
}

func TestApplyPrefix_Disabled(t *testing.T) {
	cfg := Config{BranchPrefix: BranchPrefix{Enabled: boolPtr(false)}}
	got := ApplyPrefix(cfg, "users-feature", "db-schema")
	if got != "db-schema" {
		t.Errorf("should return raw name when disabled, got %q", got)
	}
}

func TestApplyPrefix_CustomSeparator(t *testing.T) {
	cfg := Config{BranchPrefix: BranchPrefix{
		Enabled:   boolPtr(true),
		Separator: "-",
	}}
	got := ApplyPrefix(cfg, "users-feature", "db-schema")
	if got != "users-feature-db-schema" {
		t.Errorf("expected 'users-feature-db-schema', got %q", got)
	}
}

func TestApplyPrefix_CustomPrefix(t *testing.T) {
	cfg := Config{BranchPrefix: BranchPrefix{
		Enabled:   boolPtr(true),
		Scope:     "feat",
		Separator: "/",
	}}
	got := ApplyPrefix(cfg, "users-feature", "db-schema")
	if got != "feat/db-schema" {
		t.Errorf("expected 'feat/db-schema', got %q", got)
	}
}

func TestApplyPrefix_EmptyStackID(t *testing.T) {
	cfg := Defaults()
	got := ApplyPrefix(cfg, "", "db-schema")
	if got != "db-schema" {
		t.Errorf("empty stackID with empty prefix should return raw name, got %q", got)
	}
}

func TestHasPrefix_True(t *testing.T) {
	cfg := Defaults()
	if !HasPrefix(cfg, "feat", "feat/db-schema") {
		t.Error("should detect prefix")
	}
}

func TestHasPrefix_False(t *testing.T) {
	cfg := Defaults()
	if HasPrefix(cfg, "feat", "db-schema") {
		t.Error("should not detect prefix when absent")
	}
}

func TestHasPrefix_Disabled(t *testing.T) {
	cfg := Config{BranchPrefix: BranchPrefix{Enabled: boolPtr(false)}}
	if HasPrefix(cfg, "feat", "feat/db-schema") {
		t.Error("should return false when disabled")
	}
}

func TestStripPrefix_HasPrefix(t *testing.T) {
	cfg := Defaults()
	got := StripPrefix(cfg, "feat", "feat/db-schema")
	if got != "db-schema" {
		t.Errorf("expected 'db-schema', got %q", got)
	}
}

func TestStripPrefix_NoPrefix(t *testing.T) {
	cfg := Defaults()
	got := StripPrefix(cfg, "feat", "db-schema")
	if got != "db-schema" {
		t.Errorf("expected 'db-schema', got %q", got)
	}
}

func TestStripPrefix_Disabled(t *testing.T) {
	cfg := Config{BranchPrefix: BranchPrefix{Enabled: boolPtr(false)}}
	got := StripPrefix(cfg, "feat", "feat/db-schema")
	if got != "feat/db-schema" {
		t.Errorf("should not strip when disabled, got %q", got)
	}
}

func TestStripPrefix_CustomSeparator(t *testing.T) {
	cfg := Config{BranchPrefix: BranchPrefix{
		Enabled:   boolPtr(true),
		Separator: "-",
		Scope:     "feat",
	}}
	got := StripPrefix(cfg, "stack", "feat-db-schema")
	if got != "db-schema" {
		t.Errorf("expected 'db-schema', got %q", got)
	}
}
