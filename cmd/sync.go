package cmd

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
)

// syncAction represents a single planned operation during sync.
type syncAction struct {
	kind   string // "skip-merged", "rebase", "push", "update-pr-base", "update-title", "update-description"
	branch string
	onto   string // target base for rebase or PR base update
	pr     int    // PR number (for update-pr-base, update-title, update-description)
	title  string // proposed new title (for update-title)
}

// syncOptions controls PR update behavior during sync.
type syncOptions struct {
	updateDescriptions bool
	updateTitles       bool
	cfg                cfgpkg.Config
}

func RunSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	yes := fs.Bool("y", false, "skip confirmation prompt")
	cont := fs.Bool("continue", false, "resume a paused sync after manual conflict resolution")
	stackFlag := fs.String("stack", "", "stack to sync (default: auto-detect)")
	updateDescs := fs.Bool("update-descriptions", false, "update PR descriptions via Claude")
	updateTitles := fs.Bool("update-titles", false, "update PR titles from branch/commits")
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

	return runSyncFull(root, stackName, *yes, *updateDescs, *updateTitles)
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
		return runSyncFull(root, "", false, false, false)
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
	return runSyncFrom(root, s, progress.ResumeIndex+1, nil)
}

// runSyncFull runs a complete sync from scratch.
func runSyncFull(root, stackName string, skipConfirm, flagUpdateDescs, flagUpdateTitles bool) error {
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

	// Fast-forward the base branch so RevParse returns the latest tip
	if err := gitpkg.FastForward(s.Base); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not fast-forward %s: %v\n", s.Base, err)
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

	// Load config for PR update settings
	cfg, err := cfgpkg.Load(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not load config: %v\n", err)
		cfg = cfgpkg.Defaults()
	}

	opts := syncOptions{
		updateDescriptions: cfg.UpdateDescriptionsEnabled() || flagUpdateDescs,
		updateTitles:       cfg.UpdateTitlesEnabled() || flagUpdateTitles,
		cfg:                cfg,
	}

	// Compute and show the sync plan
	plan := computeSyncPlan(s, &opts)
	if len(plan) == 0 {
		fmt.Println("\nEverything is in sync.")
		// Still update stack navigation (catches empty/stale nav hashes)
		if err := updateStackNavForAllPRs(root, s); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not update PR descriptions: %v\n", err)
		}
		return nil
	}

	printSyncPlan(plan)

	// Skip confirmation when all actions are just acknowledging merged PRs
	onlySkipMerged := true
	for _, a := range plan {
		if a.kind != "skip-merged" {
			onlySkipMerged = false
			break
		}
	}

	if !skipConfirm && !onlySkipMerged {
		if !confirmSync() {
			fmt.Println("Aborted.")
			return nil
		}
	}

	fmt.Println()

	return runSyncFrom(root, s, 0, &opts)
}

// runSyncFrom runs sync starting at a given node index. Index 0 means full sync.
// opts controls PR content updates (titles/descriptions); nil disables updates.
func runSyncFrom(root string, s *stack.Stack, startIndex int, opts *syncOptions) error {
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

			// Only update PR base when the parent branch name changed
			// (merged node skipped). Cascade rebases keep the same base.
			if node.PR > 0 && ghpkg.Available() {
				directParent := s.Base
				if i > 0 {
					directParent = s.Nodes[i-1].Branch
				}
				if parent != directParent {
					fmt.Printf("  → updating PR #%d base to %s\n", node.PR, parent)
					if err := ghpkg.PREditBase(node.PR, parent); err != nil {
						fmt.Fprintf(os.Stderr, "  warning: could not update PR base: %v\n", err)
					}
				}
			}
		}
	}

	if modified {
		if err := stack.Save(root, s); err != nil {
			return fmt.Errorf("cannot save stack: %w", err)
		}
	}

	// If the original branch was merged, switch to the first open branch
	// in the stack (or the base branch if all are merged).
	checkoutTarget := originalBranch
	if node := s.FindNode(originalBranch); node != nil && node.Status == "merged" {
		checkoutTarget = s.Base
		for _, n := range s.Nodes {
			if n.Status != "merged" {
				checkoutTarget = n.Branch
				break
			}
		}
	}
	gitpkg.Checkout(checkoutTarget)

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

	// Update PR titles and descriptions if configured
	updatePRContent(root, s, opts)

	// Update stack navigation in all PRs (runs even when in sync,
	// to catch stale nav hashes from PRs created before this feature)
	if err := updateStackNavForAllPRs(root, s); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not update PR descriptions: %v\n", err)
	}

	return nil
}

