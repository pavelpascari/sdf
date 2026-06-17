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
	Use:   "restack [branch]",
	Short: "Move a branch to a new position in the stack",
	Long: `Moves a branch to a new position (after the --after target), rebases all
affected branches, pushes, and updates PR bases on GitHub.

Use --continue to resume after resolving conflicts.
Use --abort to restore all branches to their pre-restack state.`,
	Example: `  sdf restack feature/job --after feature/index
  sdf restack feature/auth --after main
  sdf restack --continue
  sdf restack --abort`,
	Annotations: map[string]string{"category": "stack"},
	Args:        cobra.MaximumNArgs(1),
	RunE:        runRestackCmd,
}

func init() {
	rootCmd.AddCommand(restackCmd)
	restackCmd.Flags().String("after", "", "branch to insert after")
	restackCmd.Flags().Bool("continue", false, "resume after conflict resolution")
	restackCmd.Flags().Bool("abort", false, "restore branches to pre-restack state")
}

func runRestackCmd(cmd *cobra.Command, args []string) error {
	contFlag, _ := cmd.Flags().GetBool("continue")
	abortFlag, _ := cmd.Flags().GetBool("abort")

	if contFlag && abortFlag {
		return fmt.Errorf("cannot use --continue and --abort together")
	}
	if contFlag {
		return runRestackContinue()
	}
	if abortFlag {
		return runRestackAbort()
	}

	if len(args) == 0 {
		return fmt.Errorf("branch name required (or use --continue / --abort)")
	}
	after, _ := cmd.Flags().GetString("after")
	if after == "" {
		return fmt.Errorf("--after flag is required")
	}
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

	// The base branch (e.g. "main") is not a node in the Nodes array, so
	// reorderNodes can't match on it. Empty string means "insert at front",
	// which is correct — "after main" means first in the stack.
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

	// Save snapshot for --abort / --continue
	branchSHAs := make(map[string]string)
	for _, a := range plan {
		sha, err := gitpkg.RevParse(a.Branch)
		if err == nil {
			branchSHAs[a.Branch] = sha
		}
	}

	serialPlan := make([]stack.RestackAction, len(plan))
	for i, a := range plan {
		serialPlan[i] = stack.RestackAction{
			Branch:    a.Branch,
			NewParent: a.NewParent,
			OldParent: a.OldParent,
		}
	}

	originalNodes := make([]stack.Node, len(s.Nodes))
	copy(originalNodes, s.Nodes)

	restackProgress := &stack.RestackProgress{
		StackID:        s.StackID,
		OriginalBranch: originalBranch,
		OriginalNodes:  originalNodes,
		BranchSHAs:     branchSHAs,
		Plan:           serialPlan,
		ResumeIndex:    0,
	}
	if err := stack.WithLocalLock(root, func(ls *stack.LocalState) error {
		ls.RestackProgress = restackProgress
		return nil
	}); err != nil {
		return fmt.Errorf("cannot save restack progress: %w", err)
	}

	// Apply new node order
	s.Nodes = newNodes

	// Rebase each affected branch (no pushes yet — defer until all succeed)
	var rebased []string
	for i, a := range plan {
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

		if s.Worktree {
			dir, err := requireBranchWorktreeDir(s, a.Branch)
			if err != nil {
				return err
			}
			if rebaseErr := gitpkg.RebaseOntoAt(dir, parentTip, oldBase, a.Branch); rebaseErr != nil {
				if conflictErr := handleMoveConflict(s, a.Branch, rebaseErr, bus, dir); conflictErr != nil {
					_ = stack.WithLocalLock(root, func(ls *stack.LocalState) error {
						if ls.RestackProgress != nil {
							ls.RestackProgress.ResumeIndex = i
						}
						return nil
					})
					_ = stack.WithLock(root, s.StackID, func(fresh *stack.Stack) error {
						fresh.Nodes = s.Nodes
						return nil
					})
					return fmt.Errorf("rebase of %s failed: %w — resolve conflicts and run `sdf restack --continue` or `sdf restack --abort`",
						a.Branch, conflictErr)
				}
			}
			newTip, _ := gitpkg.RevParseAt(dir, a.NewParent)
			node.BaseTip = newTip
		} else {
			if err := gitpkg.RebaseOnto(parentTip, oldBase, a.Branch); err != nil {
				if conflictErr := handleMoveConflict(s, a.Branch, err, bus, ""); conflictErr != nil {
					// Save progress with resume index
					_ = stack.WithLocalLock(root, func(ls *stack.LocalState) error {
						if ls.RestackProgress != nil {
							ls.RestackProgress.ResumeIndex = i
						}
						return nil
					})
					stack.Save(root, s)
					gitpkg.Checkout(originalBranch)
					return fmt.Errorf("rebase of %s failed: %w — resolve conflicts and run `sdf restack --continue` or `sdf restack --abort`",
						a.Branch, conflictErr)
				}
			}
			// Update BaseTip
			newTip, _ := gitpkg.RevParse(a.NewParent)
			node.BaseTip = newTip
		}
		rebased = append(rebased, a.Branch)

		bus.Printf("  %s %s rebased", ui.SymOK, ui.Branch(a.Branch))
	}

	// All rebases succeeded — now push all affected branches
	bus.Print("")
	for _, branch := range rebased {
		if s.Worktree {
			dir, err := requireBranchWorktreeDir(s, branch)
			if err != nil {
				bus.Warnf("  %s could not push %s: %v", ui.SymWarn, ui.Branch(branch), err)
				continue
			}
			if err := gitpkg.PushAt(dir, branch); err != nil {
				bus.Warnf("  %s could not push %s: %v", ui.SymWarn, ui.Branch(branch), err)
			} else {
				bus.Printf("  %s %s pushed", ui.SymOK, ui.Branch(branch))
			}
		} else {
			if err := gitpkg.Push(branch); err != nil {
				bus.Warnf("  %s could not push %s: %v", ui.SymWarn, ui.Branch(branch), err)
			} else {
				bus.Printf("  %s %s pushed", ui.SymOK, ui.Branch(branch))
			}
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

	if !s.Worktree {
		// Restore original branch (no-op in worktree mode — branches live in their own worktrees)
		gitpkg.Checkout(originalBranch)
	}

	// Save stack
	if s.Worktree {
		if err := stack.WithLock(root, s.StackID, func(fresh *stack.Stack) error {
			fresh.Nodes = s.Nodes
			return nil
		}); err != nil {
			return fmt.Errorf("cannot save stack: %w", err)
		}
	} else {
		if err := stack.Save(root, s); err != nil {
			return fmt.Errorf("cannot save stack: %w", err)
		}
	}

	// Clear restack progress
	_ = stack.WithLocalLock(root, func(ls *stack.LocalState) error {
		ls.RestackProgress = nil
		return nil
	})

	bus.Printf("\n%s Restack complete.", ui.SymOK)
	return nil
}

func runRestackAbort() error {
	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{})
	defer func() { _ = bus.Finish() }()

	ls, err := stack.LoadLocal(root)
	if err != nil {
		return fmt.Errorf("cannot read local state: %w", err)
	}
	if ls.RestackProgress == nil {
		return fmt.Errorf("no restack in progress")
	}

	progress := ls.RestackProgress
	bus.Printf("Aborting restack in stack %s...", ui.Bold.Render(progress.StackID))

	// Load the stack to determine worktree mode and locate worktree paths.
	s, err := stack.LoadStack(root, progress.StackID)
	if err != nil {
		return fmt.Errorf("cannot load stack: %w", err)
	}

	if s.Worktree {
		// Abort any in-progress rebase in each branch's own worktree.
		// We don't know which branch was mid-rebase, so try all branches.
		for branch := range progress.BranchSHAs {
			if dir, err := requireBranchWorktreeDir(s, branch); err == nil && dir != "" {
				_ = gitpkg.RebaseAbortAt(dir)
			}
		}

		// Restore each branch in its own worktree.
		for branch, sha := range progress.BranchSHAs {
			bus.Printf("  restoring %s to %s...", ui.Branch(branch), short(sha))
			dir, err := requireBranchWorktreeDir(s, branch)
			if err != nil {
				bus.Warnf("  %s %v", ui.SymWarn, err)
				continue
			}
			if err := gitpkg.ResetHardAt(dir, sha); err != nil {
				bus.Warnf("  %s could not reset %s: %v", ui.SymWarn, ui.Branch(branch), err)
				continue
			}
			bus.Printf("  %s %s restored", ui.SymOK, ui.Branch(branch))

			// Push restored branch from its worktree.
			if err := gitpkg.PushAt(dir, branch); err != nil {
				bus.Warnf("  %s could not push %s: %v", ui.SymWarn, ui.Branch(branch), err)
			}
		}

		// Restore original node order and BaseTips under the lock.
		if err := stack.WithLock(root, progress.StackID, func(fresh *stack.Stack) error {
			fresh.Nodes = progress.OriginalNodes
			return nil
		}); err != nil {
			return fmt.Errorf("cannot save stack: %w", err)
		}
		// No branch restore needed in worktree mode — each branch lives in its own worktree.
	} else {
		// Abort any in-progress rebase
		gitpkg.RebaseAbort()

		// Restore each branch to its pre-restack SHA
		for branch, sha := range progress.BranchSHAs {
			bus.Printf("  restoring %s to %s...", ui.Branch(branch), short(sha))
			if err := gitpkg.Checkout(branch); err != nil {
				bus.Warnf("  %s could not checkout %s: %v", ui.SymWarn, ui.Branch(branch), err)
				continue
			}
			if err := gitpkg.ResetHard(sha); err != nil {
				bus.Warnf("  %s could not reset %s: %v", ui.SymWarn, ui.Branch(branch), err)
				continue
			}
			bus.Printf("  %s %s restored", ui.SymOK, ui.Branch(branch))

			// Push restored branch
			if err := gitpkg.Push(branch); err != nil {
				bus.Warnf("  %s could not push %s: %v", ui.SymWarn, ui.Branch(branch), err)
			}
		}

		s.Nodes = progress.OriginalNodes
		if err := stack.Save(root, s); err != nil {
			return fmt.Errorf("cannot save stack: %w", err)
		}

		// Restore original branch
		gitpkg.Checkout(progress.OriginalBranch)
	}

	// Clear progress
	if err := stack.WithLocalLock(root, func(ls *stack.LocalState) error {
		ls.RestackProgress = nil
		return nil
	}); err != nil {
		return fmt.Errorf("cannot clear restack progress: %w", err)
	}

	bus.Printf("\n%s Restack aborted. All branches restored.", ui.SymOK)
	return nil
}

func runRestackContinue() error {
	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{})
	defer func() { _ = bus.Finish() }()

	ls, err := stack.LoadLocal(root)
	if err != nil {
		return fmt.Errorf("cannot read local state: %w", err)
	}
	if ls.RestackProgress == nil {
		return fmt.Errorf("no restack in progress")
	}

	progress := ls.RestackProgress
	s, err := stack.LoadStack(root, progress.StackID)
	if err != nil {
		return fmt.Errorf("cannot load stack: %w", err)
	}

	bus.Printf("Continuing restack in stack %s...", ui.Bold.Render(progress.StackID))

	// Resume rebasing from where we left off (no pushes yet)
	var rebased []string
	for i := progress.ResumeIndex; i < len(progress.Plan); i++ {
		a := progress.Plan[i]
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

		if s.Worktree {
			dir, err := requireBranchWorktreeDir(s, a.Branch)
			if err != nil {
				return err
			}
			if rebaseErr := gitpkg.RebaseOntoAt(dir, parentTip, oldBase, a.Branch); rebaseErr != nil {
				if conflictErr := handleMoveConflict(s, a.Branch, rebaseErr, bus, dir); conflictErr != nil {
					_ = stack.WithLocalLock(root, func(ls *stack.LocalState) error {
						if ls.RestackProgress != nil {
							ls.RestackProgress.ResumeIndex = i
						}
						return nil
					})
					_ = stack.WithLock(root, s.StackID, func(fresh *stack.Stack) error {
						fresh.Nodes = s.Nodes
						return nil
					})
					return fmt.Errorf("rebase of %s failed: %w — resolve conflicts and run `sdf restack --continue` or `sdf restack --abort`",
						a.Branch, conflictErr)
				}
			}
			newTip, _ := gitpkg.RevParseAt(dir, a.NewParent)
			node.BaseTip = newTip
		} else {
			if err := gitpkg.RebaseOnto(parentTip, oldBase, a.Branch); err != nil {
				if conflictErr := handleMoveConflict(s, a.Branch, err, bus, ""); conflictErr != nil {
					_ = stack.WithLocalLock(root, func(ls *stack.LocalState) error {
						if ls.RestackProgress != nil {
							ls.RestackProgress.ResumeIndex = i
						}
						return nil
					})
					stack.Save(root, s)
					gitpkg.Checkout(progress.OriginalBranch)
					return fmt.Errorf("rebase of %s failed: %w — resolve conflicts and run `sdf restack --continue` or `sdf restack --abort`",
						a.Branch, conflictErr)
				}
			}
			newTip, _ := gitpkg.RevParse(a.NewParent)
			node.BaseTip = newTip
		}
		rebased = append(rebased, a.Branch)

		bus.Printf("  %s %s rebased", ui.SymOK, ui.Branch(a.Branch))
	}

	// All rebases succeeded — push all affected branches
	// Include branches rebased before the pause (from the full plan)
	for i := 0; i < progress.ResumeIndex; i++ {
		rebased = append(rebased, progress.Plan[i].Branch)
	}
	bus.Print("")
	for _, branch := range rebased {
		if s.Worktree {
			dir, err := requireBranchWorktreeDir(s, branch)
			if err != nil {
				bus.Warnf("  %s could not push %s: %v", ui.SymWarn, ui.Branch(branch), err)
				continue
			}
			if err := gitpkg.PushAt(dir, branch); err != nil {
				bus.Warnf("  %s could not push %s: %v", ui.SymWarn, ui.Branch(branch), err)
			} else {
				bus.Printf("  %s %s pushed", ui.SymOK, ui.Branch(branch))
			}
		} else {
			if err := gitpkg.Push(branch); err != nil {
				bus.Warnf("  %s could not push %s: %v", ui.SymWarn, ui.Branch(branch), err)
			} else {
				bus.Printf("  %s %s pushed", ui.SymOK, ui.Branch(branch))
			}
		}
	}

	// Update PR bases
	if ghpkg.Available() {
		for _, a := range progress.Plan {
			node := s.FindNode(a.Branch)
			if node != nil && node.PR > 0 {
				if err := ghpkg.PREditBase(node.PR, a.NewParent); err != nil {
					bus.Warnf("  %s could not update PR #%d base: %v", ui.SymWarn, node.PR, err)
				} else {
					bus.Printf("  PR %s base updated -> %s", ui.PR(node.PR), ui.Branch(a.NewParent))
				}
			}
		}
	}

	// Update nav
	if err := updateStackNavForAllPRs(root, s, nil, bus); err != nil {
		bus.Warnf("warning: could not update PR navigation: %v", err)
	}

	if !s.Worktree {
		// Restore original branch (no-op in worktree mode — branches live in their own worktrees)
		gitpkg.Checkout(progress.OriginalBranch)
	}

	// Save stack and clear progress
	if s.Worktree {
		if err := stack.WithLock(root, s.StackID, func(fresh *stack.Stack) error {
			fresh.Nodes = s.Nodes
			return nil
		}); err != nil {
			return fmt.Errorf("cannot save stack: %w", err)
		}
	} else {
		if err := stack.Save(root, s); err != nil {
			return fmt.Errorf("cannot save stack: %w", err)
		}
	}
	if err := stack.WithLocalLock(root, func(ls *stack.LocalState) error {
		ls.RestackProgress = nil
		return nil
	}); err != nil {
		return fmt.Errorf("cannot clear restack progress: %w", err)
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
