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
	cont := fs.Bool("continue", false, "resume a paused sync after manual conflict resolution")
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

	if *cont {
		return runSyncContinue(root)
	}

	return runSyncFull(root, stackName, *yes)
}

// runSyncContinue resumes a sync that was paused for manual conflict resolution.
func runSyncContinue(root string) error {
	local, err := stack.LoadLocal(root)
	if err != nil {
		return err
	}

	if local.SyncProgress == nil {
		return fmt.Errorf("no paused sync to continue — run `sdf sync` to start a new sync")
	}
	progress := local.SyncProgress

	if gitpkg.IsRebaseInProgress() {
		fmt.Printf("Continuing rebase of %s...\n", progress.PausedAt)
		if err := gitpkg.RebaseContinue(); err != nil {
			return fmt.Errorf("rebase --continue failed: %w\n\nResolve remaining conflicts, stage them, and run `sdf sync --continue` again", err)
		}
		fmt.Printf("  ✓ %s rebased successfully\n", progress.PausedAt)
	} else if gitpkg.IsAncestor(progress.ParentTip, progress.PausedAt) {
		// No rebase in progress but the parent tip is an ancestor of the
		// paused branch — the user completed the rebase manually.
		fmt.Printf("  ✓ %s was rebased (completed outside sdf)\n", progress.PausedAt)
	} else {
		// The parent tip is NOT an ancestor — the rebase was aborted.
		fmt.Printf("Rebase of %s was aborted. Starting a fresh sync.\n", progress.PausedAt)
		local.SyncProgress = nil
		stack.SaveLocal(root, local)
		return runSyncFull(root, "", false)
	}

	s, err := stack.Load(root)
	if err != nil {
		return err
	}

	node := s.FindNode(progress.PausedAt)
	if node != nil {
		node.BaseTip = progress.ParentTip

		fmt.Printf("  → pushing %s...\n", node.Branch)
		if err := gitpkg.Push(node.Branch); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: push failed for %s: %v\n", node.Branch, err)
		}

		parent := s.ParentBranch(node.Branch)
		if node.PR > 0 && ghpkg.Available() {
			fmt.Printf("  → updating PR #%d base to %s\n", node.PR, parent)
			if err := ghpkg.PREditBase(node.PR, parent); err != nil {
				fmt.Fprintf(os.Stderr, "  warning: could not update PR base: %v\n", err)
			}
		}
	}

	if err := stack.Save(root, s); err != nil {
		return fmt.Errorf("cannot save stack: %w", err)
	}

	local.SyncProgress = nil
	stack.SaveLocal(root, local)

	gitpkg.Checkout(progress.OriginalBranch)

	fmt.Println("\nResuming sync for remaining branches...")
	return runSyncFrom(root, s, progress.ResumeIndex+1)
}

// runSyncFull runs a complete sync from scratch.
func runSyncFull(root, stackName string, skipConfirm bool) error {
	// Check for stale sync progress
	local, _ := stack.LoadLocal(root)
	if local.SyncProgress != nil {
		if gitpkg.IsRebaseInProgress() {
			return fmt.Errorf("a rebase is in progress from a previous sync pause on %s\n\n"+
				"  To continue:  resolve conflicts, stage, then run `sdf sync --continue`\n"+
				"  To abort:     run `git rebase --abort`, then `sdf sync`",
				local.SyncProgress.PausedAt)
		}
		local.SyncProgress = nil
		stack.SaveLocal(root, local)
	}

	s, err := resolveStack(root, stackName)
	if err != nil {
		return err
	}

	if len(s.Nodes) == 0 {
		fmt.Printf("No branches in stack %q. Nothing to sync.\n", s.StackID)
		return nil
	}

	clean, err := gitpkg.IsClean()
	if err != nil {
		return fmt.Errorf("cannot check working tree status: %w", err)
	}
	if !clean {
		return fmt.Errorf("working tree is not clean — commit or stash changes before syncing")
	}

	fmt.Printf("Syncing stack %q...\n", s.StackID)
	fmt.Println("Fetching from origin...")
	if err := gitpkg.FetchAll(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: fetch failed: %v\n", err)
	}

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

	// Compute and show the sync plan
	plan := computeSyncPlan(s)
	if len(plan) == 0 {
		fmt.Println("\nEverything is in sync.")
		return nil
	}

	printSyncPlan(plan)

	if !skipConfirm {
		if !confirmSync() {
			fmt.Println("Aborted.")
			return nil
		}
	}

	fmt.Println()

	return runSyncFrom(root, s, 0)
}

