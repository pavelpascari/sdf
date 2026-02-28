package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
	"github.com/spf13/cobra"
)

// StatusResult is the structured output of sdf status when --json is used.
type StatusResult struct {
	Stack         string             `json:"stack"`
	Base          string             `json:"base"`
	CurrentBranch string             `json:"current_branch"`
	Nodes         []StatusNodeResult `json:"nodes"`
	NeedsSync     []string           `json:"needs_sync,omitempty"`
	DriftWarnings []string           `json:"drift_warnings,omitempty"`
}

// StatusNodeResult describes a single branch in the stack status.
type StatusNodeResult struct {
	Branch       string `json:"branch"`
	PR           int    `json:"pr,omitempty"`
	Status       string `json:"status"`
	SyncState    string `json:"sync_state,omitempty"`
	CommitsAhead int    `json:"commits_ahead,omitempty"`
	IsCurrent    bool   `json:"is_current,omitempty"`
	CIStatus     string `json:"ci_status,omitempty"`
	ReviewStatus string `json:"review_status,omitempty"`
	Mergeable    string `json:"mergeable,omitempty"`
	IsDraft      bool   `json:"is_draft,omitempty"`
}

var statusCmd = &cobra.Command{
	Use:               "status [stack-name]",
	Short:             "Show stack topology and sync state",
	Annotations:       map[string]string{"category": "stack"},
	ValidArgsFunction: completeStackNames,
	RunE:              runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().String("stack", "", "stack to show (default: auto-detect)")
	statusCmd.Flags().Bool("json", false, "output result as JSON")
	_ = statusCmd.RegisterFlagCompletionFunc("stack", completeStackNames)
}

// RunStatus is a compatibility wrapper for callers that use the old interface.
func RunStatus(args []string) error {
	rootCmd.SetArgs(append([]string{"status"}, args...))
	return rootCmd.Execute()
}

