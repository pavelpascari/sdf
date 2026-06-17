package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/spf13/cobra"
)

type PruneResult struct {
	DryRun       bool     `json:"dry_run"`
	Actions      []string `json:"actions"`
	StacksPruned int      `json:"stacks_pruned"`
	NodesPruned  int      `json:"nodes_pruned"`
	LocalPruned  bool     `json:"local_pruned"`
}

var pruneCmd = &cobra.Command{
	Use:         "prune",
	Short:       "Clean stale .sdf metadata",
	Annotations: map[string]string{"category": "stack"},
	RunE:        runPrune,
}

func init() {
	rootCmd.AddCommand(pruneCmd)
	pruneCmd.Flags().Bool("apply", false, "apply changes (default: dry-run)")
	pruneCmd.Flags().Bool("json", false, "output result as JSON")
}

func runPrune(cmd *cobra.Command, args []string) error {
	apply, _ := cmd.Flags().GetBool("apply")
	jsonFlag, _ := cmd.Flags().GetBool("json")

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	stacks, err := stack.LoadAll(root)
	if err != nil {
		return err
	}

	local, _ := stack.LoadLocal(root)
	if local == nil {
		local = &stack.LocalState{}
	}

	result := PruneResult{DryRun: !apply}
	keepStacks := make(map[string]bool)

	for _, s := range stacks {
		// Remove orphan nodes first, then check if the stack should be deleted.
		// A stack that becomes empty after orphan removal is intentionally deleted.
		removedCount, removedNodes := pruneMissingNodes(s)
		result.NodesPruned += removedCount
		if removedCount > 0 {
			if s.Worktree && apply {
				removeWorktreesForNodes(removedNodes)
			}
			result.Actions = append(result.Actions, fmt.Sprintf("remove %d orphan node(s) from stack %s", removedCount, s.StackID))
		}

		if shouldDeleteStack(s) {
			if s.Worktree && apply {
				removeWorktreesForNodes(s.Nodes)
			}
			result.StacksPruned++
			result.Actions = append(result.Actions, fmt.Sprintf("delete stack file %s", s.StackID))
			if apply {
				_ = os.Remove(stack.StackPath(root, s.StackID))
			}
			continue
		}

		keepStacks[s.StackID] = true
		if apply && removedCount > 0 {
			if err := stack.Save(root, s); err != nil {
				return fmt.Errorf("cannot save stack %s: %w", s.StackID, err)
			}
		}
	}

	// Remove legacy .sdf/context/ directory (no longer used).
	pruneLegacyDir(root, "context", apply, &result)

	// Remove stale split plan files for stacks that no longer exist.
	pruneSplitPlans(root, keepStacks, apply, &result)

	// Compute which local.json entries are stale. Branch-existence (git) checks
	// happen HERE, outside the local lock, so the locked apply below stays tiny
	// and git-free (deadlock rule: no git inside a WithLocalLock body).
	plan := planLocalPrune(local, keepStacks)
	if plan.any() {
		result.LocalPruned = true
		result.Actions = append(result.Actions, "prune stale entries from .sdf/local.json")
		if apply {
			// Apply the precomputed plan against a FRESH LocalState under the
			// repo-wide lock so a concurrent writer's update is not clobbered.
			if err := stack.WithLocalLock(root, func(ls *stack.LocalState) error {
				plan.apply(ls)
				return nil
			}); err != nil {
				return fmt.Errorf("cannot save local state: %w", err)
			}
		}
	}

	var rdr render.Renderer
	if jsonFlag {
		rdr = &render.JSONRenderer{}
	}
	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{Renderer: rdr})
	if !jsonFlag {
		defer func() { _ = bus.Finish() }()
	}

	if jsonFlag {
		_ = bus.Finish()
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	if len(result.Actions) == 0 {
		bus.Print("No stale .sdf artifacts found.")
		return nil
	}

	mode := "Dry-run"
	if apply {
		mode = "Applied"
	}
	bus.Printf("%s prune actions:", mode)
	for _, a := range result.Actions {
		bus.Printf("  - %s", a)
	}
	if !apply {
		bus.Print("\nRe-run with --apply to apply.")
	}
	return nil
}

