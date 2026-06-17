package cmd

import (
	"fmt"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
)

// clearWorktreeProgress removes one branch's entry from WorktreeProgress under
// the repo-wide local lock. Callers may hold a stack lock when calling this
// (stack-outer / local-inner — the allowed nesting order). The body is tiny and
// git-free, never acquiring a stack lock.
func clearWorktreeProgress(root, branch string) error {
	return stack.WithLocalLock(root, func(ls *stack.LocalState) error {
		delete(ls.WorktreeProgress, branch)
		if len(ls.WorktreeProgress) == 0 {
			ls.WorktreeProgress = nil
		}
		return nil
	})
}

// continueWorktreeSync finishes a paused in-worktree rebase for the given
// branch. It reads progress from WorktreeProgress[branch] under the lock,
// does the rebase-state detection in the worktree, then acquires the stack
// lock for the BaseTip update, push, save, and WorktreeProgress cleanup.
func continueWorktreeSync(root, stackID, branch string, bus *render.Bus) error {
	// Read progress outside the lock first (read-only; detect rebase state).
	local0, _ := stack.LoadLocal(root)
	if local0 == nil || local0.WorktreeProgress == nil || local0.WorktreeProgress[branch] == nil {
		return fmt.Errorf("no paused worktree sync for %s — run `sdf sync` in this worktree", branch)
	}
	progress := local0.WorktreeProgress[branch]
	wt := progress.WorktreePath

	switch inProg, _ := gitpkg.IsRebaseInProgressAt(wt); {
	case inProg:
		bus.Printf("  rebasing %s (continuing)...", ui.Branch(progress.PausedAt))
		if err := gitpkg.RebaseContinueAt(wt); err != nil {
			return fmt.Errorf("rebase --continue failed: %w\n\nResolve remaining conflicts, stage them, and run `sdf sync --continue` again", err)
		}
	case gitpkg.IsAncestor(progress.ParentTip, progress.PausedAt):
		bus.Printf("  %s %s rebased (completed outside sdf)", ui.SymOK, ui.Branch(progress.PausedAt))
	default:
		bus.Printf("Rebase of %s was aborted. Clearing paused state.", ui.Branch(progress.PausedAt))
		// Clear only this branch's entry. The stack lock guards the stack file;
		// the WorktreeProgress mutation goes through the repo-wide local lock
		// (stack-outer / local-inner, the allowed nesting order).
		lockErr := stack.WithLock(root, stackID, func(_ *stack.Stack) error {
			return clearWorktreeProgress(root, branch)
		})
		return lockErr
	}

	var childBranch string
	lockErr := stack.WithLock(root, stackID, func(s *stack.Stack) error {
		node := s.FindNode(progress.PausedAt)
		if node != nil {
			node.BaseTip = progress.ParentTip
			if err := gitpkg.PushAt(wt, node.Branch); err != nil {
				bus.Warnf("  %s push failed for %s: %v", ui.SymFail, ui.Branch(node.Branch), err)
			} else {
				bus.Printf("  %s %s rebased and pushed", ui.SymOK, ui.Branch(node.Branch))
			}
			// stack.Save is NOT called here; WithLock saves on nil return.
		}
		// Clear this branch's entry from WorktreeProgress via the repo-wide
		// local lock (nested inside the stack lock — allowed order).
		if err := clearWorktreeProgress(root, branch); err != nil {
			return err
		}
		if child := findNextOpenNode(s, progress.PausedAt); child != nil {
			childBranch = child.Branch
		}
		return nil
	})
	if lockErr != nil {
		return lockErr
	}

	if childBranch != "" {
		bus.Printf("\nDownstream %s now needs to sync.", ui.Branch(childBranch))
	}
	return nil
}

