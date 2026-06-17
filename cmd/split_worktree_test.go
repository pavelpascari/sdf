// cmd/split_worktree_test.go
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	splitpkg "github.com/pavelpascari/sdf/internal/split"
	"github.com/pavelpascari/sdf/internal/stack"
)

// splitWorktreeRepo sets up a repo with a source branch that has multiple
// file-based changes suitable for splitting, and chdirs into it.
// Returns the repo root.
func splitWorktreeRepo(t *testing.T) string {
	t.Helper()
	root := bareRepoWithClone(t) // from new_worktree_test.go

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Create a feature branch with two clearly separated file changes.
	git("checkout", "-b", "big-feature")
	if err := os.WriteFile(filepath.Join(root, "alpha.go"), []byte("package main\nfunc Alpha() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "beta.go"), []byte("package main\nfunc Beta() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("-c", "user.email=t@t.com", "-c", "user.name=T", "commit", "-m", "add alpha and beta")
	git("checkout", "main")

	return root
}

// makeTwoLayerPlan returns a minimal Plan with two layers, one file each.
func makeTwoLayerPlan() *splitpkg.Plan {
	return &splitpkg.Plan{
		Layers: []splitpkg.Layer{
			{
				Name:        "alpha",
				Description: "Add alpha",
				Files:       []string{"alpha.go"},
			},
			{
				Name:        "beta",
				Description: "Add beta",
				Files:       []string{"beta.go"},
			},
		},
	}
}

// TestSplitWorktreeModeCreatesWorktrees verifies that ExecuteWorktree:
//   - creates each split branch as a git worktree with a recorded WorktreePath,
//   - applies the per-layer changes in the correct worktree dir,
//   - leaves the main-repo checkout on main (untouched).
func TestSplitWorktreeModeCreatesWorktrees(t *testing.T) {
	root := splitWorktreeRepo(t)

	plan := makeTwoLayerPlan()
	stackName := "split-feat"
	base := "main"
	source := "big-feature"

	cfg, err := cfgpkg.Load(root)
	if err != nil {
		cfg = cfgpkg.Defaults()
	}

	addFn := func(node *stack.Node, createFrom string) error {
		return addWorktreeForNode(cfg, root, node, createFrom)
	}

	branches, err := splitpkg.ExecuteWorktree(plan, stackName, base, source, root, addFn)
	if err != nil {
		t.Fatalf("ExecuteWorktree: %v", err)
	}

	if len(branches) != 2 {
		t.Fatalf("expected 2 branches, got %d: %v", len(branches), branches)
	}

	// Load the stack to inspect worktree paths.
	s, err := stack.LoadStack(root, stackName)
	if err != nil {
		t.Fatalf("LoadStack: %v", err)
	}

	if !s.Worktree {
		t.Error("stack.Worktree should be true")
	}

	if len(s.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(s.Nodes))
	}

	for i, node := range s.Nodes {
		if node.WorktreePath == "" {
			t.Errorf("node %d (%s): WorktreePath is empty", i, node.Branch)
			continue
		}

		// Verify the worktree directory exists on disk.
		if _, err := os.Stat(node.WorktreePath); err != nil {
			t.Errorf("node %d (%s): worktree dir missing: %v", i, node.Branch, err)
		}

		// Verify the worktree is checked out to the correct branch.
		branchCmd := exec.Command("git", "-C", node.WorktreePath, "rev-parse", "--abbrev-ref", "HEAD")
		raw, runErr := branchCmd.CombinedOutput()
		if runErr != nil {
			t.Errorf("node %d (%s): cannot get branch: %v", i, node.Branch, runErr)
			continue
		}
		gotBranch := strings.TrimSpace(string(raw))
		if gotBranch != node.Branch {
			t.Errorf("node %d: worktree on branch %q, want %q", i, gotBranch, node.Branch)
		}

		// Verify the expected WorktreePath matches config convention.
		want := cfg.WorktreePathFor(root, node.Branch)
		if node.WorktreePath != want {
			t.Errorf("node %d (%s): WorktreePath = %q, want %q", i, node.Branch, node.WorktreePath, want)
		}
	}

	// Layer 1 worktree should have alpha.go, not beta.go.
	wt1 := s.Nodes[0].WorktreePath
	if _, err := os.Stat(filepath.Join(wt1, "alpha.go")); err != nil {
		t.Errorf("layer-1 worktree missing alpha.go: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt1, "beta.go")); err == nil {
		t.Error("layer-1 worktree should NOT have beta.go")
	}

	// Layer 2 worktree should have both alpha.go (inherited) and beta.go.
	wt2 := s.Nodes[1].WorktreePath
	if _, err := os.Stat(filepath.Join(wt2, "alpha.go")); err != nil {
		t.Errorf("layer-2 worktree missing alpha.go: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt2, "beta.go")); err != nil {
		t.Errorf("layer-2 worktree missing beta.go: %v", err)
	}

	// Main repo checkout must remain on main (untouched).
	cmd := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD")
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse HEAD in main: %v", err)
	}
	if got := strings.TrimSpace(string(raw)); got != "main" {
		t.Errorf("main repo HEAD = %q, want main", got)
	}
}

// TestSplitWorktreeCheckoutsAreGuarded verifies that no plain gitpkg.Checkout
// is invoked when worktreeMode is true in runSplitCmd. This is a structural
// guard: the test calls the low-level ExecuteWorktree directly (not through
// RunSplit, because RunSplit requires Claude CLI). The two plain Checkout calls
// in cmd/split.go are guarded by if !worktreeMode — this test confirms the
// happy path of ExecuteWorktree does not touch the main-repo HEAD.
func TestSplitWorktreeMainRepoUntouched(t *testing.T) {
	root := splitWorktreeRepo(t)

	plan := makeTwoLayerPlan()
	cfg, err := cfgpkg.Load(root)
	if err != nil {
		cfg = cfgpkg.Defaults()
	}

	addFn := func(node *stack.Node, createFrom string) error {
		return addWorktreeForNode(cfg, root, node, createFrom)
	}

	if _, execErr := splitpkg.ExecuteWorktree(plan, "wt-split", "main", "big-feature", root, addFn); execErr != nil {
		t.Fatalf("ExecuteWorktree: %v", execErr)
	}

	// After ExecuteWorktree the main repo must still be on main.
	cmd := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "main" {
		t.Errorf("main repo HEAD = %q after ExecuteWorktree, want main", got)
	}
}
