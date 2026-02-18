package cmd

import (
	"fmt"
	"os"
	"strings"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	ctxpkg "github.com/pavelpascari/sdf/internal/context"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
)

func RunSync(args []string) error {
	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	s, err := stack.Load(root)
	if err != nil {
		return err
	}

	if len(s.Nodes) == 0 {
		fmt.Println("No branches in stack. Nothing to sync.")
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

	// Process merged nodes and cascade rebases
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

		// Commit the updated stack.json
		if err := gitpkg.Add(".sdf/stack.json"); err == nil {
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