// updatePRContent updates PR titles and descriptions for open PRs in the stack.
// Titles are generated from branch names and commit messages.
// Descriptions are generated by Claude CLI (skipped silently if unavailable).
func updatePRContent(_ string, s *stack.Stack, opts *syncOptions) {
	if opts == nil {
		return
	}
	if !opts.updateTitles && !opts.updateDescriptions {
		return
	}
	if !ghpkg.Available() {
		return
	}

	updated := 0

	for i := range s.Nodes {
		node := &s.Nodes[i]
		if node.PR == 0 || node.Status == "merged" {
			continue
		}

		if opts.updateTitles {
			parent := s.ParentBranch(node.Branch)
			subjects, _ := gitpkg.LogSubjects(parent, node.Branch)
			proposedTitle := cfgpkg.GeneratePRTitle(opts.cfg, s.StackID, node.Branch, subjects)

			currentTitle, err := ghpkg.PRViewTitle(node.PR)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  warning: could not read PR #%d title: %v\n", node.PR, err)
				continue
			}

			if currentTitle != proposedTitle {
				fmt.Printf("  → PR #%d title: %q\n", node.PR, proposedTitle)
				if err := ghpkg.PREditTitle(node.PR, proposedTitle); err != nil {
					fmt.Fprintf(os.Stderr, "  warning: could not update PR #%d title: %v\n", node.PR, err)
				} else {
					updated++
				}
			}
		}

		if opts.updateDescriptions && claudepkg.Available() {
			parent := s.ParentBranch(node.Branch)
			subjects, _ := gitpkg.LogSubjects(parent, node.Branch)
			diff, _ := gitpkg.DiffFull(parent, node.Branch)

			if len(subjects) == 0 {
				continue
			}

			p := buildDescriptionPrompt(node.Branch, subjects, diff)
			sessionName := claudepkg.SanitizeSessionName("pr-desc", node.Branch)

			// Stream Claude output on a single updating line
			fmt.Printf("  ⋯ PR #%d: generating description", node.PR)
			sw := &streamWriter{w: os.Stdout}
			description, err := claudepkg.RunPromptStreaming(sessionName, p, sw)
			// Clear the streaming line and print the final status
			fmt.Printf("\r\033[K")
			if err != nil {
				fmt.Fprintf(os.Stderr, "  ✗ PR #%d: Claude could not generate description: %v\n", node.PR, err)
				continue
			}

			currentBody, err := ghpkg.PRViewBody(node.PR)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  ✗ PR #%d: could not read body: %v\n", node.PR, err)
				continue
			}

			newBody := replaceDescription(currentBody, description)
			if newBody != currentBody {
				if err := ghpkg.PREditBody(node.PR, newBody); err != nil {
					fmt.Fprintf(os.Stderr, "  ✗ PR #%d: could not update description: %v\n", node.PR, err)
				} else {
					fmt.Printf("  ✓ PR #%d: description updated\n", node.PR)
					updated++
				}
			} else {
				fmt.Printf("  ✓ PR #%d: description unchanged\n", node.PR)
			}
		}
	}

	if updated > 0 {
		fmt.Printf("  Updated %d PR(s).\n", updated)
	}
}

// buildDescriptionPrompt creates a prompt for Claude to generate a PR description
// from the branch's commits and diff.
func buildDescriptionPrompt(branch string, subjects []string, diff string) string {
	var b strings.Builder

	b.WriteString("You are a PR description generator. Output ONLY the description text — nothing else.\n")
	b.WriteString("No preamble, no thinking, no commentary, no markdown headers, no formatting.\n")
	b.WriteString("Start directly with the first sentence of the description.\n\n")
	fmt.Fprintf(&b, "Branch: %s\n\n", branch)
	b.WriteString("Commits:\n")
	for _, subj := range subjects {
		fmt.Fprintf(&b, "  - %s\n", subj)
	}

	if diff != "" {
		b.WriteString("\nDiff:\n")
		b.WriteString(diff)
		b.WriteString("\n")
	}

	b.WriteString("\nWrite 2-5 sentences explaining what this change does and why. Focus on user impact and key changes.")

	return b.String()
}

