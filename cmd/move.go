package cmd

import (
	"fmt"
	"os"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

// RunMove moves commits from the current branch to its parent in the stack.
//
// Usage mirrors git cherry-pick:
//
//	sdf move <commit>...
//
// The listed commits are cherry-picked onto the parent branch, stripped from the
// current branch, and all downstream branches are cascade-rebased to keep the
// stack consistent.
func RunMove(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: sdf move <commit>...")
		fmt.Fprintln(os.Stderr, "       moves commits from current branch to its parent in the stack")
		os.Exit(1)
	}

	commits := args

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	s, err := resolveStack(root, "")
	if err != nil {
		return err
	}

	// Working tree must be clean
	clean, err := gitpkg.IsClean()
	if err != nil {
		return fmt.Errorf("cannot check working tree status: %w", err)
	}
	if !clean {
		return fmt.Errorf("working tree is not clean — commit or stash changes before moving")
	}

	// Determine which branch we're on
	branch, err := gitpkg.CurrentBranch()
	if err != nil {
		return fmt.Errorf("cannot determine current branch: %w", err)
	}

	idx := s.NodeIndex(branch)
	if idx < 0 {
		return fmt.Errorf("branch %q is not part of stack %q", branch, s.StackID)
	}

	parent := s.ParentBranch(branch)

	// Resolve every commit to a full SHA and validate it belongs to this branch
	branchCommits, err := gitpkg.LogCommits(parent, branch)
	if err != nil {
		return fmt.Errorf("cannot list commits on %s: %w", branch, err)
	}
	if len(branchCommits) == 0 {
		return fmt.Errorf("branch %s has no commits above %s", branch, parent)
	}

	commitSet := make(map[string]bool, len(branchCommits))
	for _, c := range branchCommits {
		commitSet[c] = true
	}

	resolvedCommits := make([]string, 0, len(commits))
	for _, c := range commits {
		sha, err := gitpkg.RevParse(c)
		if err != nil {
			return fmt.Errorf("cannot resolve commit %q: %w", c, err)
		}
		if !commitSet[sha] {
			return fmt.Errorf("commit %s (%s) is not on branch %s above %s", c, short(sha), branch, parent)
		}
		resolvedCommits = append(resolvedCommits, sha)
	}

	// After moving, at least one commit must remain on the branch
	if len(resolvedCommits) >= len(branchCommits) {
		return fmt.Errorf("cannot move all %d commits — branch %s would become empty", len(branchCommits), branch)
	}

	// Compute the new rebase boundary: the latest (furthest from parent)
	// commit being moved. We'll use rebase --onto to strip everything up to
	// and including this commit from the branch.
	//
	// This works correctly when moving a contiguous prefix of the branch's
	// commits. For non-contiguous selections the caller should run multiple
	// moves or use interactive rebase.
	lastMovedIdx := -1
	for i, c := range branchCommits {
		for _, rc := range resolvedCommits {
			if c == rc {
				if i > lastMovedIdx {
					lastMovedIdx = i
				}
			}
		}
	}
	lastMovedSHA := branchCommits[lastMovedIdx]

	// Verify the selected commits are a contiguous prefix
	for i := 0; i <= lastMovedIdx; i++ {
		found := false
		for _, rc := range resolvedCommits {
			if branchCommits[i] == rc {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("commits must be a contiguous prefix of %s; commit %s would be skipped — use interactive rebase for non-contiguous moves",
				branch, short(branchCommits[i]))
		}
	}

	fmt.Printf("Moving %d commit(s) from %s to %s\n", len(resolvedCommits), branch, parent)
	for _, c := range resolvedCommits {
		fmt.Printf("  %s\n", short(c))
	}

	// --- Phase 1: cherry-pick commits onto the parent ---
	fmt.Printf("\n→ cherry-picking onto %s...\n", parent)
	if err := gitpkg.Checkout(parent); err != nil {
		return fmt.Errorf("cannot checkout %s: %w", parent, err)
	}

	if err := gitpkg.CherryPick(resolvedCommits...); err != nil {
		// Cherry-pick conflict — abort and restore
		gitpkg.CherryPickAbort()
		gitpkg.Checkout(branch)
		return fmt.Errorf("cherry-pick onto %s failed (conflict): %w\nResolve manually or split into smaller moves.", parent, err)
	}

	newParentTip, err := gitpkg.RevParse(parent)
	if err != nil {
		return fmt.Errorf("cannot resolve new tip of %s: %w", parent, err)
	}
	fmt.Printf("  ✓ %s tip is now %s\n", parent, short(newParentTip))

	// --- Phase 2: strip moved commits from current branch ---
	fmt.Printf("→ rebasing %s onto updated %s...\n", branch, parent)
	if err := gitpkg.RebaseOnto(newParentTip, lastMovedSHA, branch); err != nil {
		if conflictErr := handleConflict(root, s, branch, err); conflictErr != nil {
			gitpkg.Checkout(branch)
			return fmt.Errorf("rebase of %s failed: %w", branch, conflictErr)
		}
	}

	// Update the current node's BaseTip
	s.Nodes[idx].BaseTip = newParentTip

	// --- Phase 3: cascade rebase downstream branches ---
	for i := idx + 1; i < len(s.Nodes); i++ {
		downstream := &s.Nodes[i]
		upstreamBranch := s.Nodes[i-1].Branch

		upstreamTip, err := gitpkg.RevParse(upstreamBranch)
		if err != nil {
			continue
		}

		if downstream.BaseTip != "" && downstream.BaseTip != upstreamTip {
			fmt.Printf("→ rebasing %s onto updated %s...\n", downstream.Branch, upstreamBranch)

			if err := gitpkg.RebaseOnto(upstreamTip, downstream.BaseTip, downstream.Branch); err != nil {
				if conflictErr := handleConflict(root, s, downstream.Branch, err); conflictErr != nil {
					// Save partial progress before failing
					stack.Save(root, s)
					gitpkg.Checkout(branch)
					return fmt.Errorf("cascade rebase of %s failed: %w", downstream.Branch, conflictErr)
				}
			}
			downstream.BaseTip = upstreamTip
		}
	}

	// --- Phase 4: persist stack state ---
	// Restore working branch before saving so the commit lands on the right branch
	gitpkg.Checkout(branch)

	if err := stack.Save(root, s); err != nil {
		return fmt.Errorf("cannot save stack: %w", err)
	}

	if err := gitpkg.Add(stack.StackRelPath(s)); err == nil {
		gitpkg.Commit("sdf: update stack after move")
	}

	fmt.Printf("\n✓ Moved %d commit(s) from %s to %s\n", len(resolvedCommits), branch, parent)
	return nil
}

// short returns the first 10 characters of a SHA.
func short(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}
