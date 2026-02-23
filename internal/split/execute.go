package split

import (
	"fmt"
	"os"

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

		// Extract diff for this layer's files
		patch, err := gitpkg.DiffFiles(base, source, layer.Files)
		if err != nil {
			return createdBranches, fmt.Errorf("cannot extract diff for %s: %w", layer.Name, err)
		}

		if patch == "" {
			return createdBranches, fmt.Errorf("empty diff for layer %s — no changes to apply", layer.Name)
		}

		// Apply the patch
		if err := gitpkg.ApplyPatch(patch); err != nil {
			return createdBranches, fmt.Errorf("apply failed for %s: %w", layer.Name, err)
		}

		// Stage and commit
		if err := gitpkg.Add(layer.Files...); err != nil {
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
	gitpkg.Checkout(restoreTo)
	for _, b := range branches {
		gitpkg.DeleteBranch(b)
	}
	// Remove the stack file created during Init
	os.Remove(stack.StackPath(root, stackID))
}
