package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/ops"
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
Use --abort to restore all branches to their pre-restack state.
Use --quit to drop restack state without restoring branches.`,
	Example: `  sdf restack feature/job --after feature/index
  sdf restack feature/auth --after main
  sdf restack --continue
  sdf restack --abort
  sdf restack --quit`,
	Annotations: map[string]string{"category": "stack"},
	Args:        cobra.MaximumNArgs(1),
	RunE:        runRestackCmd,
}

func init() {
	rootCmd.AddCommand(restackCmd)
	restackCmd.Flags().String("after", "", "branch to insert after")
	restackCmd.Flags().Bool("continue", false, "resume after conflict resolution")
	restackCmd.Flags().Bool("abort", false, "restore branches to pre-restack state")
	restackCmd.Flags().Bool("quit", false, "drop restack state without restoring branches")
	restackCmd.Flags().BoolP("verbose", "v", false, "show exact git commands in plan")
	restackCmd.Flags().Bool("dry-run", false, "show plan without executing")
}

func runRestackCmd(cmd *cobra.Command, args []string) error {
	contFlag, _ := cmd.Flags().GetBool("continue")
	abortFlag, _ := cmd.Flags().GetBool("abort")
	quitFlag, _ := cmd.Flags().GetBool("quit")
	verboseFlag, _ := cmd.Flags().GetBool("verbose")
	dryRunFlag, _ := cmd.Flags().GetBool("dry-run")

	exclusive := 0
	if contFlag {
		exclusive++
	}
	if abortFlag {
		exclusive++
	}
	if quitFlag {
		exclusive++
	}
	if exclusive > 1 {
		return fmt.Errorf("cannot combine --continue, --abort, and --quit")
	}

	if quitFlag {
		return runRestackQuit()
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
	return runRestackLogic(args[0], after, verboseFlag, dryRunFlag)
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

func runRestackLogic(sourceBranch, afterBranch string, verbose, dryRun bool) error {
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

	// --- Build ops.Operation ---
	operation := &Operation{
		Command:        "restack",
		StackID:        s.StackID,
		StartedAt:      time.Now(),
		OriginalBranch: originalBranch,
		Snapshot:       make(map[string]string),
		Steps:          make([]*ops.Step, 0),
	}

	// Snapshot: save current SHAs for all branches in the plan
	for _, a := range plan {
		sha, err := gitpkg.RevParse(a.Branch)
		if err == nil {
			operation.Snapshot[a.Branch] = sha
		}
	}

	// Save original nodes as CommandData (for abort/quit restore)
	originalNodes := make([]stack.Node, len(s.Nodes))
	copy(originalNodes, s.Nodes)
	origData, _ := json.Marshal(originalNodes)
	operation.CommandData = origData

	// Build a map: branch -> rebase step ID, for ref chaining
	rebaseStepIDs := make(map[string]string)
	for _, a := range plan {
		rebaseStepIDs[a.Branch] = "rebase-" + a.Branch
	}

	// Mutation: rebase steps (pushes and PR updates are done post-executor
	// because they are non-fatal — matching the original behavior where push
	// failures are warnings, not errors)
	for _, a := range plan {
		node := s.FindNode(a.Branch)
		oldBase := node.BaseTip
		if oldBase == "" {
			oldBase = a.OldParent
		}

		// Determine the "onto" input: if the new parent was rebased in an
		// earlier step, use a Ref to its new_sha output. Otherwise, resolve
		// the parent's current tip as a literal.
		var ontoVal ops.Value
		if parentStepID, ok := rebaseStepIDs[a.NewParent]; ok {
			ontoVal = ops.Ref(parentStepID + ".new_sha")
		} else {
			parentTip, err := gitpkg.RevParse(a.NewParent)
			if err != nil {
				return fmt.Errorf("cannot resolve %s: %w", a.NewParent, err)
			}
			ontoVal = ops.Lit(parentTip)
		}

		operation.Steps = append(operation.Steps, &ops.Step{
			ID:    "rebase-" + a.Branch,
			Kind:  ops.KindGitRebase,
			Phase: ops.PhaseMutation,
			Inputs: map[string]ops.Value{
				"onto":     ontoVal,
				"old_base": ops.Lit(oldBase),
				"branch":   ops.Lit(a.Branch),
			},
			Status: ops.StatusPending,
		})
	}

	// Print plan
	bus.Print("\nRestack plan:")
	for _, a := range plan {
		bus.Printf("  %s rebase %s onto %s", ui.SymPlan, ui.Branch(a.Branch), ui.Branch(a.NewParent))
	}
	bus.Print("")

	if verbose {
		bus.Print("Verbose plan:")
		bus.Print(ops.FormatPlan(operation, true))
		bus.Print("")
	}

	if dryRun {
		bus.Print("Dry run — no changes made.")
		return nil
	}

	// Apply new node order to stack before executing (so abort can restore)
	s.Nodes = newNodes

	// Save operation
	if err := ops.Save(root, operation); err != nil {
		return fmt.Errorf("cannot save operation: %w", err)
	}

	// Execute rebases via ops executor
	exec := ops.NewExecutor(operation,
		ops.WithHandler(ops.DefaultHandler),
		ops.WithPersistence(root),
	)
	if err := exec.Run(); err != nil {
		// Save stack with new node order (abort will restore original)
		stack.Save(root, s)
		gitpkg.Checkout(originalBranch)
		return fmt.Errorf("%w — resolve conflicts and run `sdf restack --continue` or `sdf restack --abort`", err)
	}

	// All rebases succeeded — update BaseTips from the actual git state
	for _, a := range plan {
		node := s.FindNode(a.Branch)
		if node == nil {
			continue
		}
		parentTip, _ := gitpkg.RevParse(a.NewParent)
		node.BaseTip = parentTip
		bus.Printf("  %s %s rebased", ui.SymOK, ui.Branch(a.Branch))
	}

	// Push all affected branches (non-fatal — warnings only)
	bus.Print("")
	for _, a := range plan {
		if err := gitpkg.Push(a.Branch); err != nil {
			bus.Warnf("  %s could not push %s: %v", ui.SymWarn, ui.Branch(a.Branch), err)
		} else {
			bus.Printf("  %s %s pushed", ui.SymOK, ui.Branch(a.Branch))
		}
	}

	// Update PR bases on GitHub (non-fatal)
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

	// Clear operation
	_ = ops.Clear(root)

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

	// Try ops-based abort first
	operation, _ := ops.Load(root)
	if operation != nil && operation.Command == "restack" {
		bus.Printf("Aborting restack in stack %s...", ui.Bold.Render(operation.StackID))

		// Abort any in-progress rebase
		gitpkg.RebaseAbort()

		// Use ops.Abort to restore snapshot branches
		if err := ops.Abort(root, ops.DefaultHandler); err != nil {
			bus.Warnf("  %s ops abort: %v", ui.SymWarn, err)
		}

		// Restore original nodes from CommandData
		if operation.CommandData != nil {
			s, loadErr := stack.LoadStack(root, operation.StackID)
			if loadErr == nil {
				var origNodes []stack.Node
				if json.Unmarshal(operation.CommandData, &origNodes) == nil {
					s.Nodes = origNodes
					stack.Save(root, s)
				}
			}
		}

		// Restore original branch
		gitpkg.Checkout(operation.OriginalBranch)

		bus.Printf("\n%s Restack aborted. All branches restored.", ui.SymOK)
		return nil
	}

	// Fall back to old RestackProgress for upgrades mid-operation
	ls, err := stack.LoadLocal(root)
	if err != nil {
		return fmt.Errorf("cannot read local state: %w", err)
	}
	if ls.RestackProgress == nil {
		return fmt.Errorf("no restack in progress")
	}

	progress := ls.RestackProgress
	bus.Printf("Aborting restack in stack %s...", ui.Bold.Render(progress.StackID))

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

	// Restore original node order and BaseTips
	s, err := stack.LoadStack(root, progress.StackID)
	if err != nil {
		return fmt.Errorf("cannot load stack: %w", err)
	}
	s.Nodes = progress.OriginalNodes
	if err := stack.Save(root, s); err != nil {
		return fmt.Errorf("cannot save stack: %w", err)
	}

	// Restore original branch
	gitpkg.Checkout(progress.OriginalBranch)

	// Clear progress
	ls.RestackProgress = nil
	if err := stack.SaveLocal(root, ls); err != nil {
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

	// Try ops-based continue first
	operation, err := ops.Continue(root)
	if err == nil && operation != nil && operation.Command == "restack" {
		s, loadErr := stack.LoadStack(root, operation.StackID)
		if loadErr != nil {
			return fmt.Errorf("cannot load stack: %w", loadErr)
		}

		bus.Printf("Continuing restack in stack %s...", ui.Bold.Render(operation.StackID))

		exec := ops.NewExecutor(operation,
			ops.WithHandler(ops.DefaultHandler),
			ops.WithPersistence(root),
		)
		if err := exec.Run(); err != nil {
			stack.Save(root, s)
			gitpkg.Checkout(operation.OriginalBranch)
			return fmt.Errorf("%w — resolve conflicts and run `sdf restack --continue` or `sdf restack --abort`", err)
		}

		// Update BaseTips and collect rebased branches
		var rebased []string
		for _, step := range operation.Steps {
			if step.Kind != ops.KindGitRebase || step.Status != ops.StatusDone {
				continue
			}
			branch := step.Inputs["branch"].Literal
			rebased = append(rebased, branch)
			node := s.FindNode(branch)
			if node == nil {
				continue
			}
			parentTip, _ := gitpkg.RevParse(s.ParentBranch(branch))
			node.BaseTip = parentTip
		}

		// Push all rebased branches (non-fatal)
		bus.Print("")
		for _, branch := range rebased {
			if err := gitpkg.Push(branch); err != nil {
				bus.Warnf("  %s could not push %s: %v", ui.SymWarn, ui.Branch(branch), err)
			} else {
				bus.Printf("  %s %s pushed", ui.SymOK, ui.Branch(branch))
			}
		}

		// Update PR bases (non-fatal)
		if ghpkg.Available() {
			for _, branch := range rebased {
				node := s.FindNode(branch)
				if node != nil && node.PR > 0 {
					newParent := s.ParentBranch(branch)
					if err := ghpkg.PREditBase(node.PR, newParent); err != nil {
						bus.Warnf("  %s could not update PR #%d base: %v", ui.SymWarn, node.PR, err)
					} else {
						bus.Printf("  PR %s base updated -> %s", ui.PR(node.PR), ui.Branch(newParent))
					}
				}
			}
		}

		// Update PR navigation
		if err := updateStackNavForAllPRs(root, s, nil, bus); err != nil {
			bus.Warnf("warning: could not update PR navigation: %v", err)
		}

		// Restore original branch
		gitpkg.Checkout(operation.OriginalBranch)

		// Save stack and clear operation
		if err := stack.Save(root, s); err != nil {
			return fmt.Errorf("cannot save stack: %w", err)
		}
		_ = ops.Clear(root)

		bus.Printf("\n%s Restack complete.", ui.SymOK)
		return nil
	}

	// Fall back to old RestackProgress for upgrades mid-operation
	ls, loadErr := stack.LoadLocal(root)
	if loadErr != nil {
		return fmt.Errorf("cannot read local state: %w", loadErr)
	}
	if ls.RestackProgress == nil {
		return fmt.Errorf("no restack in progress")
	}

	progress := ls.RestackProgress
	s, loadErr := stack.LoadStack(root, progress.StackID)
	if loadErr != nil {
		return fmt.Errorf("cannot load stack: %w", loadErr)
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

		if err := gitpkg.RebaseOnto(parentTip, oldBase, a.Branch); err != nil {
			if conflictErr := handleMoveConflict(s, a.Branch, err, bus); conflictErr != nil {
				ls.RestackProgress.ResumeIndex = i
				stack.SaveLocal(root, ls)
				stack.Save(root, s)
				gitpkg.Checkout(progress.OriginalBranch)
				return fmt.Errorf("rebase of %s failed: %w — resolve conflicts and run `sdf restack --continue` or `sdf restack --abort`",
					a.Branch, conflictErr)
			}
		}

		newTip, _ := gitpkg.RevParse(a.NewParent)
		node.BaseTip = newTip
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
		if err := gitpkg.Push(branch); err != nil {
			bus.Warnf("  %s could not push %s: %v", ui.SymWarn, ui.Branch(branch), err)
		} else {
			bus.Printf("  %s %s pushed", ui.SymOK, ui.Branch(branch))
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

	// Restore original branch
	gitpkg.Checkout(progress.OriginalBranch)

	// Save stack and clear progress
	if err := stack.Save(root, s); err != nil {
		return fmt.Errorf("cannot save stack: %w", err)
	}
	ls.RestackProgress = nil
	if err := stack.SaveLocal(root, ls); err != nil {
		return fmt.Errorf("cannot clear restack progress: %w", err)
	}

	bus.Printf("\n%s Restack complete.", ui.SymOK)
	return nil
}

func runRestackQuit() error {
	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{})
	defer func() { _ = bus.Finish() }()

	// Try ops-based quit first
	operation, _ := ops.Load(root)
	if operation != nil && operation.Command == "restack" {
		// Restore original nodes from CommandData
		if operation.CommandData != nil {
			s, loadErr := stack.LoadStack(root, operation.StackID)
			if loadErr == nil {
				var origNodes []stack.Node
				if json.Unmarshal(operation.CommandData, &origNodes) == nil {
					s.Nodes = origNodes
					stack.Save(root, s)
				}
			}
		}

		if err := ops.Quit(root); err != nil {
			return err
		}

		gitpkg.Checkout(operation.OriginalBranch)
		bus.Printf("%s Restack state cleared (branches not restored).", ui.SymOK)
		return nil
	}

	// Fall back to old RestackProgress
	ls, err := stack.LoadLocal(root)
	if err != nil {
		return fmt.Errorf("cannot read local state: %w", err)
	}
	if ls.RestackProgress == nil {
		return fmt.Errorf("no restack in progress")
	}

	progress := ls.RestackProgress

	// Restore original nodes
	s, loadErr := stack.LoadStack(root, progress.StackID)
	if loadErr == nil {
		s.Nodes = progress.OriginalNodes
		stack.Save(root, s)
	}

	gitpkg.Checkout(progress.OriginalBranch)

	ls.RestackProgress = nil
	if err := stack.SaveLocal(root, ls); err != nil {
		return fmt.Errorf("cannot clear restack progress: %w", err)
	}

	bus.Printf("%s Restack state cleared (branches not restored).", ui.SymOK)
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

// Operation is a type alias for ops.Operation used in this file.
type Operation = ops.Operation
