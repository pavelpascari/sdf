package cmd

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	ctxpkg "github.com/pavelpascari/sdf/internal/context"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
)

// syncAction represents a single planned operation during sync.
type syncAction struct {
	kind   string // "remove-merged", "rebase", "push", "update-pr-base"
	branch string
	onto   string // target base for rebase or PR base update
	pr     int    // PR number (for update-pr-base)
}

func RunSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	yes := fs.Bool("y", false, "skip confirmation prompt")
	stackFlag := fs.String("stack", "", "stack to sync (default: auto-detect)")
	fs.Parse(args)

	// Accept positional arg as stack name: sdf sync <stack-name>
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
		fmt.Printf("No branches in stack %q. Nothing to sync.\n", s.StackID)
		return nil
	}

	// Check for clean working tree
	clean, err := gitpkg.IsClean()
	if err != nil {
		return fmt.Errorf("cannot check working tree status: %w", err)
	}
	if !clean {
		return fmt.Errorf("working tree is not clean — commit or stash changes before syncing")
	}

	// Remember current branch to restore later
	originalBranch, err := gitpkg.CurrentBranch()
	if err != nil {
		return fmt.Errorf("cannot determine current branch: %w", err)
	}

	// Fetch latest from origin
	fmt.Printf("Syncing stack %q...\n", s.StackID)
	fmt.Println("Fetching from origin...")
	if err := gitpkg.FetchAll(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: fetch failed: %v\n", err)
	}

	// Poll PR states from GitHub
	if ghpkg.Available() {
		branches := make([]string, len(s.Nodes))
		for i, n := range s.Nodes {
			branches[i] = n.Branch
		}

		prs, err := ghpkg.PRList(branches)
		if err == nil {
			for _, pr := range prs {
				node := s.FindNode(pr.HeadRefName)
				if node != nil {
					node.PR = pr.Number
					if strings.ToUpper(pr.State) == "MERGED" {
						node.Status = "merged"
					}
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "warning: could not poll PR states: %v\n", err)
		}
	}

	// Compute the sync plan
	plan := computeSyncPlan(s)
	if len(plan) == 0 {
		fmt.Println("\nEverything is in sync.")
		return nil
	}

	// Display the plan
	printSyncPlan(plan)

	// Confirm unless -y was passed
	if !*yes {
		if !confirmSync() {
			fmt.Println("Aborted.")
			return nil
		}
	}

	fmt.Println()

	// Execute: process merged nodes and cascade rebases
	modified := false
	for i := 0; i < len(s.Nodes); i++ {
		node := &s.Nodes[i]

		if node.Status == "merged" {
			// This node is merged — the next node needs to rebase
			fmt.Printf("  ✓ %s is merged\n", node.Branch)

			// If there's a next node, rebase it
			if i+1 < len(s.Nodes) {
				next := &s.Nodes[i+1]
				newBase := s.Base

				// The new base for the node after a merged node is the stack base
				// (since the merged node's commits are now in the base)
				fmt.Printf("  → rebasing %s onto %s...\n", next.Branch, newBase)

				oldBase := node.Branch
				if err := gitpkg.RebaseOnto(newBase, oldBase, next.Branch); err != nil {
					// Rebase conflict
					if err := handleConflict(root, s, next.Branch, err); err != nil {
						// Restore original branch on failure
						gitpkg.Checkout(originalBranch)
						return fmt.Errorf("rebase of %s failed: %w", next.Branch, err)
					}
				}

				// Update base tip
				newTip, _ := gitpkg.RevParse(newBase)
				next.BaseTip = newTip

				// Push the rebased branch
				fmt.Printf("  → pushing %s...\n", next.Branch)
				if err := gitpkg.Push(next.Branch); err != nil {
					fmt.Fprintf(os.Stderr, "  warning: push failed for %s: %v\n", next.Branch, err)
				}

				// Update PR base in GitHub
				if next.PR > 0 && ghpkg.Available() {
					fmt.Printf("  → updating PR #%d base to %s\n", next.PR, newBase)
					if err := ghpkg.PREditBase(next.PR, newBase); err != nil {
						fmt.Fprintf(os.Stderr, "  warning: could not update PR base: %v\n", err)
					}
				}
			}

			// Remove the merged node
			s.Nodes = append(s.Nodes[:i], s.Nodes[i+1:]...)
			i-- // Adjust index after removal
			modified = true
			continue
		}

		// Check if this non-merged node needs rebasing onto its parent
		parent := s.Base
		if i > 0 {
			parent = s.Nodes[i-1].Branch
		}

		currentParentTip, err := gitpkg.RevParse(parent)
		if err != nil {
			continue
		}

		if node.BaseTip != "" && currentParentTip != node.BaseTip {
			fmt.Printf("  → rebasing %s onto updated %s...\n", node.Branch, parent)

			if err := gitpkg.RebaseOnto(parent, node.BaseTip, node.Branch); err != nil {
				if err := handleConflict(root, s, node.Branch, err); err != nil {
					gitpkg.Checkout(originalBranch)
					return fmt.Errorf("rebase of %s failed: %w", node.Branch, err)
				}
			}

			node.BaseTip = currentParentTip
			modified = true

			// Push the rebased branch
			fmt.Printf("  → pushing %s...\n", node.Branch)
			if err := gitpkg.Push(node.Branch); err != nil {
				fmt.Fprintf(os.Stderr, "  warning: push failed for %s: %v\n", node.Branch, err)
			}

			// Update PR base if needed
			if node.PR > 0 && ghpkg.Available() {
				fmt.Printf("  → updating PR #%d base to %s\n", node.PR, parent)
				if err := ghpkg.PREditBase(node.PR, parent); err != nil {
					fmt.Fprintf(os.Stderr, "  warning: could not update PR base: %v\n", err)
				}
			}
		}
	}

	if modified {
		// Save updated stack
		if err := stack.Save(root, s); err != nil {
			return fmt.Errorf("cannot save stack: %w", err)
		}

		// Commit the updated stack file
		if err := gitpkg.Add(stack.StackRelPath(s)); err == nil {
			gitpkg.Commit("sdf: update stack after sync")
		}

		fmt.Println("\nSync complete. Stack updated.")
	} else {
		fmt.Println("\nEverything is in sync.")
	}

	// Restore original branch
	gitpkg.Checkout(originalBranch)

	return nil
}

