package cmd

import (
	"fmt"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
)

// runWorktreeSyncStep integrates the current worktree's branch onto its parent.
// It rebases only this branch (pull model); downstream branches pick up their
// turn when their own agents run sync.
func runWorktreeSyncStep(root string, s *stack.Stack, node *stack.Node, bus *render.Bus) error {
	lock, err := stack.AcquireLock(root, s.StackID, stackLockTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	wt := node.WorktreePath
	parent := s.ParentBranch(node.Branch)

	parentTip, err := gitpkg.RevParse(parent)
	if err != nil {
		return fmt.Errorf("cannot resolve parent %s: %w", parent, err)
	}
	if parentTip == node.BaseTip {
		bus.Printf("%s %s is up to date with %s", ui.SymOK, ui.Branch(node.Branch), ui.Branch(parent))
		return nil
	}

	clean, err := gitpkg.IsCleanAt(wt)
	if err != nil {
		return err
	}
	if !clean {
		return fmt.Errorf("worktree for %s has uncommitted changes — commit your work, then run `sdf sync`", node.Branch)
	}

	rebaseOldBase := node.BaseTip
	if rebaseOldBase == "" || !gitpkg.IsAncestor(rebaseOldBase, node.Branch) {
		if mb, mbErr := gitpkg.MergeBase(parent, node.Branch); mbErr == nil && mb != "" {
			rebaseOldBase = mb
		}
	}

	bus.Printf("  rebasing %s onto %s...", ui.Branch(node.Branch), ui.Branch(parent))
	if err := gitpkg.RebaseOntoAt(wt, parent, rebaseOldBase, node.Branch); err != nil {
		// Pause for in-worktree manual resolution.
		conflicts, _ := gitpkg.ConflictedFilesAt(wt)
		local, _ := stack.LoadLocal(root)
		if local == nil {
			local = &stack.LocalState{}
		}
		local.SyncProgress = &stack.SyncProgress{
			PausedAt:     node.Branch,
			ResumeIndex:  s.NodeIndex(node.Branch),
			ParentTip:    parentTip,
			WorktreePath: wt,
		}
		_ = stack.SaveLocal(root, local)
		bus.Warnf("  %s conflict rebasing %s", ui.SymFail, ui.Branch(node.Branch))
		for _, f := range conflicts {
			bus.Printf("      %s", f)
		}
		bus.Print("  Resolve the conflicts in this worktree, stage them, then run `sdf sync --continue`.")
		return fmt.Errorf("conflict in %s", node.Branch)
	}

	if err := gitpkg.PushAt(wt, node.Branch); err != nil {
		bus.Warnf("  %s push failed for %s: %v", ui.SymFail, ui.Branch(node.Branch), err)
	} else {
		bus.Printf("  %s %s rebased and pushed", ui.SymOK, ui.Branch(node.Branch))
	}

	node.BaseTip = parentTip
	if err := stack.Save(root, s); err != nil {
		return err
	}

	// Update PR base only when the parent NAME differs from the direct parent
	// (a merged node was skipped) — same rule as the monolithic sync.
	idx := s.NodeIndex(node.Branch)
	directParent := s.Base
	if idx > 0 {
		directParent = s.Nodes[idx-1].Branch
	}
	if parent != directParent && node.PR > 0 && ghpkg.Available() {
		if err := ghpkg.PREditBase(node.PR, parent); err != nil {
			bus.Warnf("  %s could not update PR %s base: %v", ui.SymWarn, ui.PR(node.PR), err)
		}
	}

	// Tell the next agent its turn has arrived.
	if child := findNextOpenNode(s, node.Branch); child != nil {
		bus.Printf("\nDownstream %s now needs to sync (run `sdf sync` in its worktree).", ui.Branch(child.Branch))
	}
	return nil
}

// runWorktreeDashboard prints per-branch readiness when sdf sync is run from
// the main repo of a worktree-mode stack. It never rebases anything.
func runWorktreeDashboard(root string, s *stack.Stack, bus *render.Bus) error {
	bus.Printf("Stack %s (worktree mode) — branch readiness:", ui.Bold.Render(s.StackID))
	for i := range s.Nodes {
		node := &s.Nodes[i]
		if node.Status == "merged" || node.Status == "closed" {
			bus.Printf("  %s %s (%s)", ui.SymOK, ui.Branch(node.Branch), node.Status)
			continue
		}
		state := worktreeReadiness(s, node)
		bus.Printf("  %s %s — %s", state.symbol, ui.Branch(node.Branch), state.label)
	}
	bus.Print("\nRun `sdf sync` inside a branch's worktree to integrate it.")

	// Stack-wide nav refresh still runs from the main repo.
	if err := updateStackNavForAllPRs(root, s, nil, bus); err != nil {
		bus.Warnf("warning: could not update PR descriptions: %v", err)
	}
	return nil
}

type readinessState struct {
	symbol string
	label  string
}

func worktreeReadiness(s *stack.Stack, node *stack.Node) readinessState {
	if node.WorktreePath == "" {
		return readinessState{ui.SymWarn, "no worktree (run `sdf worktree enable`)"}
	}
	if inProgress, _ := gitpkg.IsRebaseInProgressAt(node.WorktreePath); inProgress {
		return readinessState{ui.SymFail, "rebase paused — resolve, then `sdf sync --continue`"}
	}
	parent := s.ParentBranch(node.Branch)
	parentTip, err := gitpkg.RevParse(parent)
	if err != nil {
		return readinessState{ui.SymWarn, "cannot resolve parent"}
	}
	if parentTip != node.BaseTip {
		return readinessState{ui.SymWarn, fmt.Sprintf("needs sync (%s advanced)", parent)}
	}
	if clean, _ := gitpkg.IsCleanAt(node.WorktreePath); !clean {
		return readinessState{ui.SymOK, "up to date (uncommitted work present)"}
	}
	return readinessState{ui.SymOK, "up to date"}
}
