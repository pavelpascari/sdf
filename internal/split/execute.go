package split

import (
	"fmt"
	"os"
	"strings"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

// Execute creates branches and applies per-layer diffs for a validated plan.
// Returns the list of created branch names. On failure, the caller is
// responsible for cleanup (use Cleanup).
func Execute(plan *Plan, stackID, base, source, root string) ([]string, error) {
	if err := stack.MigrateIfNeeded(root); err != nil {
		return nil, fmt.Errorf("cannot migrate stack layout: %w", err)
	}

	if err := stack.Init(root, stackID, base); err != nil {
		return nil, fmt.Errorf("cannot initialize stack: %w", err)
	}

	cfg, err := cfgpkg.Load(root)
	if err != nil {
		cfg = cfgpkg.Defaults()
	}

	s, err := stack.LoadStack(root, stackID)
	if err != nil {
		return nil, fmt.Errorf("cannot load stack: %w", err)
	}

	var createdBranches []string
	parent := base

	for i, layer := range plan.Layers {
		shortName := fmt.Sprintf("%d-%s", i+1, layer.Name)
		branchName := cfgpkg.ApplyPrefix(cfg, stackID, shortName)

		if err := gitpkg.Checkout(parent); err != nil {
			return createdBranches, fmt.Errorf("cannot checkout %s: %w", parent, err)
		}

		if err := gitpkg.CreateBranch(branchName); err != nil {
			return createdBranches, fmt.Errorf("cannot create branch %s: %w", branchName, err)
		}
		createdBranches = append(createdBranches, branchName)

		// Build the combined patch for this layer
		var patchParts []string

		// Whole files — extract their full diff
		if len(layer.Files) > 0 {
			wholePatch, err := gitpkg.DiffFiles(base, source, layer.Files)
			if err != nil {
				return createdBranches, fmt.Errorf("cannot extract diff for %s: %w", layer.Name, err)
			}
			if wholePatch != "" {
				patchParts = append(patchParts, wholePatch)
			}
		}

		// Partial files — extract and filter hunks
		for _, pf := range layer.PartialFiles {
			fileDiff, err := gitpkg.DiffFiles(base, source, []string{pf.Path})
			if err != nil {
				return createdBranches, fmt.Errorf("cannot extract diff for %s in %s: %w", pf.Path, layer.Name, err)
			}
			parsed := ParseDiff(fileDiff)
			if len(parsed) == 0 {
				return createdBranches, fmt.Errorf("no diff found for partial file %s in layer %s", pf.Path, layer.Name)
			}
			filtered := FilterHunks(parsed[0], pf.Hunks)
			if filtered != "" {
				patchParts = append(patchParts, filtered)
			}
		}

		if len(patchParts) == 0 {
			return createdBranches, fmt.Errorf("empty diff for layer %s — no changes to apply", layer.Name)
		}

		patch := strings.Join(patchParts, "")

		// Apply the patch
		if err := gitpkg.ApplyPatch(patch); err != nil {
			return createdBranches, fmt.Errorf("apply failed for %s: %w", layer.Name, err)
		}

		// Stage and commit all files (whole + partial)
		allFiles := layer.AllFilePaths()
		if err := gitpkg.Add(allFiles...); err != nil {
			return createdBranches, fmt.Errorf("cannot stage files for %s: %w", layer.Name, err)
		}

		if err := gitpkg.Commit(layer.Description); err != nil {
			return createdBranches, fmt.Errorf("cannot commit %s: %w", layer.Name, err)
		}

		// Record node in stack
		parentTip, _ := gitpkg.RevParse(parent)
		s.Nodes = append(s.Nodes, stack.Node{
			Branch:  branchName,
			Status:  "open",
			BaseTip: parentTip,
		})

		parent = branchName
	}

	if err := stack.Save(root, s); err != nil {
		return createdBranches, fmt.Errorf("cannot save stack: %w", err)
	}

	return createdBranches, nil
}

// ExecuteWorktreeFunc is a function that creates a worktree for a new node.
// The caller (cmd layer) provides this to avoid the internal/split package
// importing cmd-level helpers.
type ExecuteWorktreeFunc func(node *stack.Node, createFrom string) error

// ExecuteWorktree is the worktree-mode counterpart of Execute. Each layer
// branch is materialized as a git worktree (via addFn) rather than a plain
// checkout. All git operations (apply, add, commit) run inside the branch's
// worktree directory so the main-repo checkout is never disturbed.
func ExecuteWorktree(plan *Plan, stackID, base, source, root string, addFn ExecuteWorktreeFunc) ([]string, error) {
	if err := stack.MigrateIfNeeded(root); err != nil {
		return nil, fmt.Errorf("cannot migrate stack layout: %w", err)
	}

	if err := stack.Init(root, stackID, base); err != nil {
		return nil, fmt.Errorf("cannot initialize stack: %w", err)
	}

	// Mark the stack as worktree-mode immediately.
	if err := stack.WithLock(root, stackID, func(s *stack.Stack) error {
		s.Worktree = true
		return nil
	}); err != nil {
		return nil, fmt.Errorf("cannot enable worktree mode: %w", err)
	}

	cfg, err := cfgpkg.Load(root)
	if err != nil {
		cfg = cfgpkg.Defaults()
	}

	s, err := stack.LoadStack(root, stackID)
	if err != nil {
		return nil, fmt.Errorf("cannot load stack: %w", err)
	}

	var createdBranches []string
	parent := base

	for i, layer := range plan.Layers {
		shortName := fmt.Sprintf("%d-%s", i+1, layer.Name)
		branchName := cfgpkg.ApplyPrefix(cfg, stackID, shortName)

		parentTip, _ := gitpkg.RevParse(parent)

		node := stack.Node{
			Branch:  branchName,
			Status:  "open",
			BaseTip: parentTip,
		}

		// Materialize the branch as a worktree branching from parent.
		if err := addFn(&node, parent); err != nil {
			return createdBranches, fmt.Errorf("cannot create worktree for %s: %w", branchName, err)
		}
		createdBranches = append(createdBranches, branchName)

		wtDir := node.WorktreePath

		// Build the combined patch for this layer.
		var patchParts []string

		if len(layer.Files) > 0 {
			wholePatch, err := gitpkg.DiffFiles(base, source, layer.Files)
			if err != nil {
				return createdBranches, fmt.Errorf("cannot extract diff for %s: %w", layer.Name, err)
			}
			if wholePatch != "" {
				patchParts = append(patchParts, wholePatch)
			}
		}

		for _, pf := range layer.PartialFiles {
			fileDiff, err := gitpkg.DiffFiles(base, source, []string{pf.Path})
			if err != nil {
				return createdBranches, fmt.Errorf("cannot extract diff for %s in %s: %w", pf.Path, layer.Name, err)
			}
			parsed := ParseDiff(fileDiff)
			if len(parsed) == 0 {
				return createdBranches, fmt.Errorf("no diff found for partial file %s in layer %s", pf.Path, layer.Name)
			}
			filtered := FilterHunks(parsed[0], pf.Hunks)
			if filtered != "" {
				patchParts = append(patchParts, filtered)
			}
		}

		if len(patchParts) == 0 {
			return createdBranches, fmt.Errorf("empty diff for layer %s — no changes to apply", layer.Name)
		}

		patch := strings.Join(patchParts, "")

		if err := gitpkg.ApplyPatchAt(wtDir, patch); err != nil {
			return createdBranches, fmt.Errorf("apply failed for %s: %w", layer.Name, err)
		}

		allFiles := layer.AllFilePaths()
		if err := gitpkg.AddAt(wtDir, allFiles...); err != nil {
			return createdBranches, fmt.Errorf("cannot stage files for %s: %w", layer.Name, err)
		}

		if err := gitpkg.CommitAt(wtDir, layer.Description); err != nil {
			return createdBranches, fmt.Errorf("cannot commit %s: %w", layer.Name, err)
		}

		s.Nodes = append(s.Nodes, node)
		parent = branchName
	}

	if err := stack.Save(root, s); err != nil {
		return createdBranches, fmt.Errorf("cannot save stack: %w", err)
	}

	return createdBranches, nil
}

// ValidateTree checks that two refs have identical trees.
// Returns nil if they match, an error if they differ.
func ValidateTree(source, lastBranch string) error {
	diff, err := gitpkg.DiffFull(source, lastBranch)
	if err != nil {
		return fmt.Errorf("cannot verify split: %w", err)
	}
	if diff != "" {
		return fmt.Errorf("tree differs from original branch (this is a bug)")
	}
	return nil
}

// Cleanup deletes created branches, removes the stack file, and restores
// the original branch. Safe to call with empty branches or missing stack.
func Cleanup(branches []string, restoreTo, root, stackID string) {
	_ = gitpkg.Checkout(restoreTo)
	for _, b := range branches {
		_ = gitpkg.DeleteBranch(b)
	}
	// Remove the stack file created during Init
	_ = os.Remove(stack.StackPath(root, stackID))
}
