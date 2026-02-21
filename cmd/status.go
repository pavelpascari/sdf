package cmd

import (
	"fmt"
	"strings"

	"github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:         "status [stack-name]",
	Short:       "Show stack topology and sync state",
	Annotations: map[string]string{"category": "stack"},
	RunE:        runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().String("stack", "", "stack to show (default: auto-detect)")
}

// RunStatus is a compatibility wrapper for callers that use the old interface.
func RunStatus(args []string) error {
	rootCmd.SetArgs(append([]string{"status"}, args...))
	return rootCmd.Execute()
}

func runStatus(cmd *cobra.Command, args []string) error {
	stackFlag, _ := cmd.Flags().GetString("stack")

	// Accept positional arg as stack name: sdf status <stack-name>
	stackName := stackFlag
	if stackName == "" && len(args) > 0 {
		stackName = args[0]
	}

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	s, err := resolveStack(root, stackName)
	if err != nil {
		return err
	}

	if len(s.Nodes) == 0 {
		fmt.Printf("  %s  (base: %s)\n\n", s.StackID, s.Base)
		fmt.Println("  No branches in stack yet. Run `sdf branch <name>` to add one.")
		return nil
	}

	// Fetch and fast-forward the base branch so sync checks are accurate
	gitpkg.FetchAll()
	gitpkg.FastForward(s.Base)

	// Try to get current branch for highlighting
	currentBranch, _ := gitpkg.CurrentBranch()

	// Try to fetch PR info from GitHub
	branches := make([]string, len(s.Nodes))
	for i, n := range s.Nodes {
		branches[i] = n.Branch
	}

	var driftWarnings []string
	if gh.Available() {
		prs, err := gh.PRList(branches)
		if err == nil {
			// Build PRState list for reconciliation
			states := make([]stack.PRState, len(prs))
			for i, pr := range prs {
				states[i] = stack.PRState{
					Number:      pr.Number,
					HeadRefName: pr.HeadRefName,
					BaseRefName: pr.BaseRefName,
					State:       pr.State,
				}
			}

			// Reconcile: apply routine changes, collect notable ones
			changes := stack.ReconcileFromPRs(s, states)
			for _, c := range changes {
				if !c.Notable {
					stack.ApplyRoutineChange(s, c)
				} else {
					driftWarnings = append(driftWarnings, c.Detail)
				}
			}
			stack.Save(root, s)
		}
	}

	// Print header
	fmt.Printf("  %s  (base: %s)\n\n", ui.Bold.Render(s.StackID), ui.Branch(s.Base))

	// Print each node
	needsSync := []string{}
	for _, node := range s.Nodes {
		icon := ui.Gray.Render("●")
		status := node.Status

		switch status {
		case "merged":
			icon = ui.SymOK
		case "closed":
			icon = ui.SymFail
		}

		// Check sync state
		syncStatus := ""
		parent := s.ParentBranch(node.Branch)

		if status != "merged" && status != "closed" {
			// Check if parent has commits this branch hasn't seen
			if node.BaseTip != "" {
				currentParentTip, err := gitpkg.RevParse(parent)
				if err == nil && currentParentTip != node.BaseTip {
					syncStatus = ui.Yellow.Render("needs sync")
					needsSync = append(needsSync, node.Branch)
				} else {
					syncStatus = ui.Green.Render("in sync")
				}
			}

			// Count commits ahead
			count, err := gitpkg.CommitCount(parent, node.Branch)
			if err == nil && count != "0" {
				syncStatus = count + " commits ahead, " + syncStatus
			}
		}

		// Format the line
		marker := " "
		if node.Branch == currentBranch {
			marker = ui.Cyan.Render("→")
		}

		prInfo := ""
		if node.PR > 0 {
			prInfo = fmt.Sprintf("PR %-5s", ui.PR(node.PR))
		} else {
			prInfo = "        "
		}

		statusStr := fmt.Sprintf("%-8s", status)
		syncStr := ""
		if syncStatus != "" {
			syncStr = "  " + syncStatus
		}

		fmt.Printf(" %s %s  %-30s %s %s%s\n", marker, icon, ui.Branch(node.Branch), prInfo, statusStr, syncStr)
	}

	// Print sync suggestion
	if len(needsSync) > 0 {
		fmt.Println()
		fmt.Printf("  run `sdf sync` to rebase %s\n", strings.Join(needsSync, ", "))
	}

	// Print drift warnings
	if len(driftWarnings) > 0 {
		fmt.Println()
		fmt.Printf("  %s Drift detected:\n", ui.SymWarn)
		for _, w := range driftWarnings {
			fmt.Printf("    %s\n", w)
		}
		fmt.Printf("\n  Run `sdf fetch` to reconcile.\n")
	}

	return nil
}
