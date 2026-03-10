package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
)

// syncOptions holds optional behavior flags for a sync run.
type syncOptions struct {
	withContent  bool
	jsonMode     bool
	cfg          cfgpkg.Config
	fromHead     bool   // rebase onto the latest base branch tip
	preFFBaseTip string // base branch SHA before fast-forward
}

// SyncResult is the structured output of sdf sync when --json is used.
type SyncResult struct {
	Stack     string         `json:"stack"`
	Base      string         `json:"base"`
	Branches  []BranchResult `json:"branches"`
	PRUpdates []PRUpdate     `json:"pr_updates,omitempty"`
	Warnings  []string       `json:"warnings,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// BranchResult describes what happened to a single branch during sync.
type BranchResult struct {
	Branch      string `json:"branch"`
	PR          int    `json:"pr,omitempty"`
	Action      string `json:"action"`
	Pushed      bool   `json:"pushed,omitempty"`
	BaseUpdated bool   `json:"base_updated,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// PRUpdate describes a PR field update during sync.
type PRUpdate struct {
	PR     int    `json:"pr"`
	Field  string `json:"field"`
	Status string `json:"status"`
}

// syncAction represents a single planned operation during sync.
type syncAction struct {
	kind   string // "skip-merged", "update-tip", "rebase", "push", "update-pr-base", "update-content"
	branch string
	onto   string // target base for rebase or PR base update
	pr     int    // PR number (for update-pr-base, update-content)
}

var syncCmd = &cobra.Command{
	Use:   "sync [stack-name]",
	Short: "Detect merged PRs and cascade-rebase downstream branches",
	Long: `Fetches from origin, queries GitHub for PR status, reconciles PR states,
cascade-rebases downstream branches, pushes, and updates PR navigation links.

By default, sync only rebases branches within the stack (e.g. after a PR is
merged or a parent branch is amended). To also rebase onto the latest base
branch tip (e.g. main), use --from-head.

When a rebase conflict occurs, an interactive menu offers Claude resolution,
manual resolution (pausing sync), skip, or abort.`,
	Example: `  sdf sync                          # sync within the stack only
  sdf sync --from-head              # also rebase onto latest base branch
  sdf sync my-feature               # sync a specific stack by name
  sdf sync -y                       # skip confirmation prompt
  sdf sync --continue               # resume after manual conflict resolution
  sdf sync --with-content           # also update PR titles and descriptions`,
	Annotations:       map[string]string{"category": "stack"},
	ValidArgsFunction: completeStackNames,
	RunE:              runSyncCmd,
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")
	syncCmd.Flags().Bool("continue", false, "resume after manual conflict resolution")
	syncCmd.Flags().String("stack", "", "stack to sync (default: auto-detect)")
	syncCmd.Flags().Bool("with-content", false, "update PR titles and descriptions")
	syncCmd.Flags().Bool("json", false, "output result as JSON")
	syncCmd.Flags().Bool("from-head", false, "also rebase onto the latest base branch")
	_ = syncCmd.RegisterFlagCompletionFunc("stack", completeStackNames)
}

func runSyncCmd(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	cont, _ := cmd.Flags().GetBool("continue")
	stackFlag, _ := cmd.Flags().GetString("stack")
	withContent, _ := cmd.Flags().GetBool("with-content")
	jsonFlag, _ := cmd.Flags().GetBool("json")
	fromHead, _ := cmd.Flags().GetBool("from-head")

	stackName := stackFlag
	if stackName == "" && len(args) > 0 {
		stackName = args[0]
	}

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	var renderer render.Renderer
	var jsonRenderer *render.JSONRenderer
	if jsonFlag {
		jsonRenderer = &render.JSONRenderer{}
		renderer = jsonRenderer
	}
	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{Renderer: renderer})

	var result *SyncResult
	if jsonFlag {
		result = &SyncResult{Branches: []BranchResult{}}
	}

	if cont {
		err = runSyncContinue(root, result, bus)
	} else {
		err = runSyncFull(root, stackName, yes || jsonFlag, withContent, jsonFlag, fromHead, result, bus)
	}

	if jsonFlag {
		_ = bus.Finish()
		if err != nil {
			result.Error = err.Error()
		}
		if jsonRenderer != nil {
			result.Warnings = append(result.Warnings, jsonRenderer.Warnings()...)
		}
		data, marshalErr := json.MarshalIndent(result, "", "  ")
		if marshalErr != nil {
			return fmt.Errorf("cannot marshal result: %w", marshalErr)
		}
		fmt.Println(string(data))
		return nil // error is in the JSON output
	}

	_ = bus.Finish()
	return err
}