// runSyncFrom runs sync starting at a given node index. Index 0 means full sync.
func runSyncFrom(root string, s *stack.Stack, startIndex int) error {
	originalBranch, err := gitpkg.CurrentBranch()
	if err != nil {
		return fmt.Errorf("cannot determine current branch: %w", err)
	}

	modified := false
	failed := make(map[string]error)

	for i := 0; i < len(s.Nodes); i++ {
		node := &s.Nodes[i]

		if node.Status == "merged" {
			fmt.Printf("  ✓ %s is merged\n", node.Branch)
			modified = true
			continue
		}

		if i < startIndex {
			continue
		}

		if isBlocked(s, i, failed) {
			fmt.Printf("  ⊘ skipping %s — depends on a branch that failed to rebase\n", node.Branch)
			continue
		}

		parent := s.ParentBranch(node.Branch)

		currentParentTip, err := gitpkg.RevParse(parent)
		if err != nil {
			continue
		}

		if node.BaseTip != "" && currentParentTip != node.BaseTip {
			fmt.Printf("  → rebasing %s onto %s...\n", node.Branch, parent)

			if err := gitpkg.RebaseOnto(parent, node.BaseTip, node.Branch); err != nil {
				action, err := promptOnConflict(root, s, node.Branch, originalBranch, i, err)
				if action == conflictPaused {
					return err
				}
				if action == conflictAborted {
					gitpkg.Checkout(originalBranch)
					return err
				}
				if action == conflictFailed {
					failed[node.Branch] = err
					continue
				}
			}

			node.BaseTip = currentParentTip
			modified = true

			fmt.Printf("  → pushing %s...\n", node.Branch)
			if err := gitpkg.Push(node.Branch); err != nil {
				fmt.Fprintf(os.Stderr, "  warning: push failed for %s: %v\n", node.Branch, err)
			}

			if node.PR > 0 && ghpkg.Available() {
				fmt.Printf("  → updating PR #%d base to %s\n", node.PR, parent)
				if err := ghpkg.PREditBase(node.PR, parent); err != nil {
					fmt.Fprintf(os.Stderr, "  warning: could not update PR base: %v\n", err)
				}
			}
		}
	}

	if modified {
		if err := stack.Save(root, s); err != nil {
			return fmt.Errorf("cannot save stack: %w", err)
		}
	}

	gitpkg.Checkout(originalBranch)

	if len(failed) > 0 {
		fmt.Printf("\nSync partially complete. %d branch(es) failed:\n", len(failed))
		for branch, err := range failed {
			fmt.Printf("  ✗ %s: %v\n", branch, err)
		}
		fmt.Println("\nRun `sdf sync` again to retry.")
		return fmt.Errorf("%d branch(es) could not be synced", len(failed))
	}

	if modified {
		fmt.Println("\nSync complete. Stack updated.")
	} else {
		fmt.Println("\nEverything is in sync.")
	}

	return nil
}

// computeSyncPlan determines what operations sync will perform without
// executing them.
func computeSyncPlan(s *stack.Stack) []syncAction {
	var actions []syncAction

	nodes := make([]stack.Node, len(s.Nodes))
	copy(nodes, s.Nodes)

	rebased := make(map[string]bool)

	for i := 0; i < len(nodes); i++ {
		node := nodes[i]

		if node.Status == "merged" {
			actions = append(actions, syncAction{kind: "skip-merged", branch: node.Branch})
			continue
		}

		parent := s.ParentBranch(node.Branch)

		needsRebase := rebased[parent]
		if !needsRebase {
			currentParentTip, err := gitpkg.RevParse(parent)
			if err == nil && node.BaseTip != "" && currentParentTip != node.BaseTip {
				needsRebase = true
			}
		}

		if needsRebase {
			actions = append(actions, syncAction{kind: "rebase", branch: node.Branch, onto: parent})
			rebased[node.Branch] = true
			actions = append(actions, syncAction{kind: "push", branch: node.Branch})

			if node.PR > 0 && ghpkg.Available() {
				actions = append(actions, syncAction{kind: "update-pr-base", branch: node.Branch, pr: node.PR, onto: parent})
			}
		}
	}

	return actions
}

