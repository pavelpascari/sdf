// Package config manages sdf configuration from global and repo-level files.
//
// Configuration is loaded from two locations:
//   - Global: ~/.config/sdf/config.json (user-level defaults)
//   - Repo:   .sdf/config.json (per-repo overrides, committed to git)
//
// Repo-level values override global values on a field-by-field basis.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pavelpascari/sdf/internal/stack"
)

// BranchPrefix holds branch prefix enforcement settings.
type BranchPrefix struct {
	Enabled   *bool  `json:"enabled,omitempty"`   // nil = unset (defaults true)
	Scope     string `json:"scope,omitempty"`     // empty = use stack_id
	Separator string `json:"separator,omitempty"` // empty = default "/"
}

// PRTitle holds PR title generation settings.
type PRTitle struct {
	ConventionalCommits *bool  `json:"conventional_commits,omitempty"` // nil = unset (defaults true)
	TicketPattern       string `json:"ticket_pattern,omitempty"`       // regex to extract ticket ID from branch name
}

// SyncConfig holds sync-time PR update settings.
type SyncConfig struct {
	WithContent *bool `json:"with_content,omitempty"` // nil = unset (defaults false)
}

// Config represents the sdf configuration.
type Config struct {
	BranchPrefix BranchPrefix `json:"branch_prefix"`
	PRTitle      PRTitle      `json:"pr_title,omitempty"`
	Sync         SyncConfig   `json:"sync,omitempty"`
}

// ConfigFile is the filename for the configuration within .sdf/.
const ConfigFile = "config.json"

// boolPtr returns a pointer to the given bool value.
func boolPtr(b bool) *bool { return &b }

// Defaults returns a Config with all default values applied.
func Defaults() Config {
	return Config{
		BranchPrefix: BranchPrefix{
			Enabled:   boolPtr(true),
			Scope:     "",
			Separator: "/",
		},
	}
}

// GlobalPath returns the path to the global config file.
// This is always ~/.config/sdf/config.json.
func GlobalPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "sdf", ConfigFile), nil
}

// RepoPath returns the path to the repo-level config file.
func RepoPath(root string) string {
	return filepath.Join(root, stack.SDFDir, ConfigFile)
}

// Load reads config from global + repo locations and merges them.
// Missing files are not errors — the tool works without any config file.
func Load(root string) (Config, error) {
	var global, repo Config

	globalPath, err := GlobalPath()
	if err == nil {
		global, err = loadFile(globalPath)
		if err != nil {
			return Config{}, fmt.Errorf("cannot load global config %s: %w", globalPath, err)
		}
	}

	repoPath := RepoPath(root)
	repo, err = loadFile(repoPath)
	if err != nil {
		return Config{}, fmt.Errorf("cannot load repo config %s: %w", repoPath, err)
	}

	merged := merge(global, repo)
	applyDefaults(&merged)
	return merged, nil
}

// Save writes a Config to the given path, creating parent directories as needed.
func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal config: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

// IsEnabled returns whether branch prefix enforcement is enabled.
// Defaults to true if Enabled is nil.
func (c Config) IsEnabled() bool {
	if c.BranchPrefix.Enabled == nil {
		return true
	}
	return *c.BranchPrefix.Enabled
}

// EffectivePrefix returns the resolved prefix string.
// If Prefix is empty, falls back to stackID.
func (c Config) EffectivePrefix(stackID string) string {
	if c.BranchPrefix.Scope != "" {
		return c.BranchPrefix.Scope
	}
	return stackID
}

// EffectiveSeparator returns the resolved separator.
// Defaults to "/" if empty.
func (c Config) EffectiveSeparator() string {
	if c.BranchPrefix.Separator != "" {
		return c.BranchPrefix.Separator
	}
	return "/"
}

// ConventionalCommitsEnabled returns whether conventional commit PR titles are enabled.
// Defaults to true if unset.
func (c Config) ConventionalCommitsEnabled() bool {
	if c.PRTitle.ConventionalCommits == nil {
		return true
	}
	return *c.PRTitle.ConventionalCommits
}

// WithContentEnabled returns whether sync should update PR titles and descriptions.
func (c Config) WithContentEnabled() bool {
	if c.Sync.WithContent == nil {
		return false
	}
	return *c.Sync.WithContent
}

// ConfigKeyMeta describes a single configuration key for documentation.
type ConfigKeyMeta struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Default     string `json:"default"`
	Description string `json:"description"`
}

// ConfigKeys returns metadata for all configuration keys.
func ConfigKeys() []ConfigKeyMeta {
	return []ConfigKeyMeta{
		{"branch_prefix.enabled", "bool", "true", "Enable/disable branch prefix enforcement"},
		{"branch_prefix.scope", "string", "", "Scope string used as branch prefix and conventional commit scope (empty = use stack ID)"},
		{"branch_prefix.separator", "string", "/", "Separator character between prefix and branch name"},
		{"pr_title.conventional_commits", "bool", "true", "Enable conventional commit prefixes in PR titles"},
		{"pr_title.ticket_pattern", "string", "", "Regex to extract ticket ID from branch name for PR titles"},
		{"sync.with_content", "bool", "false", "Auto-update PR titles and descriptions during sync"},
	}
}

// loadFile reads and unmarshals a config file.
// Returns zero Config if the file doesn't exist (not an error).
func loadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("invalid JSON: %w", err)
	}
	return cfg, nil
}

// merge returns a new Config with repo values overriding global values.
// Non-zero/non-nil repo fields take precedence.
func merge(global, repo Config) Config {
	result := global

	if repo.BranchPrefix.Enabled != nil {
		result.BranchPrefix.Enabled = repo.BranchPrefix.Enabled
	}
	if repo.BranchPrefix.Scope != "" {
		result.BranchPrefix.Scope = repo.BranchPrefix.Scope
	}
	if repo.BranchPrefix.Separator != "" {
		result.BranchPrefix.Separator = repo.BranchPrefix.Separator
	}

	if repo.PRTitle.ConventionalCommits != nil {
		result.PRTitle.ConventionalCommits = repo.PRTitle.ConventionalCommits
	}
	if repo.PRTitle.TicketPattern != "" {
		result.PRTitle.TicketPattern = repo.PRTitle.TicketPattern
	}

	if repo.Sync.WithContent != nil {
		result.Sync.WithContent = repo.Sync.WithContent
	}
	return result
}

// applyDefaults fills in any remaining unset fields with default values.
func applyDefaults(cfg *Config) {
	if cfg.BranchPrefix.Enabled == nil {
		cfg.BranchPrefix.Enabled = boolPtr(true)
	}
	if cfg.BranchPrefix.Separator == "" {
		cfg.BranchPrefix.Separator = "/"
	}
}