func runStatus(cmd *cobra.Command, args []string) error {
	stackFlag, _ := cmd.Flags().GetString("stack")
	jsonFlag, _ := cmd.Flags().GetBool("json")

	// Accept positional arg as stack name: sdf status <stack-name>
	stackName := stackFlag
	if stackName == "" && len(args) > 0 {
		stackName = args[0]
	}

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	var rdr render.Renderer
	if jsonFlag {
		rdr = &render.JSONRenderer{}
	}
	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{Renderer: rdr})
	if !jsonFlag {
		defer func() { _ = bus.Finish() }()
	}

	s, err := resolveStack(root, stackName)
	if err != nil {
		return err
	}

	if len(s.Nodes) == 0 {
		if jsonFlag {
			result := StatusResult{
				Stack:         s.StackID,
				Base:          s.Base,
				CurrentBranch: "",
				Nodes:         []StatusNodeResult{},
			}
			_ = bus.Finish()
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("cannot marshal result: %w", err)
			}
			fmt.Println(string(data))
			return nil
		}
		bus.Printf("  %s  (base: %s)\n", s.StackID, s.Base)
		bus.Print("  No branches in stack yet. Run `sdf branch <name>` to add one.")
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
	prInfoByBranch := make(map[string]gh.PRInfo)
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
				prInfoByBranch[pr.HeadRefName] = pr
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

	// Build node results (used by both TTY and JSON)
	var needsSync []string
	var nodeResults []StatusNodeResult
	for _, node := range s.Nodes {
		nr := StatusNodeResult{
			Branch:    node.Branch,
			PR:        node.PR,
			Status:    node.Status,
			IsCurrent: node.Branch == currentBranch,
		}

		if prInfo, ok := prInfoByBranch[node.Branch]; ok {
			nr.CIStatus = gh.AggregateCheckStatus(prInfo.StatusChecks)
			nr.ReviewStatus = strings.ToLower(prInfo.ReviewDecision)
			nr.Mergeable = strings.ToLower(prInfo.Mergeable)
			nr.IsDraft = prInfo.IsDraft
		}

		status := node.Status
		parent := s.ParentBranch(node.Branch)

		if status != "merged" && status != "closed" {
			if node.BaseTip != "" {
				currentParentTip, err := gitpkg.RevParse(parent)
				if err == nil && currentParentTip != node.BaseTip {
					nr.SyncState = "needs_sync"
					needsSync = append(needsSync, node.Branch)
				} else {
					nr.SyncState = "in_sync"
				}
			}

			count, err := gitpkg.CommitCount(parent, node.Branch)
			if err == nil && count != "0" {
				fmt.Sscanf(count, "%d", &nr.CommitsAhead)
			}
		}

		nodeResults = append(nodeResults, nr)
	}

	if jsonFlag {
		result := StatusResult{
			Stack:         s.StackID,
			Base:          s.Base,
			CurrentBranch: currentBranch,
			Nodes:         nodeResults,
			NeedsSync:     needsSync,
			DriftWarnings: driftWarnings,
		}
		_ = bus.Finish()
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("cannot marshal result: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	// TTY output
	bus.Printf("  %s  (base: %s)\n", ui.Bold.Render(s.StackID), ui.Branch(s.Base))

	for _, nr := range nodeResults {
		icon := ui.Gray.Render("●")
		switch nr.Status {
		case "merged":
			icon = ui.SymOK
		case "closed":
			icon = ui.SymFail
		}

		syncStatus := ""
		switch nr.SyncState {
		case "needs_sync":
			syncStatus = ui.Yellow.Render("needs sync")
		case "in_sync":
			syncStatus = ui.Green.Render("in sync")
		}
		if nr.CommitsAhead > 0 {
			syncStatus = fmt.Sprintf("%d commits ahead, %s", nr.CommitsAhead, syncStatus)
		}

		marker := " "
		if nr.IsCurrent {
			marker = ui.Cyan.Render("→")
		}

		var prInfo string
		if nr.PR > 0 {
			prInfo = fmt.Sprintf("PR %-5s", ui.PR(nr.PR))
		} else {
			prInfo = "        "
		}

		statusStr := fmt.Sprintf("%-8s", nr.Status)
		syncStr := ""
		if syncStatus != "" {
			syncStr = "  " + syncStatus
		}

		mergeStr := formatMergeability(nr)

		bus.Printf(" %s %s  %-30s %s %s%s%s", marker, icon, ui.Branch(nr.Branch), prInfo, statusStr, syncStr, mergeStr)
	}

	// Print sync suggestion
	if len(needsSync) > 0 {
		bus.Print("")
		bus.Printf("  run `sdf sync` to rebase %s", strings.Join(needsSync, ", "))
	}

	// Print drift warnings
	if len(driftWarnings) > 0 {
		bus.Print("")
		bus.Printf("  %s Drift detected:", ui.SymWarn)
		for _, w := range driftWarnings {
			bus.Printf("    %s", w)
		}
		bus.Print("")
		bus.Print("  Run `sdf fetch` to reconcile.")
	}

	return nil
}

// formatMergeability renders compact [CI:x] [R:x] badges for TTY output.
func formatMergeability(nr StatusNodeResult) string {
	// Only show for open PRs with a PR number
	if nr.PR == 0 || nr.Status == "merged" || nr.Status == "closed" {
		return ""
	}

	if nr.IsDraft {
		return "  " + ui.Gray.Render("draft")
	}

	if nr.Mergeable == "conflicting" {
		return "  " + ui.Red.Render("[conflict]")
	}

	// CI badge
	var ciBadge string
	switch nr.CIStatus {
	case "pass":
		ciBadge = ui.Green.Render("[CI:✓]")
	case "fail":
		ciBadge = ui.Red.Render("[CI:✗]")
	case "pending":
		ciBadge = ui.Yellow.Render("[CI:⏳]")
	default:
		ciBadge = ui.Gray.Render("[CI:–]")
	}

	// Review badge
	var reviewBadge string
	switch nr.ReviewStatus {
	case "approved":
		reviewBadge = ui.Green.Render("[R:✓]")
	case "changes_requested":
		reviewBadge = ui.Red.Render("[R:✗]")
	case "review_required":
		reviewBadge = ui.Yellow.Render("[R:?]")
	default:
		reviewBadge = ui.Gray.Render("[R:–]")
	}

	return "  " + ciBadge + " " + reviewBadge
}
