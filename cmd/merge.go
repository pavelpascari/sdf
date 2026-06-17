package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
)

// MergeResult is the structured output of sdf merge when --json is used.
type MergeResult struct {
	Stack        string `json:"stack"`
	MergedPR     int    `json:"merged_pr"`
	MergedBranch string `json:"merged_branch"`
	Method       string `json:"method"`
	Base         string `json:"base"`
	Retargeted   int    `json:"retargeted_pr,omitempty"`
	Remaining    int    `json:"remaining"`
	Error        string `json:"error,omitempty"`
}

var mergeCmd = &cobra.Command{
	Use:   "merge",
	Short: "Merge head PR and sync remaining branches",
	Long: `Merges the first open PR in the stack, retargets the next PR's base,
then triggers a sync to cascade-rebase remaining branches.`,
	Example: `  sdf merge                          # merge with squash (default)
  sdf merge -y                       # skip confirmation
  sdf merge --auto                   # enable GitHub auto-merge
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
	mergeCmd.Flags().Bool("auto", false, "enable GitHub auto-merge if merge requirements are not yet met")
	mergeCmd.Flags().Bool("json", false, "output result as JSON")
	_ = mergeCmd.RegisterFlagCompletionFunc("stack", completeStackNames)
	_ = mergeCmd.RegisterFlagCompletionFunc("method", completeMergeMethods)
}

func runMergeCmd(cmd *cobra.Command, args []string) error {
	stackFlag, _ := cmd.Flags().GetString("stack")
	yes, _ := cmd.Flags().GetBool("yes")
	method, _ := cmd.Flags().GetString("method")
	autoMerge, _ := cmd.Flags().GetBool("auto")
	jsonFlag, _ := cmd.Flags().GetBool("json")

	if jsonFlag && !yes {
		return fmt.Errorf("--json requires --yes (-y) to skip confirmation")
	}

	return runMergeLogic(stackFlag, yes, method, autoMerge, jsonFlag)
}

// RunMerge is a compatibility wrapper for callers that use the old interface.
func RunMerge(args []string) error {
	rootCmd.SetArgs(append([]string{"merge"}, args...))
	return rootCmd.Execute()
}

func runMergeLogic(stackFlag string, yes bool, method string, autoMerge bool, jsonMode bool) error {
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

	var rdr render.Renderer
	if jsonMode {
		rdr = &render.JSONRenderer{}
	}
	mergeBus := render.NewBus(os.Stdout, os.Stderr, render.Options{Renderer: rdr})
	if !jsonMode {
		defer func() { _ = mergeBus.Finish() }()
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
	mergeBus.Print("Fetching from origin...")
	if err := gitpkg.FetchAll(); err != nil {
		mergeBus.Warnf("fetch failed: %v", err)
	}
	if err := gitpkg.FastForward(s.Base); err != nil {
		mergeBus.Warnf("could not fast-forward %s: %v", s.Base, err)
	}

	reconcileSyncPRStates(s, mergeBus)
	stack.Save(root, s)

	// Find head PR
	node, err := findHeadPR(s)
	if err != nil {
		return err
	}

	parent := s.ParentBranch(node.Branch)

	// Pre-merge info
	remaining := countOpen(s) - 1
	mergeBus.Printf("\nMerge PR %s (%s) into %s via %s",
		ui.PR(node.PR), ui.Branch(node.Branch), ui.Branch(parent), ui.Bold.Render(method))
	if remaining > 0 {
		mergeBus.Printf("  %d open PR(s) remaining after merge", remaining)
	} else {
		mergeBus.Print("  This is the last open PR in the stack")
	}

	if !yes {
		mergeBus.Pause()
		proceed := ui.Confirm("Proceed?")
		mergeBus.Resume()
		if !proceed {
			mergeBus.Print("Aborted.")
			return nil
		}
	}

	result := &MergeResult{
		Stack:        s.StackID,
		MergedPR:     node.PR,
		MergedBranch: node.Branch,
		Method:       method,
		Base:         parent,
		Remaining:    remaining,
	}

	// Pre-merge: retarget the next open PR's base before the merge deletes
	// the head branch. Without this, GitHub auto-closes the downstream PR
	// when the base branch is deleted by --delete-branch.
	nextNode := findNextOpenNode(s, node.Branch)
	if nextNode != nil && nextNode.PR > 0 {
		newBase := s.ParentBranch(node.Branch) // will be stack base after merge
		mergeBus.Printf("  Retargeting PR %s base → %s...", ui.PR(nextNode.PR), ui.Branch(newBase))
		if err := ghpkg.PREditBase(nextNode.PR, newBase); err != nil {
			mergeBus.Warnf("  %s could not retarget PR %s: %v", ui.SymWarn, ui.PR(nextNode.PR), err)
			mergeBus.Warnf("    The PR may be auto-closed when the branch is deleted.")
		} else {
			result.Retargeted = nextNode.PR
		}
	}

	originalBase := node.Branch
	newBase := parent

	// Merge
	mergeBus.Printf("  Merging PR %s...", ui.PR(node.PR))
	if err := ghpkg.PRMergeWithOptions(node.PR, method, autoMerge); err != nil {
		if result.Retargeted > 0 {
			mergeBus.Warnf("  %s merge failed — rolling back PR %s base to %s", ui.SymWarn, ui.PR(result.Retargeted), ui.Branch(originalBase))
			if rbErr := ghpkg.PREditBase(result.Retargeted, originalBase); rbErr != nil {
				mergeBus.Warnf("  %s rollback failed for PR %s: %v", ui.SymWarn, ui.PR(result.Retargeted), rbErr)
			}
		}
		friendly := formatMergeError(err, autoMerge, newBase)
		if jsonMode {
			result.Error = friendly
			_ = mergeBus.Finish()
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
			return nil
		}
		return fmt.Errorf("%s", friendly)
	}

	// gh pr merge --delete-branch can leave staged changes in the index
	// when it updates the local ref. Reset the index to keep the tree clean
	// for the post-merge sync.
	gitpkg.ResetHead()

	node.Status = "merged"
	if err := stack.Save(root, s); err != nil {
		return fmt.Errorf("cannot save stack: %w", err)
	}

	mergeBus.Printf("  %s PR %s merged", ui.SymOK, ui.PR(node.PR))

	// Worktree stacks: remove merged worktree and skip cascade (pull model).
	if s.Worktree {
		cleanupMergedWorktree(root, s, node, false, mergeBus)
		_ = stack.Save(root, s)
		if jsonMode {
			_ = mergeBus.Finish()
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
		}
		return nil // pull model: downstream syncs on its own turn, no cascade
	}

	// Post-merge: sync remaining branches
	if remaining > 0 {
		mergeBus.Print("\nSyncing remaining branches...")

		// Fast-forward base to include the merge commit
		if err := gitpkg.FastForward(s.Base); err != nil {
			mergeBus.Warnf("could not fast-forward %s: %v", s.Base, err)
		}

		// Run sync with confirmation skipped (user already confirmed the merge)
		if err := runSyncFull(root, s.StackID, true, false, jsonMode, true, nil, mergeBus); err != nil {
			if jsonMode {
				result.Error = fmt.Sprintf("post-merge sync failed: %v", err)
				_ = mergeBus.Finish()
				data, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(data))
				return nil
			}
			return fmt.Errorf("post-merge sync failed: %w\n\nRun `sdf sync` to retry", err)
		}
	} else {
		mergeBus.Printf("\n%s Stack %s fully merged.", ui.SymOK, ui.Bold.Render(s.StackID))
	}

	if jsonMode {
		_ = mergeBus.Finish()
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("cannot marshal result: %w", err)
		}
		fmt.Println(string(data))
	}

	return nil
}

func formatMergeError(err error, autoMerge bool, _ string) string {
	msg := err.Error()
	lower := strings.ToLower(msg)

	if strings.Contains(lower, "base branch policy prohibits the merge") ||
		strings.Contains(lower, "is not mergeable") {
		if autoMerge {
			return fmt.Sprintf("merge blocked by branch protection: %v", err)
		}
		return fmt.Sprintf("merge blocked by branch protection (required checks/reviews not satisfied). Try `sdf merge --auto` to enable GitHub auto-merge, or merge after requirements pass.\n\noriginal error: %v", err)
	}
	if strings.Contains(lower, "authentication") || strings.Contains(lower, "not logged in") {
		return fmt.Sprintf("merge failed: GitHub authentication issue. Re-authenticate with `gh auth login`.\n\noriginal error: %v", err)
	}
	if strings.Contains(lower, "network") || strings.Contains(lower, "timeout") || strings.Contains(lower, "connection") {
		return fmt.Sprintf("merge failed: network error while talking to GitHub.\n\noriginal error: %v", err)
	}
	return fmt.Sprintf("merge failed: %v", err)
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