// computeSyncPlan determines what operations sync will perform without
// executing them. It walks the stack nodes using the same logic as the
// execution phase to predict rebases, pushes, and PR base updates.
func computeSyncPlan(s *stack.Stack) []syncAction {
	var actions []syncAction

	// Work on a copy of nodes to simulate removals
	nodes := make([]stack.Node, len(s.Nodes))
	copy(nodes, s.Nodes)

	rebased := make(map[string]bool)

	for i := 0; i < len(nodes); i++ {
		node := nodes[i]

		if node.Status == "merged" {
			actions = append(actions, syncAction{
				kind:   "remove-merged",
				branch: node.Branch,
			})

			if i+1 < len(nodes) {
				next := nodes[i+1]
				newBase := s.Base

				actions = append(actions, syncAction{
					kind:   "rebase",
					branch: next.Branch,
					onto:   newBase,
				})
				rebased[next.Branch] = true

				actions = append(actions, syncAction{
					kind:   "push",
					branch: next.Branch,
				})

				if next.PR > 0 && ghpkg.Available() {
					actions = append(actions, syncAction{
						kind:   "update-pr-base",
						branch: next.Branch,
						pr:     next.PR,
						onto:   newBase,
					})
				}

				// Update simulated BaseTip so the stale-tip check
				// doesn't produce a duplicate rebase for this node.
				if tip, err := gitpkg.RevParse(newBase); err == nil {
					nodes[i+1].BaseTip = tip
				}
			}

			// Simulate removal
			nodes = append(nodes[:i], nodes[i+1:]...)
			i--
			continue
		}

		// Check for stale base tip
		parent := s.Base
		if i > 0 {
			parent = nodes[i-1].Branch
		}

		needsRebase := rebased[parent]
		if !needsRebase {
			currentParentTip, err := gitpkg.RevParse(parent)
			if err == nil && node.BaseTip != "" && currentParentTip != node.BaseTip {
				needsRebase = true
			}
		}

		if needsRebase {
			actions = append(actions, syncAction{
				kind:   "rebase",
				branch: node.Branch,
				onto:   parent,
			})
			rebased[node.Branch] = true

			actions = append(actions, syncAction{
				kind:   "push",
				branch: node.Branch,
			})

			if node.PR > 0 && ghpkg.Available() {
				actions = append(actions, syncAction{
					kind:   "update-pr-base",
					branch: node.Branch,
					pr:     node.PR,
					onto:   parent,
				})
			}
		}
	}

	return actions
}