// RunSync is a compatibility wrapper for callers that use the old interface.
func RunSync(args []string) error {
	rootCmd.SetArgs(append([]string{"sync"}, args...))
	return rootCmd.Execute()
}

// runSyncContinue resumes a sync that was paused for manual conflict resolution.
func runSyncContinue(root string, result *SyncResult, bus *render.Bus) error {
	local, err := stack.LoadLocal(root)
	if err != nil {
		return err
	}

	if local.SyncProgress == nil {
		return fmt.Errorf("no paused sync to continue — run `sdf sync` to start a new sync")
	}
	progress := local.SyncProgress

	switch {
	case gitpkg.IsRebaseInProgress():
		bus.Printf("  rebasing %s (continuing)...", ui.Branch(progress.PausedAt))
		if err := gitpkg.RebaseContinue(); err != nil {
			return fmt.Errorf("rebase --continue failed: %w\n\nResolve remaining conflicts, stage them, and run `sdf sync --continue` again", err)
		}
	case gitpkg.IsAncestor(progress.ParentTip, progress.PausedAt):
		// No rebase in progress but the parent tip is an ancestor of the
		// paused branch — the user completed the rebase manually.
		bus.Printf("  %s %s rebased (completed outside sdf)", ui.SymOK, ui.Branch(progress.PausedAt))
	default:
		// The parent tip is NOT an ancestor — the rebase was aborted.
		bus.Printf("Rebase of %s was aborted. Starting a fresh sync.", ui.Branch(progress.PausedAt))
		local.SyncProgress = nil
		stack.SaveLocal(root, local)
		return runSyncFull(root, "", false, false, false, false, result, bus)
	}

	s, err := stack.Load(root)
	if err != nil {
		return err
	}

	if result != nil {
		result.Stack = s.StackID
		result.Base = s.Base
	}

	node := s.FindNode(progress.PausedAt)
	if node != nil {
		node.BaseTip = progress.ParentTip

		pushOK := true
		if err := gitpkg.Push(node.Branch); err != nil {
			pushOK = false
			bus.Warnf("  %s push failed for %s: %v", ui.SymFail, ui.Branch(node.Branch), err)
		} else {
			bus.Printf("  %s %s rebased and pushed", ui.SymOK, ui.Branch(node.Branch))
		}

		parent := s.ParentBranch(node.Branch)
		baseUpdated := false
		if node.PR > 0 && ghpkg.Available() {
			if err := ghpkg.PREditBase(node.PR, parent); err != nil {
				bus.Warnf("  %s could not update PR %s base: %v", ui.SymWarn, ui.PR(node.PR), err)
			} else {
				baseUpdated = true
				bus.Printf("  %s PR %s base updated to %s", ui.SymOK, ui.PR(node.PR), ui.Branch(parent))
			}
		}

		if result != nil {
			result.Branches = append(result.Branches, BranchResult{
				Branch:      node.Branch,
				PR:          node.PR,
				Action:      "rebased",
				Pushed:      pushOK,
				BaseUpdated: baseUpdated,
			})
		}
	}

	if err := stack.Save(root, s); err != nil {
		return fmt.Errorf("cannot save stack: %w", err)
	}

	local.SyncProgress = nil
	stack.SaveLocal(root, local)

	gitpkg.Checkout(progress.OriginalBranch)

	bus.Print("\nResuming sync for remaining branches...")
	return runSyncFrom(root, s, progress.ResumeIndex+1, nil, result, bus)
}