func printSyncPlan(plan []syncAction) {
	fmt.Println("\nSync plan:")
	for _, a := range plan {
		switch a.kind {
		case "skip-merged":
			fmt.Printf("  ✓ %s is merged\n", a.branch)
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

func confirmSync() bool {
	fmt.Printf("Proceed? [Y/n] ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "" || answer == "y" || answer == "yes"
}

// conflictAction represents the developer's choice when conflicts occur.
type conflictAction int

const (
	conflictResolved conflictAction = iota
	conflictFailed
	conflictPaused
	conflictAborted
)

func promptOnConflict(root string, s *stack.Stack, branch, originalBranch string, nodeIndex int, rebaseErr error) (conflictAction, error) {
	conflicted, err := gitpkg.ConflictedFiles()
	if err != nil || len(conflicted) == 0 {
		gitpkg.RebaseAbort()
		return conflictFailed, rebaseErr
	}

	fmt.Printf("\n  ⚠ Conflict in %s — %d file(s):\n", branch, len(conflicted))
	for _, f := range conflicted {
		fmt.Printf("    %s\n", f)
	}
	fmt.Println()

	hasClaude := claudepkg.Available()
	if hasClaude {
		fmt.Println("  [c] Ask Claude to resolve")
	}
	fmt.Println("  [m] I'll fix it myself (pauses sync, resume with `sdf sync --continue`)")
	fmt.Println("  [s] Skip this branch, continue syncing the rest")
	fmt.Println("  [a] Abort sync entirely")
	fmt.Println()

	choice := prompt("  > ")

	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "c":
		if !hasClaude {
			fmt.Println("  Claude is not available. Pick another option.")
			return promptOnConflict(root, s, branch, originalBranch, nodeIndex, rebaseErr)
		}
		return tryClaude(root, s, branch, originalBranch, nodeIndex, rebaseErr, conflicted)
	case "m":
		return pauseForManualResolution(root, s, branch, originalBranch, nodeIndex)
	case "s":
		gitpkg.RebaseAbort()
		fmt.Printf("  Skipped %s.\n", branch)
		return conflictFailed, fmt.Errorf("conflicts in %s (skipped by user)", branch)
	case "a":
		gitpkg.RebaseAbort()
		return conflictAborted, fmt.Errorf("sync aborted by user at %s", branch)
	default:
		fmt.Println("  Please pick one of the options above.")
		return promptOnConflict(root, s, branch, originalBranch, nodeIndex, rebaseErr)
	}
}

func tryClaude(root string, s *stack.Stack, branch, originalBranch string, nodeIndex int, rebaseErr error, conflicted []string) (conflictAction, error) {
	fmt.Println("  → Invoking Claude for conflict resolution...")

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

	p := ctxpkg.BuildConflictPrompt(stackCtx, upstreamSummary, branchCtx, conflictContents)
	sessionName := claudepkg.SanitizeSessionName("conflict", branch)

	output, err := claudepkg.RunPrompt(sessionName, p)
	if err == nil {
		if err := applyResolutions(output, conflicted); err == nil {
			if err := gitpkg.Add("."); err == nil {
				if err := gitpkg.RebaseContinue(); err == nil {
					fmt.Println("  ✓ Conflicts resolved by Claude")
					return conflictResolved, nil
				}
			}
		}
	}

	fmt.Println("  Claude couldn't fully resolve the conflicts.")
	fmt.Println()
	fmt.Println("  [m] I'll fix the rest myself (pauses sync)")
	fmt.Println("  [s] Skip this branch, continue with the rest")
	fmt.Println("  [a] Abort sync entirely")
	fmt.Println()

	choice := prompt("  > ")
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "m":
		return pauseForManualResolution(root, s, branch, originalBranch, nodeIndex)
	case "s":
		gitpkg.RebaseAbort()
		return conflictFailed, fmt.Errorf("conflicts in %s (Claude failed, skipped)", branch)
	case "a":
		gitpkg.RebaseAbort()
		return conflictAborted, fmt.Errorf("sync aborted by user at %s", branch)
	default:
		fmt.Println("  Please pick one of the options above.")
		return tryClaude(root, s, branch, originalBranch, nodeIndex, rebaseErr, conflicted)
	}
}

func pauseForManualResolution(root string, s *stack.Stack, branch, originalBranch string, nodeIndex int) (conflictAction, error) {
	parent := s.ParentBranch(branch)
	parentTip, _ := gitpkg.RevParse(parent)

	stack.Save(root, s)

	local, _ := stack.LoadLocal(root)
	local.SyncProgress = &stack.SyncProgress{
		PausedAt:       branch,
		ResumeIndex:    nodeIndex,
		OriginalBranch: originalBranch,
		ParentTip:      parentTip,
	}
	stack.SaveLocal(root, local)

	fmt.Printf("\n  Sync paused. Resolve conflicts in %s, then:\n", branch)
	fmt.Println()
	fmt.Println("    1. Edit the conflicted files")
	fmt.Println("    2. git add <resolved files>")
	fmt.Println("    3. sdf sync --continue")
	fmt.Println()

	return conflictPaused, nil
}

// isBlocked returns true if the node at index i depends on a branch that failed.
func isBlocked(s *stack.Stack, i int, failed map[string]error) bool {
	for j := 0; j < i; j++ {
		if _, ok := failed[s.Nodes[j].Branch]; ok {
			return true
		}
	}
	return false
}

func prompt(msg string) string {
	fmt.Print(msg)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return line
}

// applyResolutions parses Claude's output for fenced code blocks and writes
// the resolved content to the conflicted files.
func applyResolutions(output string, conflicted []string) error {
	lines := strings.Split(output, "\n")
	var currentFile string
	var content strings.Builder
	inBlock := false

	resolved := make(map[string]string)

	for _, line := range lines {
		if strings.HasPrefix(line, "```") && !inBlock {
			parts := strings.Fields(strings.TrimPrefix(line, "```"))
			if len(parts) >= 2 {
				currentFile = parts[len(parts)-1]
				content.Reset()
				inBlock = true
			} else if len(parts) == 1 {
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
