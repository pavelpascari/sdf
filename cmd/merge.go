package cmd

import (
	"flag"
	"fmt"
	"os"
	"strings"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
)

// RunMerge merges the head (bottom-most open) PR in the stack, then syncs
// the remaining branches to cascade-rebase across the merge.
//
// Usage:
//
//	sdf merge [--stack <name>] [-y] [--method squash|merge|rebase]
func RunMerge(args []string) error {
	fs := flag.NewFlagSet("merge", flag.ExitOnError)
	stackFlag := fs.String("stack", "", "stack to merge (default: auto-detect)")
	yes := fs.Bool("y", false, "skip confirmation prompt")
	method := fs.String("method", "squash", "merge method: squash, merge, or rebase")
	fs.Parse(args)

	switch *method {
	case "squash", "merge", "rebase":
	default:
		return fmt.Errorf("unknown merge method %q — use squash, merge, or rebase", *method)
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

	s, err := resolveStack(root, *stackFlag)
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

	refreshPRStates(s)
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
		ui.PR(node.PR), ui.Branch(node.Branch), ui.Branch(parent), ui.Bold.Render(*method))
	if remaining > 0 {
		fmt.Printf("  %d open PR(s) remaining after merge\n", remaining)
	} else {
		fmt.Printf("  This is the last open PR in the stack\n")
	}

	if !*yes {
		if !ui.Confirm("Proceed?") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Merge
	fmt.Printf("\n  Merging PR %s...\n", ui.PR(node.PR))
	if err := ghpkg.PRMerge(node.PR, *method); err != nil {
		return fmt.Errorf("merge failed: %w", err)
	}

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

// refreshPRStates fetches current PR states from GitHub and updates the stack.
func refreshPRStates(s *stack.Stack) {
	if !ghpkg.Available() {
		return
	}

	branches := make([]string, len(s.Nodes))
	for i, n := range s.Nodes {
		branches[i] = n.Branch
	}

	prs, err := ghpkg.PRList(branches)
	if err != nil {
		return
	}

	for _, pr := range prs {
		node := s.FindNode(pr.HeadRefName)
		if node != nil {
			node.PR = pr.Number
			if strings.ToUpper(pr.State) == "MERGED" {
				node.Status = "merged"
			}
		}
	}
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
