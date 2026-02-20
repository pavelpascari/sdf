package cmd

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
)

// syncAction represents a single planned operation during sync.
type syncAction struct {
	kind   string // "skip-merged", "rebase", "push", "update-pr-base", "update-content"
	branch string
	onto   string // target base for rebase or PR base update
	pr     int    // PR number (for update-pr-base, update-content)
}

// syncOptions controls PR update behavior during sync.
type syncOptions struct {
	withContent bool
	cfg         cfgpkg.Config
}

func RunSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	yes := fs.Bool("y", false, "skip confirmation prompt")
	cont := fs.Bool("continue", false, "resume a paused sync after manual conflict resolution")
	stackFlag := fs.String("stack", "", "stack to sync (default: auto-detect)")
	withContent := fs.Bool("with-content", false, "update PR titles and descriptions")
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

	return runSyncFull(root, stackName, *yes, *withContent)
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
		return runSyncFull(root, "", false, false)
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
func runSyncFull(root, stackName string, skipConfirm, flagWithContent bool) error {
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
		withContent: cfg.WithContentEnabled() || flagWithContent,
		cfg:         cfg,
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
// Titles and descriptions are generated by Claude CLI when available,
// with local fallback for titles. Similarity checks prevent redundant updates.
func updatePRContent(_ string, s *stack.Stack, opts *syncOptions) {
	if opts == nil || !opts.withContent {
		return
	}
	if !ghpkg.Available() {
		return
	}

	// Collect eligible PRs
	type contentJob struct {
		node         stack.Node
		prefix       string // conventional commit prefix (e.g. "feat: ")
		localTitle   string // fallback title from branch name
		titlePrompt  string
		descPrompt   string
		titleSession string
		descSession  string
		index        int
	}

	hasClaude := claudepkg.Available()

	var jobs []contentJob
	for i := range s.Nodes {
		node := &s.Nodes[i]
		if node.PR == 0 || node.Status == "merged" {
			continue
		}
		parent := s.ParentBranch(node.Branch)
		subjects, _ := gitpkg.LogSubjects(parent, node.Branch)
		if len(subjects) == 0 {
			continue
		}

		localTitle := cfgpkg.GeneratePRTitle(opts.cfg, s.StackID, node.Branch, subjects)
		j := contentJob{
			node:       *node,
			localTitle: localTitle,
			index:      len(jobs),
		}

		if hasClaude {
			diff, _ := gitpkg.DiffFull(parent, node.Branch)
			currentTitle, _ := ghpkg.PRViewTitle(node.PR)
			currentBody, _ := ghpkg.PRViewBody(node.PR)
			currentDesc := extractDescription(currentBody)

			j.prefix = cfgpkg.TitlePrefix(opts.cfg, node.Branch, subjects)
			j.titlePrompt = buildTitlePrompt(node.Branch, subjects, diff, currentTitle)
			j.titleSession = claudepkg.SanitizeSessionName("pr-title", node.Branch)
			j.descPrompt = buildDescriptionPrompt(node.Branch, subjects, diff, currentDesc)
			j.descSession = claudepkg.SanitizeSessionName("pr-desc", node.Branch)
		}

		jobs = append(jobs, j)
	}

	if len(jobs) == 0 {
		return
	}

	updated := 0

	// Print all anchor + status lines upfront (title + description per PR = 4 lines each)
	// We use two separate parallel displays: one for titles, one for descriptions
	fmt.Println()

	// --- Titles ---
	titleDisp := &parallelDisplay{w: os.Stdout, count: len(jobs)}
	for _, j := range jobs {
		fmt.Printf("  Updating title for PR #%d\n", j.node.PR)
		if hasClaude {
			fmt.Printf("    waiting for Claude...\n")
		} else {
			fmt.Printf("    %s\n", j.localTitle)
		}
	}

	type titleResult struct {
		title   string
		updated bool
		err     error
	}
	titleResults := make([]titleResult, len(jobs))
	var wg sync.WaitGroup

	for _, j := range jobs {
		wg.Add(1)
		go func(j contentJob) {
			defer wg.Done()

			proposedTitle := j.localTitle
			if j.titlePrompt != "" {
				sw := titleDisp.writerFor(j.index)
				aiDesc, err := claudepkg.RunPromptStreaming(j.titleSession, j.titlePrompt, sw)
				if err != nil {
					titleDisp.setStatus(j.index, fmt.Sprintf("⚠ Claude failed, using: %s", j.localTitle))
				} else {
					aiDesc = strings.Split(aiDesc, "\n")[0]
					aiDesc = strings.Trim(aiDesc, "\"' ")
					proposedTitle = j.prefix + aiDesc
				}
			}

			currentTitle, err := ghpkg.PRViewTitle(j.node.PR)
			if err != nil {
				titleDisp.setStatus(j.index, fmt.Sprintf("✗ could not read title: %v", err))
				titleResults[j.index] = titleResult{err: err}
				return
			}

			if similar(currentTitle, proposedTitle, 0.8) {
				titleDisp.setStatus(j.index, fmt.Sprintf("✓ %s", currentTitle))
				titleResults[j.index] = titleResult{title: currentTitle}
				return
			}

			if err := ghpkg.PREditTitle(j.node.PR, proposedTitle); err != nil {
				titleDisp.setStatus(j.index, fmt.Sprintf("✗ could not update: %v", err))
				titleResults[j.index] = titleResult{err: err}
			} else {
				titleDisp.setStatus(j.index, fmt.Sprintf("✓ %s", proposedTitle))
				titleResults[j.index] = titleResult{title: proposedTitle, updated: true}
			}
		}(j)
	}
	wg.Wait()

	for _, r := range titleResults {
		if r.updated {
			updated++
		}
	}

	// --- Descriptions ---
	if hasClaude {
		descDisp := &parallelDisplay{w: os.Stdout, count: len(jobs)}
		for _, j := range jobs {
			fmt.Printf("  Updating description for PR #%d\n", j.node.PR)
			fmt.Printf("    waiting for Claude...\n")
		}

		type descResult struct {
			description string
			updated     bool
			err         error
		}
		descResults := make([]descResult, len(jobs))

		for _, j := range jobs {
			wg.Add(1)
			go func(j contentJob) {
				defer wg.Done()
				sw := descDisp.writerFor(j.index)
				description, err := claudepkg.RunPromptStreaming(j.descSession, j.descPrompt, sw)
				if err != nil {
					descDisp.setStatus(j.index, fmt.Sprintf("✗ failed: %v", err))
					descResults[j.index] = descResult{err: err}
					return
				}

				currentBody, err := ghpkg.PRViewBody(j.node.PR)
				if err != nil {
					descDisp.setStatus(j.index, fmt.Sprintf("✗ could not read body: %v", err))
					descResults[j.index] = descResult{err: err}
					return
				}

				currentDesc := extractDescription(currentBody)
				if similar(currentDesc, description, 0.85) {
					descDisp.setStatus(j.index, "✓ unchanged")
					return
				}

				newBody := replaceDescription(currentBody, description)
				if err := ghpkg.PREditBody(j.node.PR, newBody); err != nil {
					descDisp.setStatus(j.index, fmt.Sprintf("✗ could not update: %v", err))
					descResults[j.index] = descResult{err: err}
				} else {
					descDisp.setStatus(j.index, "✓ done")
					descResults[j.index] = descResult{description: description, updated: true}
				}
			}(j)
		}
		wg.Wait()

		for _, r := range descResults {
			if r.updated {
				updated++
			}
		}
	}

	if updated > 0 {
		fmt.Printf("\n  Updated %d PR(s).\n", updated)
	}
}

// similar returns true if two strings are similar enough to skip updating.
// Uses Jaccard similarity on normalized word sets.
func similar(a, b string, threshold float64) bool {
	if a == b {
		return true
	}
	wordsA := normalizeWords(a)
	wordsB := normalizeWords(b)
	if len(wordsA) == 0 && len(wordsB) == 0 {
		return true
	}
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return false
	}

	setA := make(map[string]bool, len(wordsA))
	for _, w := range wordsA {
		setA[w] = true
	}
	setB := make(map[string]bool, len(wordsB))
	for _, w := range wordsB {
		setB[w] = true
	}

	intersection := 0
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}

	union := len(setA)
	for w := range setB {
		if !setA[w] {
			union++
		}
	}

	return float64(intersection)/float64(union) >= threshold
}

