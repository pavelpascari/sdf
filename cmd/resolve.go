package cmd

import (
	"fmt"
	"strings"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

// resolveStack finds the right stack based on (in order):
//  1. Explicit stack name (from --stack flag or positional arg)
//  2. Current branch membership (if on a branch that belongs to a stack)
//  3. If exactly one stack exists, use it
//
// Returns an error listing available stacks if resolution is ambiguous.
func resolveStack(root string, stackName string) (*stack.Stack, error) {
	stack.MigrateIfNeeded(root)

	if stackName != "" {
		return stack.LoadStack(root, stackName)
	}

	// Try current branch
	branch, err := gitpkg.CurrentBranch()
	if err == nil {
		s, err := stack.LoadByBranch(root, branch)
		if err == nil {
			return s, nil
		}
	}

	// If only one stack, use it
	names, err := stack.ListStacks(root)
	if err != nil {
		return nil, fmt.Errorf("cannot list stacks: %w", err)
	}

	if len(names) == 1 {
		return stack.LoadStack(root, names[0])
	}

	if len(names) == 0 {
		return nil, fmt.Errorf("no stacks found — run `sdf new <name>` to create one")
	}

	return nil, fmt.Errorf("multiple stacks found — specify with --stack <name>:\n  %s", strings.Join(names, "\n  "))
}
