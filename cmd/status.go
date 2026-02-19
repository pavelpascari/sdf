package cmd

import (
	"flag"
	"fmt"
	"strings"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
)

func RunStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	stackFlag := fs.String("stack", "", "stack to show (default: auto-detect)")
	fs.Parse(args)

	// Accept positional arg as stack name: sdf status <stack-name>
	stackName := *stackFlag
	if stackName == "" && fs.NArg() > 0 {
		stackName = fs.Arg(0)
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

	// Try to get current branch for highlighting
	currentBranch, _ := gitpkg.CurrentBranch()

	// Try to fetch PR info from GitHub
	branches := make([]string, len(s.Nodes))
	for i, n := range s.Nodes {
		branches[i] = n.Branch
	}

	prMap := make(map[string]gh.PRInfo)
	if gh.Available() {
		prs, err := gh.PRList(branches)
		if err == nil {
			for _, pr := range prs {
				prMap[pr.HeadRefName] = pr
			}
		}
	}

	// Print header
	fmt.Printf("  %s  (base: %s)\n\n", s.StackID, s.Base)

	// Print each node
	needsSync := []string{}
	for i, node := range s.Nodes {
		icon := "●"
		status := node.Status

		// Update status from GitHub if available
		if pr, ok := prMap[node.Branch]; ok {
			node.PR = pr.Number
			switch strings.ToUpper(pr.State) {
			case "MERGED":
				status = "merged"
				icon = "✓"
			case "CLOSED":
				status = "closed"
				icon = "✗"
			default:
				status = "open"
			}
		}

		// Check sync state
		syncStatus := ""
		parent := s.Base
		if i > 0 {
			parent = s.Nodes[i-1].Branch
		}

		if status != "merged" && status != "closed" {
			// Check if parent has commits this branch hasn't seen
			if node.BaseTip != "" {
				currentParentTip, err := gitpkg.RevParse(parent)
				if err == nil && currentParentTip != node.BaseTip {
					syncStatus = "needs sync"
					needsSync = append(needsSync, node.Branch)
				} else {
					syncStatus = "in sync"
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
			marker = "→"
		}

		prInfo := ""
		if node.PR > 0 {
			prInfo = fmt.Sprintf("PR #%-4d", node.PR)
		} else {
			prInfo = "        "
		}

		statusStr := fmt.Sprintf("%-8s", status)
		syncStr := ""
		if syncStatus != "" {
			syncStr = "  " + syncStatus
		}

		fmt.Printf(" %s %s  %-30s %s %s%s\n", marker, icon, node.Branch, prInfo, statusStr, syncStr)
	}

	// Print sync suggestion
	if len(needsSync) > 0 {
		fmt.Println()
		fmt.Printf("  run `sdf sync` to rebase %s\n", strings.Join(needsSync, ", "))
	}

	return nil
}
