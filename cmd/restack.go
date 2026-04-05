package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
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
	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{})
	defer func() { _ = bus.Finish() }()

	s, err := resolveStack(root, "")
	if err != nil {
		return err
	}

	// Fetch and check if stack is in sync
	bus.Print("Fetching from origin...")
	if err := gitpkg.FetchAll(); err != nil {
		bus.Warnf("warning: fetch failed: %v", err)
	}

	// Fast-forward base
	if err := gitpkg.FastForward(s.Base); err != nil {
		bus.Warnf("warning: could not fast-forward %s: %v", s.Base, err)
	}

	// Check if sync is needed
	syncPlan := computeSyncPlan(s, nil)
	hasWork := false
	for _, a := range syncPlan {
		if a.kind != "skip-merged" && a.kind != "skip-closed" {
			hasWork = true
			break
		}
	}

	if hasWork {
		bus.Pause()
		ok := ui.Confirm("Stack is not in sync. Run sdf sync first?")
		bus.Resume()
		if !ok {
			return fmt.Errorf("stack must be in sync before restacking — run `sdf sync` first")
		}
		bus.Print("")
		if err := runSyncFrom(root, s, 0, &syncOptions{}, nil, bus, nil); err != nil {
			return fmt.Errorf("sync failed: %w", err)
		}
		// Reload stack after sync (BaseTips may have changed)
		s, err = resolveStack(root, s.StackID)
		if err != nil {
			return fmt.Errorf("cannot reload stack after sync: %w", err)
		}
		bus.Print("")
	}

	// Working tree must be clean
	clean, err := gitpkg.IsClean()
	if err != nil {
		return fmt.Errorf("cannot check working tree status: %w", err)
	}
	if !clean {
		return fmt.Errorf("working tree is not clean — commit or stash changes first")
	}

	// Remember current branch to restore later
	originalBranch, _ := gitpkg.CurrentBranch()

	// Validate the move
	if err := validateRestack(s, sourceBranch, afterBranch); err != nil {
		return err
	}

	// Normalize afterBranch for reorderNodes
	reorderAfter := afterBranch
	if afterBranch == s.Base {
		reorderAfter = ""
	}

	newNodes := reorderNodes(s.Nodes, sourceBranch, reorderAfter)
	plan := computeRestackPlan(s, newNodes)

	bus.Printf("Restacking %s after %s in stack %s...",
		ui.Branch(sourceBranch), ui.Branch(afterBranch), ui.Bold.Render(s.StackID))

	// Print plan
	bus.Print("\nRestack plan:")
	for _, a := range plan {
		bus.Printf("  %s rebase %s onto %s", ui.SymPlan, ui.Branch(a.Branch), ui.Branch(a.NewParent))
	}
	bus.Print("")

	// Apply new node order
	s.Nodes = newNodes

	// Rebase each affected branch
	for _, a := range plan {
		bus.Printf("  rebasing %s onto %s...", ui.Branch(a.Branch), ui.Branch(a.NewParent))

		parentTip, err := gitpkg.RevParse(a.NewParent)
		if err != nil {
			return fmt.Errorf("cannot resolve %s: %w", a.NewParent, err)
		}

		node := s.FindNode(a.Branch)
		oldBase := node.BaseTip
		if oldBase == "" {
			oldBase = a.OldParent
		}

		if err := gitpkg.RebaseOnto(parentTip, oldBase, a.Branch); err != nil {
			if conflictErr := handleMoveConflict(s, a.Branch, err, bus); conflictErr != nil {
				stack.Save(root, s)
				gitpkg.Checkout(originalBranch)
				return fmt.Errorf("rebase of %s failed: %w — resolve conflicts and run `sdf sync` to continue",
					a.Branch, conflictErr)
			}
		}

		// Update BaseTip
		newTip, _ := gitpkg.RevParse(a.NewParent)
		node.BaseTip = newTip

		bus.Printf("  %s %s rebased", ui.SymOK, ui.Branch(a.Branch))

		// Push (will fail in tests without remote — that's OK, non-fatal)
		if err := gitpkg.Push(a.Branch); err != nil {
			bus.Warnf("  %s could not push %s: %v", ui.SymWarn, ui.Branch(a.Branch), err)
		} else {
			bus.Printf("  %s %s pushed", ui.SymOK, ui.Branch(a.Branch))
		}
	}

	// Update PR bases on GitHub
	if ghpkg.Available() {
		for _, a := range plan {
			node := s.FindNode(a.Branch)
			if node != nil && node.PR > 0 {
				if err := ghpkg.PREditBase(node.PR, a.NewParent); err != nil {
					bus.Warnf("  %s could not update PR #%d base: %v", ui.SymWarn, node.PR, err)
				} else {
					bus.Printf("  PR %s base updated → %s", ui.PR(node.PR), ui.Branch(a.NewParent))
				}
			}
		}
	}

	// Update PR navigation
	if err := updateStackNavForAllPRs(root, s, nil, bus); err != nil {
		bus.Warnf("warning: could not update PR navigation: %v", err)
	}

	// Restore original branch
	gitpkg.Checkout(originalBranch)

	// Save stack
	if err := stack.Save(root, s); err != nil {
		return fmt.Errorf("cannot save stack: %w", err)
	}

	bus.Printf("\n%s Restack complete.", ui.SymOK)
	return nil
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