// runWorktreeSyncStep integrates the current worktree's branch onto its parent.
// It rebases only this branch (pull model); downstream branches pick up their
// turn when their own agents run sync.
//
// The function is split into three phases to avoid holding the advisory lock
// across network operations:
//  1. Pre-lock: load base branch name, fetch from origin, fast-forward base.
//  2. Locked: reload a fresh stack, mutate node.BaseTip, push, save.
//  3. Post-lock: reload stack read-only, refresh PR nav links.
func runWorktreeSyncStep(root, stackID, branch string, bus *render.Bus) error {
	// --- Phase 1: fetch/ff BEFORE acquiring the lock ---
	// Quick read-only load to discover the base branch name.
	sForBase, err := stack.LoadStack(root, stackID)
	if err != nil {
		return err
	}
	base := sForBase.Base

	bus.Printf("  fetching from origin...")
	if fetchErr := gitpkg.FetchAll(); fetchErr != nil {
		bus.Warnf("  warning: fetch failed: %v", fetchErr)
	}
	if ffErr := gitpkg.FastForward(base); ffErr != nil {
		bus.Warnf("  warning: could not fast-forward %s: %v", base, ffErr)
	}

	// --- Phase 2: acquire lock, reload fresh, mutate ---
	worked := false
	var childBranch string

	lockErr := stack.WithLock(root, stackID, func(s *stack.Stack) error {
		node := s.FindNode(branch)
		if node == nil {
			return fmt.Errorf("branch %s not found in stack %s", branch, stackID)
		}
		wt := node.WorktreePath
		parent := s.ParentBranch(branch)

		parentTip, err := gitpkg.RevParse(parent)
		if err != nil {
			return fmt.Errorf("cannot resolve parent %s: %w", parent, err)
		}
		if parentTip == node.BaseTip {
			bus.Printf("%s %s is up to date with %s", ui.SymOK, ui.Branch(branch), ui.Branch(parent))
			return nil // no-op; worked stays false
		}

		clean, err := gitpkg.IsCleanAt(wt)
		if err != nil {
			return err
		}
		if !clean {
			return fmt.Errorf("worktree for %s has uncommitted changes — commit your work, then run `sdf sync`", branch)
		}

		rebaseOldBase := node.BaseTip
		if rebaseOldBase == "" || !gitpkg.IsAncestor(rebaseOldBase, branch) {
			if mb, mbErr := gitpkg.MergeBase(parent, branch); mbErr == nil && mb != "" {
				rebaseOldBase = mb
			}
		}

		bus.Printf("  rebasing %s onto %s...", ui.Branch(branch), ui.Branch(parent))
		if err := gitpkg.RebaseOntoAt(wt, parent, rebaseOldBase, branch); err != nil {
			// Pause for in-worktree manual resolution.
			// Write WorktreeProgress through the repo-wide local lock so it is
			// never clobbered by a concurrent sdf process in another worktree.
			// Nested inside the stack lock (stack-outer / local-inner — allowed).
			conflicts, _ := gitpkg.ConflictedFilesAt(wt)
			progress := &stack.SyncProgress{
				PausedAt:     branch,
				ResumeIndex:  s.NodeIndex(branch),
				ParentTip:    parentTip,
				WorktreePath: wt,
			}
			_ = stack.WithLocalLock(root, func(ls *stack.LocalState) error {
				if ls.WorktreeProgress == nil {
					ls.WorktreeProgress = make(map[string]*stack.SyncProgress)
				}
				ls.WorktreeProgress[branch] = progress
				return nil
			})
			bus.Warnf("  %s conflict rebasing %s", ui.SymFail, ui.Branch(branch))
			for _, f := range conflicts {
				bus.Printf("      %s", f)
			}
			bus.Print("  Resolve the conflicts in this worktree, stage them, then run `sdf sync --continue`.")
			return fmt.Errorf("conflict in %s", branch)
		}

		if err := gitpkg.PushAt(wt, branch); err != nil {
			bus.Warnf("  %s push failed for %s: %v", ui.SymFail, ui.Branch(branch), err)
		} else {
			bus.Printf("  %s %s rebased and pushed", ui.SymOK, ui.Branch(branch))
		}

		node.BaseTip = parentTip

		// Update PR base only when the parent NAME differs from the direct
		// parent (a merged node was skipped) — same rule as the monolithic sync.
		idx := s.NodeIndex(branch)
		directParent := s.Base
		if idx > 0 {
			directParent = s.Nodes[idx-1].Branch
		}
		if parent != directParent && node.PR > 0 && ghpkg.Available() {
			if err := ghpkg.PREditBase(node.PR, parent); err != nil {
				bus.Warnf("  %s could not update PR %s base: %v", ui.SymWarn, ui.PR(node.PR), err)
			}
		}

		// Capture downstream info before the lock is released.
		if child := findNextOpenNode(s, branch); child != nil {
			childBranch = child.Branch
		}
		worked = true
		return nil // stack.WithLock saves on nil return
	})
	if lockErr != nil {
		return lockErr
	}

	if !worked {
		return nil
	}

	// --- Phase 3: post-lock — refresh PR nav links ---
	// Reload the stack read-only so nav sees the updated BaseTip.
	freshStack, err := stack.LoadStack(root, stackID)
	if err != nil {
		bus.Warnf("  warning: could not reload stack for nav update: %v", err)
	} else {
		if err := updateStackNavForAllPRs(root, freshStack, nil, bus); err != nil {
			bus.Warnf("  warning: could not update PR descriptions: %v", err)
		}
	}

	// Tell the next agent its turn has arrived.
	if childBranch != "" {
		bus.Printf("\nDownstream %s now needs to sync (run `sdf sync` in its worktree).", ui.Branch(childBranch))
	}
	return nil
}

// cleanupMergedWorktree removes the worktree of a just-merged node and reports
// which downstream worktree now needs to sync. Safe to call only for worktree stacks.
func cleanupMergedWorktree(root string, s *stack.Stack, node *stack.Node, force bool, bus *render.Bus) {
	if node.WorktreePath == "" {
		return
	}
	if removable, _ := gitpkg.IsWorktreeRemovable(node.WorktreePath); !removable && !force {
		bus.Warnf("  %s worktree for %s has uncommitted changes or untracked files; leaving it in place (use `git worktree remove --force` to drop)", ui.SymWarn, ui.Branch(node.Branch))
		return
	}
	if err := removeWorktreeForNode(root, node, force); err != nil {
		bus.Warnf("  %s could not remove worktree for %s: %v", ui.SymWarn, ui.Branch(node.Branch), err)
		return
	}
	bus.Printf("  %s removed worktree for %s", ui.SymOK, ui.Branch(node.Branch))
	if child := findNextOpenNode(s, node.Branch); child != nil {
		bus.Printf("  Downstream %s now needs to sync (run `sdf sync` in its worktree).", ui.Branch(child.Branch))
	}
}

// runWorktreeDashboard prints per-branch readiness when sdf sync is run from
// the main repo of a worktree-mode stack. It never rebases anything.
func runWorktreeDashboard(root string, s *stack.Stack, bus *render.Bus) error {
	// Fetch from origin and fast-forward the base so readiness reflects the
	// real upstream state (not just the stale local refs).
	if fetchErr := gitpkg.FetchAll(); fetchErr != nil {
		bus.Warnf("  warning: fetch failed: %v", fetchErr)
	}
	if ffErr := gitpkg.FastForward(s.Base); ffErr != nil {
		bus.Warnf("  warning: could not fast-forward %s: %v", s.Base, ffErr)
	}

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