// runSyncFull runs a complete sync from scratch.
func runSyncFull(root, stackName string, skipConfirm, flagWithContent, jsonMode, fromHead bool, result *SyncResult, bus *render.Bus) error {
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

	if result != nil {
		result.Stack = s.StackID
		result.Base = s.Base
	}

	if len(s.Nodes) == 0 {
		bus.Printf("No branches in stack %q. Nothing to sync.", s.StackID)
		return nil
	}

	clean, err := gitpkg.IsClean()
	if err != nil {
		return fmt.Errorf("cannot check working tree status: %w", err)
	}
	if !clean {
		return fmt.Errorf("working tree is not clean — commit or stash changes before syncing")
	}

	bus.Printf("Syncing stack %s...", ui.Bold.Render(s.StackID))
	bus.Print("Fetching from origin...")
	if err := gitpkg.FetchAll(); err != nil {
		bus.Warnf("warning: fetch failed: %v", err)
	}

	// Capture the base tip before fast-forwarding so we can distinguish
	// "main advanced from unrelated work" from "stack node merged into main".
	preFFBaseTip, _ := gitpkg.RevParse(s.Base)

	// Fast-forward the base branch so RevParse returns the latest tip
	if err := gitpkg.FastForward(s.Base); err != nil {
		bus.Warnf("warning: could not fast-forward %s: %v", s.Base, err)
	}

	if ghpkg.Available() {
		reconcileSyncPRStates(s, bus)
	}

	// Load config for PR update settings
	cfg, err := cfgpkg.Load(root)
	if err != nil {
		bus.Warnf("warning: could not load config: %v", err)
		cfg = cfgpkg.Defaults()
	}

	opts := syncOptions{
		withContent:  cfg.WithContentEnabled() || flagWithContent,
		jsonMode:     jsonMode,
		cfg:          cfg,
		fromHead:     fromHead,
		preFFBaseTip: preFFBaseTip,
	}

	// Compute and show the sync plan
	plan := computeSyncPlan(s, &opts)

	// Check if there's any real work beyond acknowledging merged PRs.
	onlySkipMerged := true
	for _, a := range plan {
		if a.kind != "skip-merged" {
			onlySkipMerged = false
			break
		}
	}

	if len(plan) == 0 || onlySkipMerged {
		bus.Print("\nEverything is in sync.")
		// Save any state changes from reconciliation
		if err := stack.Save(root, s); err != nil {
			bus.Warnf("warning: could not save stack: %v", err)
		}
		// Still update stack navigation (catches empty/stale nav hashes)
		if err := updateStackNavForAllPRs(root, s, result, bus); err != nil {
			bus.Warnf("warning: could not update PR descriptions: %v", err)
		}
		return nil
	}

	printSyncPlan(plan, bus)

	if !skipConfirm {
		bus.Pause()
		ok := confirmSync()
		bus.Resume()
		if !ok {
			bus.Print("Aborted.")
			return nil
		}
	}

	bus.Print("")

	return runSyncFrom(root, s, 0, &opts, result, bus)
}

