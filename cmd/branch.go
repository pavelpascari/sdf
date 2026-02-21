package cmd

import (
	"fmt"
	"os"
	"slices"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/spf13/cobra"
)

var branchCmd = &cobra.Command{
	Use:         "branch <name>",
	Short:       "Add a new branch to the current stack",
	Annotations: map[string]string{"category": "stack"},
	Args:        cobra.ExactArgs(1),
	RunE:        runBranch,
}

func init() {
	rootCmd.AddCommand(branchCmd)
	branchCmd.Flags().String("stack", "", "stack to add the branch to (default: auto-detect)")
	branchCmd.Flags().Bool("no-prefix", false, "create branch without applying the configured prefix")
}

// RunBranch is a compatibility wrapper for tests.
func RunBranch(args []string) error {
	rootCmd.SetArgs(append([]string{"branch"}, args...))
	return rootCmd.Execute()
}

func runBranch(cmd *cobra.Command, args []string) error {
	stackFlag, _ := cmd.Flags().GetString("stack")
	noPrefix, _ := cmd.Flags().GetBool("no-prefix")

	branchName := args[0]

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	s, err := resolveStack(root, stackFlag)
	if err != nil {
		return err
	}

	// Load config and apply prefix
	cfg, err := cfgpkg.Load(root)
	if err != nil {
		return fmt.Errorf("cannot load config: %w", err)
	}

	if !noPrefix {
		branchName = cfgpkg.ApplyPrefix(cfg, s.StackID, branchName)
	}

	// Check if branch already exists in this stack
	if s.FindNode(branchName) != nil {
		return fmt.Errorf("branch %q already exists in stack %q", branchName, s.StackID)
	}

	// Check cross-stack uniqueness
	if err := stack.ValidateBranchUniqueness(root, branchName); err != nil {
		return err
	}

	// Detect insertion point from current branch
	currentBranch, _ := gitpkg.CurrentBranch()
	insertAfterIdx := s.NodeIndex(currentBranch)

	// Determine parent based on insertion point
	var parent string
	if insertAfterIdx >= 0 {
		// User is on a stack branch — insert after it
		parent = s.Nodes[insertAfterIdx].Branch
	} else if len(s.Nodes) > 0 {
		// User is on base or unrelated branch — append to end
		parent = s.Nodes[len(s.Nodes)-1].Branch
		insertAfterIdx = len(s.Nodes) - 1
	} else {
		// Empty stack — first branch
		parent = s.Base
		insertAfterIdx = -1
	}

	// Ensure we're on the parent before creating the branch
	if currentBranch != parent {
		if err := gitpkg.Checkout(parent); err != nil {
			return fmt.Errorf("cannot checkout parent %s: %w", parent, err)
		}
	}

	// Get the current tip of the parent for tracking
	parentTip, err := gitpkg.RevParse(parent)
	if err != nil {
		return fmt.Errorf("cannot resolve parent branch %s: %w", parent, err)
	}

	// Create the git branch
	if err := gitpkg.CreateBranch(branchName); err != nil {
		return fmt.Errorf("cannot create branch: %w", err)
	}

	// Insert node at the correct position
	insertAt := insertAfterIdx + 1
	newNode := stack.Node{
		Branch:  branchName,
		Status:  "open",
		BaseTip: parentTip,
	}
	s.Nodes = slices.Insert(s.Nodes, insertAt, newNode)

	if err := stack.Save(root, s); err != nil {
		return err
	}

	// Push tracking branch to origin
	if err := gitpkg.PushNew(branchName); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not push to origin: %v\n", err)
		fmt.Fprintln(os.Stderr, "  You can push later with: git push -u origin", branchName)
	}

	// If there's a downstream node, update its PR base on GitHub
	if insertAt < len(s.Nodes)-1 {
		downstream := &s.Nodes[insertAt+1]
		if downstream.PR > 0 && ghpkg.Available() {
			if err := ghpkg.PREditBase(downstream.PR, branchName); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not update PR #%d base: %v\n", downstream.PR, err)
			} else {
				fmt.Printf("  Updated PR #%d base → %s\n", downstream.PR, branchName)
			}
		}
	}

	fmt.Printf("Created branch %q in stack %q (based on %s)\n", branchName, s.StackID, parent)
	fmt.Println("Next: implement your changes, then run `sdf pr`.")
	return nil
}