// streamWriter shows Claude streaming output as a single updating line.
// It collects tokens and periodically rewrites the current terminal line
// with a truncated preview of the generated text so far.
type streamWriter struct {
	w   io.Writer
	buf []byte
}

func (sw *streamWriter) Write(p []byte) (int, error) {
	sw.buf = append(sw.buf, p...)
	// Build a single-line preview: collapse whitespace, truncate
	text := strings.ReplaceAll(string(sw.buf), "\n", " ")
	text = strings.Join(strings.Fields(text), " ")
	const maxLen = 72
	if len(text) > maxLen {
		text = text[len(text)-maxLen:]
	}
	fmt.Fprintf(sw.w, "\r\033[K    %s", text)
	return len(p), nil
}

// computeSyncPlan determines what operations sync will perform without
// executing them.
func computeSyncPlan(s *stack.Stack, opts *syncOptions) []syncAction {
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

			// Only update the PR base when the parent branch name changed
			// (e.g., merged node was skipped). Cascade rebases don't change
			// the PR base — just the content.
			if node.PR > 0 && ghpkg.Available() {
				directParent := s.Base
				if i > 0 {
					directParent = nodes[i-1].Branch
				}
				if parent != directParent {
					actions = append(actions, syncAction{kind: "update-pr-base", branch: node.Branch, pr: node.PR, onto: parent})
				}
			}
		}
	}

	// Append PR content update actions for open PRs
	if opts != nil {
		for _, node := range nodes {
			if node.PR == 0 || node.Status == "merged" {
				continue
			}

			if opts.updateTitles {
				parent := s.ParentBranch(node.Branch)
				subjects, _ := gitpkg.LogSubjects(parent, node.Branch)
				proposedTitle := cfgpkg.GeneratePRTitle(opts.cfg, s.StackID, node.Branch, subjects)
				actions = append(actions, syncAction{
					kind:   "update-title",
					branch: node.Branch,
					pr:     node.PR,
					title:  proposedTitle,
				})
			}

			if opts.updateDescriptions && claudepkg.Available() {
				actions = append(actions, syncAction{
					kind:   "update-description",
					branch: node.Branch,
					pr:     node.PR,
				})
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
		case "update-title":
			fmt.Printf("  → update PR #%d title: %q\n", a.pr, a.title)
		case "update-description":
			fmt.Printf("  → update PR #%d description via Claude\n", a.pr)
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

	parent := s.ParentBranch(branch)
	upstreamSummary, _ := gitpkg.DiffSummary(s.FindNode(branch).BaseTip, parent)

	// Use PR description as context (replaces local context docs)
	var branchDesc string
	node := s.FindNode(branch)
	if node != nil && node.PR > 0 && ghpkg.Available() {
		branchDesc, _ = ghpkg.PRViewBody(node.PR)
	}

	conflictContents := make(map[string]string)
	for _, f := range conflicted {
		data, err := os.ReadFile(f)
		if err == nil {
			conflictContents[f] = string(data)
		}
	}

	p := buildConflictPrompt(upstreamSummary, branchDesc, conflictContents)
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

// buildConflictPrompt constructs the conflict resolution prompt for Claude,
// using upstream changes and the PR description as context.
func buildConflictPrompt(upstreamSummary, branchDescription string, conflictedFiles map[string]string) string {
	var b strings.Builder

	b.WriteString("You are resolving conflicts during a stack rebase.\n\n")

	if upstreamSummary != "" {
		b.WriteString("=== UPSTREAM CHANGE SUMMARY ===\n")
		b.WriteString(upstreamSummary)
		b.WriteString("\n\n")
	}

	if branchDescription != "" {
		b.WriteString("=== BRANCH PR DESCRIPTION ===\n")
		b.WriteString(branchDescription)
		b.WriteString("\n\n")
	}

	b.WriteString("=== CONFLICTS ===\n")
	for filename, content := range conflictedFiles {
		fmt.Fprintf(&b, "File: %s\n```\n%s\n```\n\n", filename, content)
	}

	b.WriteString("Resolve all conflicts. For each file output the complete resolved content in a\n")
	b.WriteString("fenced code block with the filename: ```<lang> <filename>\n")

	return b.String()
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
