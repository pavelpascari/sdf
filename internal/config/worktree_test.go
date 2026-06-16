// internal/config/worktree_test.go
package config

import (
	"path/filepath"
	"testing"
)

func TestSanitizeBranchForPath(t *testing.T) {
	cases := map[string]string{
		"feat/a":      "feat-a",
		"stack/sub/x": "stack-sub-x",
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
	want := filepath.Clean("/home/u/proj/myrepo.worktrees/feat-a")
	if got != want {
		t.Errorf("WorktreePathFor default = %q, want %q", got, want)
	}
}

func TestWorktreePathForCustomAbsolute(t *testing.T) {
	cfg := Defaults()
	cfg.Worktree.BasePath = "/scratch/wt"
	got := cfg.WorktreePathFor("/home/u/proj/myrepo", "feat/a")
	if got != filepath.Clean("/scratch/wt/feat-a") {
		t.Errorf("WorktreePathFor custom = %q", got)
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