// printSyncPlan displays the planned sync actions to the user.
func printSyncPlan(plan []syncAction) {
	fmt.Println("\nSync plan:")
	for _, a := range plan {
		switch a.kind {
		case "remove-merged":
			fmt.Printf("  ✓ %s is merged (remove from stack)\n", a.branch)
		case "rebase":
			fmt.Printf("  → rebase %s onto %s\n", a.branch, a.onto)
		case "push":
			fmt.Printf("  → force-push %s\n", a.branch)
		case "update-pr-base":
			fmt.Printf("  → update PR #%d base to %s\n", a.pr, a.onto)
		}
	}
	fmt.Println()
}

// confirmSync prompts the user to confirm the sync plan.
// Returns true if the user confirms (Enter, y, yes), false otherwise.
func confirmSync() bool {
	fmt.Printf("Proceed? [Y/n] ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "" || answer == "y" || answer == "yes"
}

func handleConflict(root string, s *stack.Stack, branch string, rebaseErr error) error {
	conflicted, err := gitpkg.ConflictedFiles()
	if err != nil || len(conflicted) == 0 {
		// Not a conflict, just a regular error
		gitpkg.RebaseAbort()
		return rebaseErr
	}

	fmt.Printf("  ⚠ Conflict in %s — %d file(s)\n", branch, len(conflicted))

	// Try Claude resolution if available
	if claudepkg.Available() {
		fmt.Println("  → invoking Claude for conflict resolution...")

		stackCtx, _ := ctxpkg.Assemble(root, s, branch)
		parent := s.ParentBranch(branch)
		upstreamSummary, _ := gitpkg.DiffSummary(s.FindNode(branch).BaseTip, parent)

		branchCtx, _ := ctxpkg.Read(root, branch)

		conflictContents := make(map[string]string)
		for _, f := range conflicted {
			data, err := os.ReadFile(f)
			if err == nil {
				conflictContents[f] = string(data)
			}
		}

		prompt := ctxpkg.BuildConflictPrompt(stackCtx, upstreamSummary, branchCtx, conflictContents)
		sessionName := claudepkg.SanitizeSessionName("conflict", branch)

		output, err := claudepkg.RunPrompt(sessionName, prompt)
		if err == nil {
			if err := applyResolutions(output, conflicted); err == nil {
				if err := gitpkg.Add("."); err == nil {
					if err := gitpkg.RebaseContinue(); err == nil {
						fmt.Println("  ✓ Conflicts resolved by Claude")
						return nil
					}
				}
			}
		}
		fmt.Fprintf(os.Stderr, "  Claude resolution failed, falling back to manual resolution\n")
	}

	// Fall back: abort rebase and tell user
	gitpkg.RebaseAbort()
	return fmt.Errorf("conflicts in %s — resolve manually and run `sdf sync` again:\n  %s",
		branch, strings.Join(conflicted, "\n  "))
}

// applyResolutions parses Claude's output for fenced code blocks and writes
// the resolved content to the conflicted files.
func applyResolutions(output string, conflicted []string) error {
	// Parse fenced code blocks: ```<lang> <filename>
	lines := strings.Split(output, "\n")
	var currentFile string
	var content strings.Builder
	inBlock := false

	resolved := make(map[string]string)

	for _, line := range lines {
		if strings.HasPrefix(line, "```") && !inBlock {
			// Opening fence — extract filename
			parts := strings.Fields(strings.TrimPrefix(line, "```"))
			if len(parts) >= 2 {
				currentFile = parts[len(parts)-1]
				content.Reset()
				inBlock = true
			} else if len(parts) == 1 {
				// Could be just the filename
				currentFile = parts[0]
				content.Reset()
				inBlock = true
			}
			continue
		}
		if line == "```" && inBlock {
			if currentFile != "" {
				resolved[currentFile] = content.String()
			}
			inBlock = false
			currentFile = ""
			continue
		}
		if inBlock {
			if content.Len() > 0 {
				content.WriteString("\n")
			}
			content.WriteString(line)
		}
	}

	if len(resolved) == 0 {
		return fmt.Errorf("no resolved files found in Claude output")
	}

	for _, f := range conflicted {
		resolvedContent, ok := resolved[f]
		if !ok {
			// Try matching by basename
			for key, val := range resolved {
				if strings.HasSuffix(f, key) || strings.HasSuffix(key, f) {
					resolvedContent = val
					ok = true
					break
				}
			}
		}
		if !ok {
			return fmt.Errorf("no resolution found for %s", f)
		}
		if err := os.WriteFile(f, []byte(resolvedContent+"\n"), 0644); err != nil {
			return fmt.Errorf("cannot write resolved file %s: %w", f, err)
		}
	}

	return nil
}
