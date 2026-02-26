package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
)

var mergeCmd = &cobra.Command{
	Use:   "merge",
	Short: "Merge head PR and sync remaining branches",
	Long: `Merges the first open PR in the stack, retargets the next PR's base,
then triggers a sync to cascade-rebase remaining branches.`,
	Example: `  sdf merge                          # merge with squash (default)
  sdf merge -y                       # skip confirmation
  sdf merge --method merge           # use regular merge
  sdf merge --stack my-feature       # target a specific stack`,
	Annotations: map[string]string{"category": "stack"},
	RunE:        runMergeCmd,
}

func init() {
	rootCmd.AddCommand(mergeCmd)
	mergeCmd.Flags().String("stack", "", "stack to merge (default: auto-detect)")
	mergeCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")
	mergeCmd.Flags().String("method", "squash", "merge method: squash, merge, or rebase")
	mergeCmd.RegisterFlagCompletionFunc("stack", completeStackNames)
	mergeCmd.RegisterFlagCompletionFunc("method", completeMergeMethods)
}

func runMergeCmd(cmd *cobra.Command, args []string) error {
	stackFlag, _ := cmd.Flags().GetString("stack")
	yes, _ := cmd.Flags().GetBool("yes")
	method, _ := cmd.Flags().GetString("method")

	return runMergeLogic(stackFlag, yes, method)
}

// RunMerge is a compatibility wrapper for callers that use the old interface.
func RunMerge(args []string) error {
	rootCmd.SetArgs(append([]string{"merge"}, args...))
	return rootCmd.Execute()
}

func runMergeLogic(stackFlag string, yes bool, method string) error {
	switch method {
	case "squash", "merge", "rebase":
	default:
		return fmt.Errorf("unknown merge method %q — use squash, merge, or rebase", method)
	}

	if !ghpkg.Available() {
		return fmt.Errorf("gh CLI is required for merging — install it from https://cli.github.com")
	}

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	clean, err := gitpkg.IsClean()
	if err != nil {
		return fmt.Errorf("cannot check working tree status: %w", err)
	}
	if !clean {
		return fmt.Errorf("working tree is not clean — commit or stash changes before merging")
	}

	s, err := resolveStack(root, stackFlag)
	if err != nil {
		return err
	}

	// Fetch and refresh PR states
	fmt.Println("Fetching from origin...")
	if err := gitpkg.FetchAll(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: fetch failed: %v\n", err)
	}
	if err := gitpkg.FastForward(s.Base); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not fast-forward %s: %v\n", s.Base, err)
	}

	reconcileSyncPRStates(s)
	stack.Save(root, s)

	// Find head PR
	node, err := findHeadPR(s)
	if err != nil {
		return err
	}

	parent := s.ParentBranch(node.Branch)

	// Pre-merge info
	remaining := countOpen(s) - 1
	fmt.Printf("\nMerge PR %s (%s) into %s via %s\n",
		ui.PR(node.PR), ui.Branch(node.Branch), ui.Branch(parent), ui.Bold.Render(method))
	if remaining > 0 {
		fmt.Printf("  %d open PR(s) remaining after merge\n", remaining)
	} else {
		fmt.Printf("  This is the last open PR in the stack\n")
	}

	if !yes {
		if !ui.Confirm("Proceed?") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Pre-merge: retarget the next open PR's base before the merge deletes
	// the head branch. Without this, GitHub auto-closes the downstream PR
	// when the base branch is deleted by --delete-branch.
	nextNode := findNextOpenNode(s, node.Branch)
	if nextNode != nil && nextNode.PR > 0 {
		newBase := s.ParentBranch(node.Branch) // will be stack base after merge
		fmt.Printf("  Retargeting PR %s base → %s...\n", ui.PR(nextNode.PR), ui.Branch(newBase))
		if err := ghpkg.PREditBase(nextNode.PR, newBase); err != nil {
			fmt.Fprintf(os.Stderr, "  %s could not retarget PR %s: %v\n", ui.SymWarn, ui.PR(nextNode.PR), err)
			fmt.Fprintln(os.Stderr, "    The PR may be auto-closed when the branch is deleted.")
		}
	}

	// Merge
	fmt.Printf("  Merging PR %s...\n", ui.PR(node.PR))
	if err := ghpkg.PRMerge(node.PR, method); err != nil {
		return fmt.Errorf("merge failed: %w", err)
	}

	// gh pr merge --delete-branch can leave staged changes in the index
	// when it updates the local ref. Reset the index to keep the tree clean
	// for the post-merge sync.
	gitpkg.ResetHead()

	node.Status = "merged"
	if err := stack.Save(root, s); err != nil {
		return fmt.Errorf("cannot save stack: %w", err)
	}

	fmt.Printf("  %s PR %s merged\n", ui.SymOK, ui.PR(node.PR))

	// Post-merge: sync remaining branches
	if remaining > 0 {
		fmt.Println("\nSyncing remaining branches...")

		// Fast-forward base to include the merge commit
		if err := gitpkg.FastForward(s.Base); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not fast-forward %s: %v\n", s.Base, err)
		}

		// Run sync with confirmation skipped (user already confirmed the merge)
		if err := runSyncFull(root, s.StackID, true, false); err != nil {
			return fmt.Errorf("post-merge sync failed: %w\n\nRun `sdf sync` to retry", err)
		}
	} else {
		fmt.Printf("\n%s Stack %s fully merged.\n", ui.SymOK, ui.Bold.Render(s.StackID))
	}

	return nil
}

// findHeadPR returns the first open node with a PR in the stack.
func findHeadPR(s *stack.Stack) (*stack.Node, error) {
	for i := range s.Nodes {
		node := &s.Nodes[i]
		if node.Status == "merged" {
			continue
		}
		if node.PR == 0 {
			return nil, fmt.Errorf("branch %s has no PR — run `sdf pr` first", node.Branch)
		}
		return node, nil
	}
	return nil, fmt.Errorf("all PRs in stack %q are merged", s.StackID)
}

// findNextOpenNode returns the first open node after the given branch.
func findNextOpenNode(s *stack.Stack, branch string) *stack.Node {
	found := false
	for i := range s.Nodes {
		if s.Nodes[i].Branch == branch {
			found = true
			continue
		}
		if found && s.Nodes[i].Status != "merged" {
			return &s.Nodes[i]
		}
	}
	return nil
}

// countOpen returns the number of open (non-merged) PRs in the stack.
func countOpen(s *stack.Stack) int {
	count := 0
	for _, n := range s.Nodes {
		if n.Status != "merged" {
			count++
		}
	}
	return count
}