func pruneMissingNodes(s *stack.Stack) (int, []stack.Node) {
	if s == nil {
		return 0, nil
	}
	kept := make([]stack.Node, 0, len(s.Nodes))
	var removed []stack.Node
	for _, n := range s.Nodes {
		if gitpkg.BranchExists(n.Branch) {
			kept = append(kept, n)
			continue
		}
		removed = append(removed, n)
	}
	s.Nodes = kept
	return len(removed), removed
}

// removeWorktreesForNodes removes the worktrees recorded on the given nodes,
// returning how many were removed. Errors are ignored (best-effort cleanup).
func removeWorktreesForNodes(nodes []stack.Node) int {
	removed := 0
	for i := range nodes {
		if nodes[i].WorktreePath == "" {
			continue
		}
		if err := gitpkg.WorktreeRemove(nodes[i].WorktreePath, true); err == nil {
			removed++
		}
	}
	return removed
}

func shouldDeleteStack(s *stack.Stack) bool {
	if s == nil {
		return false
	}
	if len(s.Nodes) == 0 {
		return true
	}
	for _, n := range s.Nodes {
		if n.Status != "merged" && n.Status != "closed" {
			return false
		}
	}
	return true
}

// pruneLegacyDir removes a legacy .sdf/<name> directory if it exists.
func pruneLegacyDir(root, name string, apply bool, result *PruneResult) {
	dir := filepath.Join(root, stack.SDFDir, name)
	if _, err := os.Stat(dir); err != nil {
		return
	}
	result.Actions = append(result.Actions, fmt.Sprintf("delete legacy .sdf/%s/ directory", name))
	if apply {
		_ = os.RemoveAll(dir)
	}
}

// pruneSplitPlans removes .sdf/split-plans/<stack>.yaml files whose stack
// no longer exists.
func pruneSplitPlans(root string, keepStacks map[string]bool, apply bool, result *PruneResult) {
	dir := filepath.Join(root, stack.SDFDir, "split-plans")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if keepStacks[name] {
			continue
		}
		result.Actions = append(result.Actions, fmt.Sprintf("delete stale split plan %s", e.Name()))
		if apply {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// localPrunePlan is a precomputed set of stale local.json entries to delete.
// It is derived once (with git queries) outside the local lock, then applied
// inside a tiny git-free WithLocalLock body so concurrent writers are not lost.
type localPrunePlan struct {
	splitSessions    map[string]bool // stack IDs to delete from SplitSessions
	worktreeBranches map[string]bool // branches to delete from WorktreeProgress
	dropSync         bool            // drop SyncProgress
}

func (p localPrunePlan) any() bool {
	return len(p.splitSessions) > 0 || len(p.worktreeBranches) > 0 || p.dropSync
}

// apply deletes exactly the planned keys from ls. It only removes keys that are
// still present, so an entry added by a concurrent writer after the plan was
// computed is left untouched.
func (p localPrunePlan) apply(ls *stack.LocalState) {
	for id := range p.splitSessions {
		delete(ls.SplitSessions, id)
	}
	if len(ls.SplitSessions) == 0 {
		ls.SplitSessions = nil
	}
	if p.dropSync {
		ls.SyncProgress = nil
	}
	for b := range p.worktreeBranches {
		delete(ls.WorktreeProgress, b)
	}
	if len(ls.WorktreeProgress) == 0 {
		ls.WorktreeProgress = nil
	}
}

// planLocalPrune inspects a loaded LocalState snapshot and returns the set of
// stale entries to delete. Git branch-existence checks happen here (outside any
// lock). It does not mutate local.
func planLocalPrune(local *stack.LocalState, keepStacks map[string]bool) localPrunePlan {
	plan := localPrunePlan{
		splitSessions:    map[string]bool{},
		worktreeBranches: map[string]bool{},
	}
	if local == nil {
		return plan
	}

	for stackID := range local.SplitSessions {
		if !keepStacks[stackID] {
			plan.splitSessions[stackID] = true
		}
	}

	if local.SyncProgress != nil && local.SyncProgress.PausedAt != "" &&
		!gitpkg.BranchExists(local.SyncProgress.PausedAt) {
		plan.dropSync = true
	}

	for b := range local.WorktreeProgress {
		if !gitpkg.BranchExists(b) {
			plan.worktreeBranches[b] = true
		}
	}

	return plan
}
