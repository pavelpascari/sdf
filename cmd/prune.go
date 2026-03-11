package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
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
		removedNodes := pruneMissingNodes(s)
		result.NodesPruned += removedNodes
		if removedNodes > 0 {
			result.Actions = append(result.Actions, fmt.Sprintf("remove %d orphan node(s) from stack %s", removedNodes, s.StackID))
		}

		if shouldDeleteStack(s) {
			result.StacksPruned++
			result.Actions = append(result.Actions, fmt.Sprintf("delete stack file %s", s.StackID))
			if apply {
				_ = os.Remove(stack.StackPath(root, s.StackID))
			}
			continue
		}

		keepStacks[s.StackID] = true
		if apply && removedNodes > 0 {
			if err := stack.Save(root, s); err != nil {
				return fmt.Errorf("cannot save stack %s: %w", s.StackID, err)
			}
		}
	}

	// Remove legacy .sdf/context/ directory (no longer used).
	pruneLegacyDir(root, "context", apply, &result)

	// Remove stale split plan files for stacks that no longer exist.
	pruneSplitPlans(root, keepStacks, apply, &result)

	if pruneLocalState(local, keepStacks) {
		result.LocalPruned = true
		result.Actions = append(result.Actions, "prune stale entries from .sdf/local.json")
		if apply {
			if err := stack.SaveLocal(root, local); err != nil {
				return fmt.Errorf("cannot save local state: %w", err)
			}
		}
	}

	if jsonFlag {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	if len(result.Actions) == 0 {
		fmt.Println("No stale .sdf artifacts found.")
		return nil
	}

	mode := "Dry-run"
	if apply {
		mode = "Applied"
	}
	fmt.Printf("%s prune actions:\n", mode)
	for _, a := range result.Actions {
		fmt.Printf("  - %s\n", a)
	}
	if !apply {
		fmt.Println("\nRe-run with --apply to apply.")
	}
	return nil
}

func pruneMissingNodes(s *stack.Stack) int {
	if s == nil {
		return 0
	}
	kept := make([]stack.Node, 0, len(s.Nodes))
	removed := 0
	for _, n := range s.Nodes {
		if gitpkg.BranchExists(n.Branch) {
			kept = append(kept, n)
			continue
		}
		removed++
	}
	s.Nodes = kept
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

func pruneLocalState(local *stack.LocalState, keepStacks map[string]bool) bool {
	changed := false
	if local == nil {
		return false
	}

	if len(local.SplitSessions) > 0 {
		for stackID := range local.SplitSessions {
			if !keepStacks[stackID] {
				delete(local.SplitSessions, stackID)
				changed = true
			}
		}
		if len(local.SplitSessions) == 0 {
			local.SplitSessions = nil
		}
	}

	if local.SyncProgress != nil && local.SyncProgress.PausedAt != "" &&
		!gitpkg.BranchExists(local.SyncProgress.PausedAt) {
		local.SyncProgress = nil
		changed = true
	}

	return changed
}
