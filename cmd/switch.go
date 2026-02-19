package cmd

import (
	"fmt"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

// RunSwitch checks out a branch and reports which stack it belongs to.
//
// Usage:
//
//	sdf switch <branch>     Switch to a branch, showing its stack context
//	sdf switch              List all branches across all stacks
func RunSwitch(args []string) error {
	if len(args) == 0 {
		return listStackBranches()
	}

	target := args[0]

	root, err := stack.FindRoot()
	if err != nil {
		// Not in an sdf repo — plain git checkout
		return gitpkg.Checkout(target)
	}

	stack.MigrateIfNeeded(root)

	// Find which stack this branch belongs to
	s, lookupErr := stack.LoadByBranch(root, target)

	if err := gitpkg.Checkout(target); err != nil {
		if lookupErr != nil {
			return fmt.Errorf("branch %q is not in any stack and git checkout failed: %w", target, err)
		}
		return fmt.Errorf("cannot checkout %s: %w", target, err)
	}

	if lookupErr != nil {
		fmt.Printf("Switched to %s (not part of any sdf stack)\n", target)
		return nil
	}

	idx := s.NodeIndex(target)
	fmt.Printf("Switched to %s [stack: %s, layer %d/%d]\n", target, s.StackID, idx+1, len(s.Nodes))

	return nil
}

// TrySwitch attempts to switch to a branch that belongs to a known stack.
// Returns nil on success, or an error if the branch is not in any stack.
// Used by the bare `sdf <branch>` shorthand.
func TrySwitch(name string) error {
	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	stack.MigrateIfNeeded(root)

	// Only handle branches that are in a known stack
	s, err := stack.LoadByBranch(root, name)
	if err != nil {
		return err
	}

	if err := gitpkg.Checkout(name); err != nil {
		return fmt.Errorf("cannot checkout %s: %w", name, err)
	}

	idx := s.NodeIndex(name)
	fmt.Printf("Switched to %s [stack: %s, layer %d/%d]\n", name, s.StackID, idx+1, len(s.Nodes))

	return nil
}

func listStackBranches() error {
	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	stack.MigrateIfNeeded(root)

	stacks, err := stack.LoadAll(root)
	if err != nil {
		return err
	}

	if len(stacks) == 0 {
		fmt.Println("No stacks found. Run `sdf init <name>` to create one.")
		return nil
	}

	currentBranch, _ := gitpkg.CurrentBranch()

	for _, s := range stacks {
		fmt.Printf("  %s  (base: %s)\n", s.StackID, s.Base)
		for _, node := range s.Nodes {
			marker := " "
			if node.Branch == currentBranch {
				marker = "→"
			}
			fmt.Printf("   %s %s\n", marker, node.Branch)
		}
		fmt.Println()
	}

	return nil
}
