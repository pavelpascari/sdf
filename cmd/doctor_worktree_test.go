// cmd/doctor_worktree_test.go
package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
)

func TestCheckWorktreesFlagsMissingPath(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	s, _ := stack.LoadStack(root, "feat")
	// Corrupt: delete the worktree directory behind sdf's back.
	_ = os.RemoveAll(s.FindNode("feat/a").WorktreePath)

	problems := checkWorktrees(root)
	if len(problems) == 0 {
		t.Errorf("expected doctor to flag the missing worktree directory")
	}
}

func TestCheckWorktreesCleanWhenConsistent(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	if problems := checkWorktrees(root); len(problems) != 0 {
		t.Errorf("expected no problems for a consistent worktree stack, got %v", problems)
	}
}

// TestCheckWorktreesSymlinkPath verifies that checkWorktreesWithPaths does not
// produce a false positive when the WorktreePath stored in the stack and the
// path reported by git differ only because one side went through a symlink
// (e.g. /tmp on macOS is /private/tmp under the hood).
func TestCheckWorktreesSymlinkPath(t *testing.T) {
	// Create a real directory that will act as the worktree.
	realDir := t.TempDir()
	// Create a sibling directory for the symlink.
	symlinkDir := filepath.Join(filepath.Dir(realDir), "symlink-wt")
	if err := os.Symlink(realDir, symlinkDir); err != nil {
		t.Skipf("cannot create symlink (may need elevated permissions): %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(symlinkDir) })

	// Build a minimal .sdf directory structure so LoadAll can find the stack.
	sdfRoot := t.TempDir()
	stacksDir := filepath.Join(sdfRoot, stack.SDFDir, stack.StacksDir)
	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write a fake stack JSON whose node has WorktreePath = symlinkDir (the
	// unresolved symlink path) — this is what sdf would have stored.
	fakeStack := stack.Stack{
		StackID:  "feat",
		Base:     "main",
		Worktree: true,
		Nodes: []stack.Node{
			{Branch: "feat/a", PR: 1, Status: "open", WorktreePath: symlinkDir},
		},
	}
	data, err := json.MarshalIndent(fakeStack, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stacksDir, "feat.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate what git reports: the resolved (real) path.
	gitPaths := []string{realDir}

	// With symlink normalisation, symlinkDir and realDir resolve to the same
	// real path, so no problem should be reported.
	problems := checkWorktreesWithPaths(sdfRoot, gitPaths)
	if len(problems) != 0 {
		t.Errorf("expected no problems when paths differ only by symlink, got %v", problems)
	}

	// Sanity-check the inverse: without symlink resolution the paths differ.
	if symlinkDir == realDir {
		t.Skip("symlink resolved to same string; test not meaningful on this platform")
	}
	knownRaw := map[string]bool{realDir: true}
	if knownRaw[symlinkDir] {
		t.Error("raw path map should NOT contain the symlink path")
	}
}
