package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

var switchCmd = &cobra.Command{
	Use:   "switch [branch]",
	Short: "Switch to a branch and report its stack",
	Long: `Without arguments, lists all branches across all stacks.
With a branch name, checks it out and shows its stack position.`,
	Example: `  sdf switch db-schema              # switch to a specific branch
  sdf switch                        # list all branches from all stacks`,
	Annotations: map[string]string{"category": "navigation"},
	RunE:        runSwitchCmd,
}

func init() {
	rootCmd.AddCommand(switchCmd)
}

func runSwitchCmd(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return listStackBranches()
	}
	return runSwitchToTarget(args[0])
}

// runSwitchToTarget checks out a branch and reports which stack it belongs to.
func runSwitchToTarget(target string) error {
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

// RunSwitch is a compatibility wrapper for callers that use the old interface.
func RunSwitch(args []string) error {
	rootCmd.SetArgs(append([]string{"switch"}, args...))
	return rootCmd.Execute()
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
