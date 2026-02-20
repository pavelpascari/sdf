package stack

import "fmt"

// ValidateBranchUniqueness checks that a branch name is not already used
// in any existing stack. Returns an error identifying the conflicting stack
// if the branch is found, nil if unique.
func ValidateBranchUniqueness(root, branch string) error {
	stacks, err := LoadAll(root)
	if err != nil {
		// If no stacks directory exists, the branch is unique by definition
		return nil
	}

	for _, s := range stacks {
		if s.FindNode(branch) != nil {
			return fmt.Errorf("branch %q already belongs to stack %q", branch, s.StackID)
		}
	}
	return nil
}