// normalizeWords lowercases and splits text into words, stripping punctuation.
func normalizeWords(s string) []string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == ' ' {
			return r
		}
		return ' '
	}, s)
	return strings.Fields(s)
}

// extractDescription returns the text between sdf:description markers,
// or empty string if no markers found.
func extractDescription(body string) string {
	start := strings.Index(body, descOpen)
	end := strings.Index(body, descClose)
	if start < 0 || end < 0 || end <= start {
		return ""
	}
	return strings.TrimSpace(body[start+len(descOpen) : end])
}

// buildTitlePrompt creates a prompt for Claude to generate the descriptive part
// of a PR title. The prefix (e.g. "feat: ") is added by the caller.
// If currentTitle is non-empty, Claude is asked to keep it if already good.
func buildTitlePrompt(branch string, subjects []string, diff, currentTitle string) string {
	var b strings.Builder

	b.WriteString("You are a PR title generator. Output ONLY a short title — nothing else.\n")
	b.WriteString("No prefix like 'feat:' or 'fix:' — just the descriptive part.\n")
	b.WriteString("No preamble, no quotes, no punctuation at the end.\n")
	b.WriteString("Keep it under 60 characters. Use lowercase.\n\n")

	if currentTitle != "" {
		fmt.Fprintf(&b, "Current title: %s\n", currentTitle)
		b.WriteString("If this title already accurately describes the change, output it unchanged.\n")
		b.WriteString("Only generate a new title if the current one is inaccurate or unclear.\n\n")
	}

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

	b.WriteString("\nWrite a concise title summarizing this change.")

	return b.String()
}

