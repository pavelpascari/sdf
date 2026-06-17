// internal/config/worktree_test.go
package config

import (
	"path/filepath"
	"testing"
)

func TestSanitizeBranchForPath(t *testing.T) {
	cases := map[string]string{
		"feat/a":      filepath.FromSlash("feat/a"),
		"stack/sub/x": filepath.FromSlash("stack/sub/x"),
		"plain":       "plain",
	}
	for in, want := range cases {
		if got := SanitizeBranchForPath(in); got != want {
			t.Errorf("SanitizeBranchForPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWorktreePathForDefault(t *testing.T) {
	cfg := Defaults()
	root := "/home/u/proj/myrepo"
	got := cfg.WorktreePathFor(root, "feat/a")
	want := filepath.Clean("/home/u/proj/myrepo.worktrees/feat/a")
	if got != want {
		t.Errorf("WorktreePathFor default = %q, want %q", got, want)
	}
}

func TestWorktreePathForCustomAbsolute(t *testing.T) {
	cfg := Defaults()
	cfg.Worktree.BasePath = "/scratch/wt"
	got := cfg.WorktreePathFor("/home/u/proj/myrepo", "feat/a")
	if got != filepath.Clean("/scratch/wt/feat/a") {
		t.Errorf("WorktreePathFor custom = %q", got)
	}
}

func TestWorktreePathForNested(t *testing.T) {
	cfg := Defaults()
	root := "/home/u/proj/myrepo"
	if got := cfg.WorktreePathFor(root, "feat/login"); got != filepath.Clean("/home/u/proj/myrepo.worktrees/feat/login") {
		t.Errorf("nested path = %q", got)
	}
	// distinct branches must map to distinct dirs
	a := cfg.WorktreePathFor(root, "feat/login")
	b := cfg.WorktreePathFor(root, "feat-login")
	if a == b {
		t.Errorf("feat/login and feat-login collided: %q", a)
	}
}

func TestWorktreeBasePathRepoMerge(t *testing.T) {
	// repo overrides global base_path
	global := Config{Worktree: WorktreeConfig{BasePath: "../g.worktrees"}}
	repo := Config{Worktree: WorktreeConfig{BasePath: "../r.worktrees"}}
	merged := merge(global, repo)
	if merged.Worktree.BasePath != "../r.worktrees" {
		t.Errorf("repo base_path should win, got %q", merged.Worktree.BasePath)
	}
}
