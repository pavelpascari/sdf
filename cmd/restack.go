package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/pavelpascari/sdf/internal/stack"
)

var restackCmd = &cobra.Command{
	Use:   "restack <branch>",
	Short: "Move a branch to a new position in the stack",
	Long: `Moves a branch to a new position (after the --after target), rebases all
affected branches, pushes, and updates PR bases on GitHub.`,
	Example: `  sdf restack feature/job --after feature/index
  sdf restack feature/auth --after main       # make it first in the stack`,
	Annotations: map[string]string{"category": "stack"},
	Args:        cobra.ExactArgs(1),
	RunE:        runRestackCmd,
}

func init() {
	rootCmd.AddCommand(restackCmd)
	restackCmd.Flags().String("after", "", "branch to insert after (required)")
	_ = restackCmd.MarkFlagRequired("after")
}

func runRestackCmd(cmd *cobra.Command, args []string) error {
	after, _ := cmd.Flags().GetString("after")
	return runRestackLogic(args[0], after)
}

func validateRestack(s *stack.Stack, sourceBranch, afterBranch string) error {
	if sourceBranch == afterBranch {
		return fmt.Errorf("cannot move %s after itself", sourceBranch)
	}

	sourceIdx := s.NodeIndex(sourceBranch)
	if sourceIdx < 0 {
		return fmt.Errorf("branch %q is not part of stack %q", sourceBranch, s.StackID)
	}

	isBase := afterBranch == s.Base
	afterIdx := s.NodeIndex(afterBranch)
	if !isBase && afterIdx < 0 {
		return fmt.Errorf("branch %q is not part of stack %q", afterBranch, s.StackID)
	}

	// Normalize: if afterBranch is the base, treat as "" for reorderNodes
	reorderAfter := afterBranch
	if isBase {
		reorderAfter = ""
	}

	newNodes := reorderNodes(s.Nodes, sourceBranch, reorderAfter)
	plan := computeRestackPlan(s, newNodes)
	if len(plan) == 0 {
		return fmt.Errorf("%s is already in that position", sourceBranch)
	}

	return nil
}

func runRestackLogic(sourceBranch, afterBranch string) error {
	return fmt.Errorf("not yet implemented")
}

// reorderNodes returns a new slice with sourceBranch moved to immediately after
// afterBranch. If afterBranch is "", source becomes first. The original slice
// is not modified.
func reorderNodes(nodes []stack.Node, sourceBranch, afterBranch string) []stack.Node {
	var source stack.Node
	remaining := make([]stack.Node, 0, len(nodes)-1)
	for _, n := range nodes {
		if n.Branch == sourceBranch {
			source = n
		} else {
			remaining = append(remaining, n)
		}
	}

	if afterBranch == "" {
		result := make([]stack.Node, 0, len(nodes))
		result = append(result, source)
		result = append(result, remaining...)
		return result
	}

	result := make([]stack.Node, 0, len(nodes))
	for _, n := range remaining {
		result = append(result, n)
		if n.Branch == afterBranch {
			result = append(result, source)
		}
	}
	return result
}

// restackAction describes a single rebase that needs to happen: Branch should
// be rebased from OldParent onto NewParent.
type restackAction struct {
	Branch    string
	NewParent string
	OldParent string
}

// computeRestackPlan compares old vs new parent for each node and returns the
// branches whose effective parent changed (skipping merged/closed nodes).
// Results are in new array order.
func computeRestackPlan(s *stack.Stack, newNodes []stack.Node) []restackAction {
	oldStack := &stack.Stack{StackID: s.StackID, Base: s.Base, Nodes: s.Nodes}
	newStack := &stack.Stack{StackID: s.StackID, Base: s.Base, Nodes: newNodes}

	var actions []restackAction
	for _, node := range newNodes {
		if node.Status == "merged" || node.Status == "closed" {
			continue
		}
		oldParent := oldStack.ParentBranch(node.Branch)
		newParent := newStack.ParentBranch(node.Branch)
		if oldParent != newParent {
			actions = append(actions, restackAction{
				Branch:    node.Branch,
				NewParent: newParent,
				OldParent: oldParent,
			})
		}
	}
	return actions
}