// buildDescriptionPrompt creates a prompt for Claude to generate a PR description
// from the branch's commits and diff.
// If currentDesc is non-empty, Claude is asked to keep it if already good.
func buildDescriptionPrompt(branch string, subjects []string, diff, currentDesc string) string {
	var b strings.Builder

	b.WriteString("You are a PR description generator. Output ONLY the description text — nothing else.\n")
	b.WriteString("No preamble, no thinking, no commentary, no markdown headers, no formatting.\n")
	b.WriteString("Start directly with the first sentence of the description.\n\n")

	if currentDesc != "" {
		b.WriteString("Current description:\n")
		b.WriteString(currentDesc)
		b.WriteString("\n\n")
		b.WriteString("If this description already accurately describes the change, output it unchanged.\n")
		b.WriteString("Only generate a new description if the current one is inaccurate, incomplete, or unclear.\n\n")
	}

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

// parallelDisplay manages a block of two-line entries (anchor + status)
// printed to the terminal. Multiple goroutines can update their own status
// lines concurrently using ANSI cursor movement.
//
// Layout (for 3 PRs, 6 lines total):
//
//	line 0:  Updating description for PR #21   (anchor, index 0)
//	line 1:    waiting for Claude...            (status, index 0)
//	line 2:  Updating description for PR #22   (anchor, index 1)
//	line 3:    waiting for Claude...            (status, index 1)
//	line 4:  Updating description for PR #23   (anchor, index 2)
//	line 5:    waiting for Claude...            (status, index 2)
//	cursor parks here (line 6, after all newlines)
type parallelDisplay struct {
	mu    sync.Mutex
	w     io.Writer
	count int // number of PR entries
}

// setStatus updates the status line for the entry at index.
func (d *parallelDisplay) setStatus(index int, msg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// Cursor is parked at the bottom (line 2*count).
	// Status line for index is at line 2*index+1.
	// Move up: 2*count - (2*index+1) = 2*(count-index) - 1
	up := 2*(d.count-index) - 1
	fmt.Fprintf(d.w, "\033[%dA\r\033[K    %s\033[%dB\r", up, msg, up)
}

// writerFor returns an io.Writer that streams tokens into the status line
// for the given index, showing a truncated live preview.
func (d *parallelDisplay) writerFor(index int) io.Writer {
	return &parallelStatusWriter{disp: d, index: index}
}

type parallelStatusWriter struct {
	disp  *parallelDisplay
	index int
	buf   []byte
}

func (pw *parallelStatusWriter) Write(p []byte) (int, error) {
	pw.buf = append(pw.buf, p...)
	text := strings.ReplaceAll(string(pw.buf), "\n", " ")
	text = strings.Join(strings.Fields(text), " ")
	const maxLen = 68
	if len(text) > maxLen {
		text = "..." + text[len(text)-maxLen+3:]
	}
	pw.disp.setStatus(pw.index, text)
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

	// Append PR content update action for open PRs
	if opts != nil && opts.withContent {
		for _, node := range nodes {
			if node.PR == 0 || node.Status == "merged" {
				continue
			}
			actions = append(actions, syncAction{
				kind:   "update-content",
				branch: node.Branch,
				pr:     node.PR,
			})
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
		case "update-content":
			fmt.Printf("  → update PR #%d title + description\n", a.pr)
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