// runSyncFrom runs sync starting at a given node index. Index 0 means full sync.
// opts controls PR content updates (titles/descriptions); nil disables updates.
func runSyncFrom(root string, s *stack.Stack, startIndex int, opts *syncOptions, result *SyncResult, bus *render.Bus) error {
	originalBranch, err := gitpkg.CurrentBranch()
	if err != nil {
		return fmt.Errorf("cannot determine current branch: %w", err)
	}

	modified := false
	failed := make(map[string]error)
	var prBasesUpdated int

	// Check if there's any real work (rebases) beyond merged PRs and
	// update-tip bookkeeping.
	hasRealWork := false
	for i := 0; i < len(s.Nodes); i++ {
		if s.Nodes[i].Status != "merged" {
			parent := s.ParentBranch(s.Nodes[i].Branch)
			tip, err := gitpkg.RevParse(parent)
			if err == nil && s.Nodes[i].BaseTip != "" {
				compareTip := tip
				if opts != nil && !opts.fromHead && parent == s.Base && opts.preFFBaseTip != "" {
					compareTip = opts.preFFBaseTip
				}
				if compareTip != s.Nodes[i].BaseTip {
					if !gitpkg.IsAncestor(compareTip, s.Nodes[i].Branch) {
						hasRealWork = true
						break
					}
				}
			}
		}
	}

	for i := 0; i < len(s.Nodes); i++ {
		node := &s.Nodes[i]

		if node.Status == "merged" {
			// Only print merged status when there's real sync work to show
			if hasRealWork {
				if node.PR > 0 {
					bus.Printf("  %s PR %s (%s) merged", ui.SymOK, ui.PR(node.PR), ui.Branch(node.Branch))
				} else {
					bus.Printf("  %s %s merged", ui.SymOK, ui.Branch(node.Branch))
				}
			}
			if result != nil {
				result.Branches = append(result.Branches, BranchResult{
					Branch: node.Branch, PR: node.PR, Action: "merged",
				})
			}
			continue
		}

		if i < startIndex {
			continue
		}

		if isBlocked(s, i, failed) {
			bus.Printf("  %s skipping %s — depends on a branch that failed", ui.SymFail, ui.Branch(node.Branch))
			if result != nil {
				result.Branches = append(result.Branches, BranchResult{
					Branch: node.Branch, PR: node.PR, Action: "blocked",
					Reason: "depends on a branch that failed",
				})
			}
			continue
		}

		parent := s.ParentBranch(node.Branch)

		currentParentTip, err := gitpkg.RevParse(parent)
		if err != nil {
			continue
		}

		compareTip := currentParentTip
		if opts != nil && !opts.fromHead && parent == s.Base && opts.preFFBaseTip != "" {
			compareTip = opts.preFFBaseTip
		}

		if node.BaseTip != "" && compareTip != node.BaseTip {
			if gitpkg.IsAncestor(compareTip, node.Branch) {
				node.BaseTip = currentParentTip
				modified = true

				baseUpdated := false
				if node.PR > 0 && ghpkg.Available() {
					directParent := s.Base
					if i > 0 {
						directParent = s.Nodes[i-1].Branch
					}
					if parent != directParent {
						if err := ghpkg.PREditBase(node.PR, parent); err != nil {
							bus.Warnf("  %s could not update PR %s base: %v", ui.SymWarn, ui.PR(node.PR), err)
						} else {
							prBasesUpdated++
							baseUpdated = true
						}
					}
				}

				if result != nil {
					result.Branches = append(result.Branches, BranchResult{
						Branch:      node.Branch,
						PR:          node.PR,
						Action:      "updated-tip",
						BaseUpdated: baseUpdated,
					})
				}
				continue
			}

			rebaseOldBase := node.BaseTip
			if !gitpkg.IsAncestor(rebaseOldBase, node.Branch) {
				if fallbackBase, err := gitpkg.MergeBase(parent, node.Branch); err == nil && fallbackBase != "" {
					rebaseOldBase = fallbackBase
				}
			}

			bus.Printf("  rebasing %s onto %s...", ui.Branch(node.Branch), ui.Branch(parent))

			if err := gitpkg.RebaseOnto(parent, rebaseOldBase, node.Branch); err != nil {
				// In JSON mode, abort immediately — no interactive prompts.
				if opts != nil && opts.jsonMode {
					if result != nil {
						result.Branches = append(result.Branches, BranchResult{
							Branch: node.Branch, PR: node.PR, Action: "failed",
							Reason: "conflict",
						})
					}
					gitpkg.RebaseAbort()
					return fmt.Errorf("conflict in %s — cannot resolve in --json mode", node.Branch)
				}

				action, err := promptOnConflict(root, s, node.Branch, originalBranch, i, err, bus)
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

			pushOK := true
			if err := gitpkg.Push(node.Branch); err != nil {
				pushOK = false
				bus.Warnf("  %s push failed for %s: %v", ui.SymFail, ui.Branch(node.Branch), err)
			} else {
				bus.Printf("  %s %s rebased and pushed", ui.SymOK, ui.Branch(node.Branch))
			}

			// Silently update PR base when the parent branch name changed
			// (merged node skipped). Report in batch at end.
			baseUpdated := false
			if node.PR > 0 && ghpkg.Available() {
				directParent := s.Base
				if i > 0 {
					directParent = s.Nodes[i-1].Branch
				}
				if parent != directParent {
					if err := ghpkg.PREditBase(node.PR, parent); err != nil {
						bus.Warnf("  %s could not update PR %s base: %v", ui.SymWarn, ui.PR(node.PR), err)
					} else {
						prBasesUpdated++
						baseUpdated = true
					}
				}
			}

			if result != nil {
				result.Branches = append(result.Branches, BranchResult{
					Branch:      node.Branch,
					PR:          node.PR,
					Action:      "rebased",
					Pushed:      pushOK,
					BaseUpdated: baseUpdated,
				})
			}
		}
	}

	if prBasesUpdated > 0 {
		bus.Printf("  %s %d PR base(s) updated on GitHub", ui.SymOK, prBasesUpdated)
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
		bus.Printf("\nSync partially complete. %d branch(es) failed:", len(failed))
		for branch, err := range failed {
			bus.Printf("  %s %s: %v", ui.SymFail, ui.Branch(branch), err)
		}
		bus.Print("\nRun `sdf sync` again to retry.")
		return fmt.Errorf("%d branch(es) could not be synced", len(failed))
	}

	if modified {
		bus.Print("\nSync complete. Stack updated.")
	} else {
		bus.Print("\nEverything is in sync.")
	}

	// Offer to create PRs for branches that don't have one yet
	promptCreateMissingPRs(root, s, opts, result, bus)

	// Update PR titles and descriptions if configured
	updatePRContent(root, s, opts, result, bus)

	// Update stack navigation in all PRs (runs even when in sync,
	// to catch stale nav hashes from PRs created before this feature)
	if err := updateStackNavForAllPRs(root, s, result, bus); err != nil {
		bus.Warnf("warning: could not update PR descriptions: %v", err)
	}

	return nil
}

// promptCreateMissingPRs checks for open branches without PRs and offers
// to create them. Runs after sync completes, before nav/content updates.
func promptCreateMissingPRs(root string, s *stack.Stack, opts *syncOptions, result *SyncResult, bus *render.Bus) {
	if opts != nil && opts.jsonMode {
		return
	}
	if !ghpkg.Available() {
		return
	}

	var missing []int // indices of nodes without PRs
	for i, node := range s.Nodes {
		if node.Status != "merged" && node.PR == 0 {
			missing = append(missing, i)
		}
	}
	if len(missing) == 0 {
		return
	}

	cfg := cfgpkg.Defaults()
	if opts != nil {
		cfg = opts.cfg
	}

	bus.Print("")
	for _, idx := range missing {
		node := &s.Nodes[idx]
		base := s.ParentBranch(node.Branch)
		subjects, _ := gitpkg.LogSubjects(base, node.Branch)
		prTitle := cfgpkg.GeneratePRTitle(cfg, s.StackID, node.Branch, subjects)

		bus.Pause()
		ok := ui.Confirm(fmt.Sprintf("%s has no PR. Create one?", ui.Branch(node.Branch)))
		bus.Resume()
		if !ok {
			continue
		}

		body := fmt.Sprintf("Part of stack: **%s**\n\nBase: `%s`", s.StackID, base)

		if err := gitpkg.Push(node.Branch); err != nil {
			if err := gitpkg.PushNew(node.Branch); err != nil {
				bus.Warnf("  %s could not push %s: %v", ui.SymFail, ui.Branch(node.Branch), err)
				continue
			}
		}

		url, err := ghpkg.PRCreate(prTitle, body, base, node.Branch)
		if err != nil {
			bus.Warnf("  %s could not create PR: %v", ui.SymFail, err)
			continue
		}

		pr, err := ghpkg.PRView(node.Branch)
		if err == nil {
			node.PR = pr.Number
			node.Status = "open"
		}

		bus.Printf("  %s PR created: %s", ui.SymOK, url)
	}

	if err := stack.Save(root, s); err != nil {
		bus.Warnf("  %s could not save stack: %v", ui.SymWarn, err)
	}
}

// updatePRContent updates PR titles and descriptions for open PRs in the stack.
// Each title and description is its own parallel task, giving fine-grained
// progress feedback and maximum concurrency.
func updatePRContent(_ string, s *stack.Stack, opts *syncOptions, result *SyncResult, bus *render.Bus) {
	if opts == nil || !opts.withContent {
		return
	}
	if !ghpkg.Available() {
		return
	}

	type contentJob struct {
		node         stack.Node
		prefix       string // conventional commit prefix (e.g. "feat: ")
		localTitle   string // fallback title from branch name
		currentTitle string // current PR title on GitHub
		titlePrompt  string
		descPrompt   string
		titleSession string
		descSession  string
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
		currentTitle, _ := ghpkg.PRViewTitle(node.PR)
		j := contentJob{
			node:         *node,
			localTitle:   localTitle,
			currentTitle: currentTitle,
		}

		if hasClaude {
			diff, _ := gitpkg.DiffFull(parent, node.Branch)
			currentBody, _ := ghpkg.PRViewBody(node.PR)
			currentDesc := extractDescription(currentBody)

			j.prefix = titlePrefix(opts.cfg, node.Branch, subjects)
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

	var updated atomic.Int32
	var mu sync.Mutex
	bus.SetLabel("Updating PR content")
	bus.Print("")
	for _, j := range jobs {
		// Title task
		bus.AddTask(render.TaskSpec{
			ID:   fmt.Sprintf("pr-%d-title", j.node.PR),
			Name: fmt.Sprintf("PR %s title", ui.PR(j.node.PR)),
			Fn: func(ctx context.Context, r *render.Reporter) error {
				currentTitle := j.currentTitle
				prefix := j.prefix
				if p, _, ok := splitConventionalTitle(currentTitle); ok {
					prefix = p
				}

				proposedTitle := j.localTitle
				if prefix != "" {
					proposedTitle = prefix + stripConventionalPrefix(proposedTitle)
				}

				if hasClaude && j.titlePrompt != "" {
					r.Log("generating with Claude...")
					aiTitle, err := claudepkg.RunPrompt(j.titleSession, j.titlePrompt)
					if err == nil {
						aiTitle = strings.Split(aiTitle, "\n")[0]
						aiTitle = strings.Trim(aiTitle, "\"' ")
						if prefix != "" {
							proposedTitle = prefix + stripConventionalPrefix(aiTitle)
						} else {
							proposedTitle = aiTitle
						}
					}
				}

				if !similar(currentTitle, proposedTitle, 0.8) {
					if err := ghpkg.PREditTitle(j.node.PR, proposedTitle); err == nil {
						updated.Add(1)
						if result != nil {
							mu.Lock()
							result.PRUpdates = append(result.PRUpdates, PRUpdate{
								PR: j.node.PR, Field: "title", Status: "updated",
							})
							mu.Unlock()
						}
						r.End("succeeded", fmt.Sprintf("%s PR %s title updated", ui.SymOK, ui.PR(j.node.PR)))
						return nil
					}
				}

				r.End("succeeded", fmt.Sprintf("%s PR %s title unchanged", ui.SymOK, ui.PR(j.node.PR)))
				return nil
			},
		})

		// Description task (Claude only)
		if hasClaude && j.descPrompt != "" {
			bus.AddTask(render.TaskSpec{
				ID:   fmt.Sprintf("pr-%d-desc", j.node.PR),
				Name: fmt.Sprintf("PR %s description", ui.PR(j.node.PR)),
				Fn: func(ctx context.Context, r *render.Reporter) error {
					r.Log("generating with Claude...")
					desc, err := claudepkg.RunPrompt(j.descSession, j.descPrompt)
					if err != nil {
						r.End("failed", fmt.Sprintf("%s PR %s description failed", ui.SymFail, ui.PR(j.node.PR)))
						return nil
					}

					currentBody, _ := ghpkg.PRViewBody(j.node.PR)
					currentDesc := extractDescription(currentBody)
					if !similar(currentDesc, desc, 0.85) {
						newBody := replaceDescription(currentBody, desc)
						if err := ghpkg.PREditBody(j.node.PR, newBody); err == nil {
							updated.Add(1)
							if result != nil {
								mu.Lock()
								result.PRUpdates = append(result.PRUpdates, PRUpdate{
									PR: j.node.PR, Field: "description", Status: "updated",
								})
								mu.Unlock()
							}
							r.End("succeeded", fmt.Sprintf("%s PR %s description updated", ui.SymOK, ui.PR(j.node.PR)))
							return nil
						}
					}

					r.End("succeeded", fmt.Sprintf("%s PR %s description unchanged", ui.SymOK, ui.PR(j.node.PR)))
					return nil
				},
			})
		}
	}
	if err := bus.RunBatch(context.Background()); err != nil {
		bus.Warnf("some PR updates failed: %v", err)
	}
	n := int(updated.Load())
	if n > 0 {
		bus.Printf("\nUpdated %d PR(s).", n)
	} else {
		bus.Print("\nAll PR content is up to date.")
	}
}

func splitConventionalTitle(title string) (prefix, body string, ok bool) {
	parts := strings.SplitN(title, ": ", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	head := parts[0]
	if head == "" {
		return "", "", false
	}
	// Match "type" or "type(scope)" — e.g. "feat", "fix(auth)".
	// Type must start with lowercase ASCII letter; scope (if present)
	// must be wrapped in balanced parens with no nesting.
	inScope := false
	for i, r := range head {
		if i == 0 && (r < 'a' || r > 'z') {
			return "", "", false
		}
		if r == '(' {
			if inScope {
				return "", "", false // nested paren
			}
			inScope = true
			continue
		}
		if r == ')' {
			if !inScope {
				return "", "", false // unmatched close
			}
			inScope = false
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return "", "", false
	}
	if inScope {
		return "", "", false // unclosed paren
	}
	return head + ": ", parts[1], true
}

func stripConventionalPrefix(title string) string {
	_, body, ok := splitConventionalTitle(title)
	if ok {
		return strings.TrimSpace(body)
	}
	return strings.TrimSpace(title)
}

// titlePrefix returns the conventional commit prefix for a PR title
// (e.g. "feat: " or "fix(PROJ-123): "). Returns empty string if
// conventional commits are disabled.
func titlePrefix(cfg cfgpkg.Config, branch string, subjects []string) string {
	if !cfg.ConventionalCommitsEnabled() {
		return ""
	}
	title := cfgpkg.GeneratePRTitle(cfg, "", branch, subjects)
	if idx := strings.Index(title, ": "); idx >= 0 {
		return title[:idx+2]
	}
	return ""
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
	b.WriteString("Use clean GitHub-flavored Markdown.\n")
	b.WriteString("Do not include stack navigation or metadata sections — sdf adds those separately.\n\n")

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

	b.WriteString("\nWrite a concise Markdown description with this structure:\n")
	b.WriteString("## Summary\n")
	b.WriteString("- 1-3 bullets describing what changed and why\n")
	b.WriteString("## Changes\n")
	b.WriteString("- bullets listing key implementation details\n")
	b.WriteString("Use inline code formatting for file/function names where helpful.\n")
	b.WriteString("Keep it short and scannable.")

	return b.String()
}

// reconcileSyncPRStates polls GitHub for PR states and applies lightweight
// reconciliation. Routine changes (status/PR fills) are applied directly;
// structural drift (base mismatches, missing PRs) triggers warnings.
func reconcileSyncPRStates(s *stack.Stack, bus *render.Bus) {
	branches := make([]string, len(s.Nodes))
	for i, n := range s.Nodes {
		branches[i] = n.Branch
	}

	prs, err := ghpkg.PRList(branches)
	if err != nil {
		bus.Warnf("warning: could not poll PR states: %v", err)
		return
	}

	childPRs, err := ghpkg.PRListByBase(branches)
	if err != nil {
		bus.Warnf("warning: could not poll child PR states: %v", err)
	} else {
		prs = ghpkg.MergePRResults(prs, childPRs)
	}

	// Convert gh.PRInfo → stack.PRState
	states := make([]stack.PRState, len(prs))
	for i, pr := range prs {
		states[i] = stack.PRState{
			Number:      pr.Number,
			HeadRefName: pr.HeadRefName,
			BaseRefName: pr.BaseRefName,
			State:       pr.State,
		}
	}

	changes := stack.ReconcileFromPRs(s, states)

	// Apply routine changes
	for _, c := range changes {
		if !c.Notable {
			stack.ApplyRoutineChange(s, c)
		}
	}

	// Print notable warnings
	hasNotable := false
	for _, c := range changes {
		if c.Notable {
			if !hasNotable {
				bus.Print("")
				hasNotable = true
			}
			bus.Warnf("  %s %s", ui.SymWarn, c.Detail)
		}
	}
	if hasNotable {
		bus.Warn("\n  Run `sdf fetch` to reconcile structural changes.\n")
	}
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
			actions = append(actions, syncAction{kind: "skip-merged", branch: node.Branch, pr: node.PR})
			continue
		}

		parent := s.ParentBranch(node.Branch)

		needsRebase := rebased[parent]
		if !needsRebase {
			currentParentTip, err := gitpkg.RevParse(parent)
			if err == nil && node.BaseTip != "" {
				compareTip := currentParentTip
				if opts != nil && !opts.fromHead && parent == s.Base && opts.preFFBaseTip != "" {
					compareTip = opts.preFFBaseTip
				}
				if compareTip != node.BaseTip {
					if gitpkg.IsAncestor(compareTip, node.Branch) {
						actions = append(actions, syncAction{kind: "update-tip", branch: node.Branch, onto: parent})

						if node.PR > 0 && ghpkg.Available() {
							directParent := s.Base
							if i > 0 {
								directParent = nodes[i-1].Branch
							}
							if parent != directParent {
								actions = append(actions, syncAction{kind: "update-pr-base", branch: node.Branch, pr: node.PR, onto: parent})
							}
						}
					} else {
						needsRebase = true
					}
				}
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

func printSyncPlan(plan []syncAction, bus *render.Bus) {
	bus.Print("\nSync plan:")
	for i := 0; i < len(plan); i++ {
		a := plan[i]
		switch a.kind {
		case "skip-merged":
			if a.pr > 0 {
				bus.Printf("  %s PR %s (%s) merged", ui.SymOK, ui.PR(a.pr), ui.Branch(a.branch))
			} else {
				bus.Printf("  %s %s merged", ui.SymOK, ui.Branch(a.branch))
			}
		case "rebase":
			// Combine with next push action for same branch
			if i+1 < len(plan) && plan[i+1].kind == "push" && plan[i+1].branch == a.branch {
				bus.Printf("  %s rebase %s onto %s + push", ui.SymPlan, ui.Branch(a.branch), ui.Branch(a.onto))
				i++ // skip the push
			} else {
				bus.Printf("  %s rebase %s onto %s", ui.SymPlan, ui.Branch(a.branch), ui.Branch(a.onto))
			}
		case "push":
			bus.Printf("  %s push %s", ui.SymPlan, ui.Branch(a.branch))
		case "update-tip":
			bus.Printf("  %s refresh %s base tip", ui.SymPlan, ui.Branch(a.branch))
		case "update-pr-base":
			bus.Printf("  %s update PR %s base → %s", ui.SymPlan, ui.PR(a.pr), ui.Branch(a.onto))
		case "update-content":
			bus.Printf("  %s update PR %s content", ui.SymPlan, ui.PR(a.pr))
		}
	}
	bus.Print("")
}

func confirmSync() bool {
	return ui.Confirm("Proceed?")
}

// conflictAction represents the developer's choice when conflicts occur.
type conflictAction int

const (
	conflictResolved conflictAction = iota
	conflictFailed
	conflictPaused
	conflictAborted
)

func promptOnConflict(root string, s *stack.Stack, branch, originalBranch string, nodeIndex int, rebaseErr error, bus *render.Bus) (conflictAction, error) {
	conflicted, err := gitpkg.ConflictedFiles()
	if err != nil || len(conflicted) == 0 {
		gitpkg.RebaseAbort()
		return conflictFailed, rebaseErr
	}

	bus.Printf("  %s conflict in %s — %d file(s):", ui.SymConflict, ui.Branch(branch), len(conflicted))
	for _, f := range conflicted {
		bus.Printf("    %s", f)
	}
	bus.Print("")

	hasClaude := claudepkg.Available()

	options := []huh.Option[string]{}
	if hasClaude {
		options = append(options, huh.NewOption("Ask Claude to resolve", "claude"))
	}
	options = append(options,
		huh.NewOption("I'll fix it myself (pauses sync)", "manual"),
		huh.NewOption("Skip this branch", "skip"),
		huh.NewOption("Abort sync", "abort"),
	)

	bus.Pause()
	choice := ui.Select("How would you like to resolve?", options)
	bus.Resume()

	switch choice {
	case "claude":
		return tryClaude(root, s, branch, originalBranch, nodeIndex, rebaseErr, conflicted, bus)
	case "manual":
		return pauseForManualResolution(root, s, branch, originalBranch, nodeIndex, bus)
	case "skip":
		gitpkg.RebaseAbort()
		bus.Printf("  Skipped %s.", branch)
		return conflictFailed, fmt.Errorf("conflicts in %s (skipped by user)", branch)
	case "abort", "":
		gitpkg.RebaseAbort()
		return conflictAborted, fmt.Errorf("sync aborted by user at %s", branch)
	default:
		return conflictAborted, fmt.Errorf("sync aborted by user at %s", branch)
	}
}

func tryClaude(root string, s *stack.Stack, branch, originalBranch string, nodeIndex int, rebaseErr error, conflicted []string, bus *render.Bus) (conflictAction, error) {
	bus.Print("  invoking Claude for conflict resolution...")

	parent := s.ParentBranch(branch)
	upstreamSummary, _ := gitpkg.DiffSummary(s.FindNode(branch).BaseTip, parent)

	// Use PR description as context for conflict resolution
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
					bus.Printf("  %s conflict resolved by Claude", ui.SymOK)
					return conflictResolved, nil
				}
			}
		}
	}

	bus.Print("  Claude couldn't fully resolve the conflicts.")
	bus.Print("")

	bus.Pause()
	choice := ui.Select("What next?", []huh.Option[string]{
		huh.NewOption("I'll fix the rest myself (pauses sync)", "manual"),
		huh.NewOption("Skip this branch, continue with the rest", "skip"),
		huh.NewOption("Abort sync entirely", "abort"),
	})
	bus.Resume()

	switch choice {
	case "manual":
		return pauseForManualResolution(root, s, branch, originalBranch, nodeIndex, bus)
	case "skip":
		gitpkg.RebaseAbort()
		return conflictFailed, fmt.Errorf("conflicts in %s (Claude failed, skipped)", branch)
	case "abort", "":
		gitpkg.RebaseAbort()
		return conflictAborted, fmt.Errorf("sync aborted by user at %s", branch)
	default:
		gitpkg.RebaseAbort()
		return conflictAborted, fmt.Errorf("sync aborted by user at %s", branch)
	}
}

func pauseForManualResolution(root string, s *stack.Stack, branch, originalBranch string, nodeIndex int, bus *render.Bus) (conflictAction, error) {
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

	bus.Printf("\n  Sync paused. Resolve conflicts in %s, then:", ui.Branch(branch))
	bus.Print("")
	bus.Print("    1. Edit the conflicted files")
	bus.Print("    2. git add <resolved files>")
	bus.Print("    3. sdf sync --continue")
	bus.Print("")

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

// buildConflictPrompt constructs the conflict resolution prompt for Claude,
// using upstream changes and the PR description for context.
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
