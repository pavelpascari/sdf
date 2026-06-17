package config

import (
	"path/filepath"
	"strings"
)

// DefaultWorktreeBasePath is used when no base_path is configured.
const DefaultWorktreeBasePath = "../{repo}.worktrees"

// WorktreeConfig holds worktree-mode settings.
type WorktreeConfig struct {
	BasePath string `json:"base_path,omitempty"` // template; {repo} = repo dir name
}

// SanitizeBranchForPath maps a branch name to a relative worktree path. It keeps
// '/' as a directory separator (nested layout), which is collision-free because
// git's ref D/F rule forbids a branch and a sub-path of it coexisting
// (e.g. you cannot have both `feat` and `feat/login`).
func SanitizeBranchForPath(branch string) string {
	return filepath.FromSlash(branch)
}

// EffectiveWorktreeBasePath resolves the configured base_path template to an
// absolute directory, relative to the repo root when the template is relative.
func (c Config) EffectiveWorktreeBasePath(repoRoot string) string {
	tmpl := c.Worktree.BasePath
	if tmpl == "" {
		tmpl = DefaultWorktreeBasePath
	}
	resolved := strings.ReplaceAll(tmpl, "{repo}", filepath.Base(repoRoot))
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(repoRoot, resolved)
	}
	return filepath.Clean(resolved)
}

// WorktreePathFor returns the absolute worktree directory for a branch.
func (c Config) WorktreePathFor(repoRoot, branch string) string {
	return filepath.Join(c.EffectiveWorktreeBasePath(repoRoot), SanitizeBranchForPath(branch))
}
