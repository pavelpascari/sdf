package cmd

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/render"
	splitpkg "github.com/pavelpascari/sdf/internal/split"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
)

var splitCmd = &cobra.Command{
	Use:   "split",
	Short: "Split a branch into a stack of smaller PRs",
	Long: `Uses an AI agent to analyze a branch and decompose it into a chain of
focused, reviewable PRs managed as an sdf stack.

Requires the Claude CLI to be installed. The original branch is never modified.`,
	Example: `  sdf split --from feature/big-change --stack my-feature
  sdf split --from feature/big-change --stack my-feature --dry-run
  sdf split --from feature/big-change --stack my-feature --base main -y`,
	Annotations: map[string]string{"category": "stack"},
	RunE:        runSplitCmd,
}

func init() {
	rootCmd.AddCommand(splitCmd)
	splitCmd.Flags().String("from", "", "source branch to split (required)")
	splitCmd.Flags().String("stack", "", "name for the new stack (required)")
	splitCmd.Flags().String("base", "", "base branch (default: auto-detected)")
	splitCmd.Flags().Bool("dry-run", false, "show the split plan without executing")
	splitCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")
	splitCmd.Flags().Bool("no-push", false, "create branches locally without pushing or creating PRs")
	splitCmd.Flags().Bool("worktrees", false, "create each split branch as a git worktree (worktree mode)")
	_ = splitCmd.MarkFlagRequired("from")
	_ = splitCmd.MarkFlagRequired("stack")
	_ = splitCmd.RegisterFlagCompletionFunc("from", completeGitBranches)
	_ = splitCmd.RegisterFlagCompletionFunc("base", completeGitBranches)
}

// RunSplit is a compatibility wrapper for tests.
func RunSplit(args []string) error {
	rootCmd.SetArgs(append([]string{"split"}, args...))
	return rootCmd.Execute()
}

func runSplitCmd(cmd *cobra.Command, args []string) error {
	fromBranch, _ := cmd.Flags().GetString("from")
	stackName, _ := cmd.Flags().GetString("stack")
	baseFlag, _ := cmd.Flags().GetString("base")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	noPush, _ := cmd.Flags().GetBool("no-push")
	worktreeMode, _ := cmd.Flags().GetBool("worktrees")

	// --- Preconditions ---

	// Claude CLI required
	if !claudepkg.Available() {
		return fmt.Errorf("sdf split requires an AI agent (claude CLI)\n  Install: https://claude.ai/download")
	}

	// Clean working tree
	clean, err := gitpkg.IsClean()
	if err != nil {
		return err
	}
	if !clean {
		return fmt.Errorf("working tree has uncommitted changes — commit or stash them first")
	}

	// Source branch must exist
	if !gitpkg.BranchExists(fromBranch) {
		return fmt.Errorf("branch %q does not exist", fromBranch)
	}

	// Determine base branch
	base := baseFlag
	if base == "" {
		detected, err := gitpkg.DefaultBranch()
		if err != nil {
			return fmt.Errorf("cannot detect base branch: %w\nSpecify one with --base <branch>", err)
		}
		base = detected
	}

	if fromBranch == base {
		return fmt.Errorf("cannot split the base branch %q", base)
	}

	nearestAncestor, err := nearestAncestorBranch(fromBranch, base)
	if err != nil {
		return err
	}
	if nearestAncestor != "" && nearestAncestor != base {
		return fmt.Errorf("branch %q is based on %q, not %q\nsdf split only works on branches based directly off the main branch", fromBranch, nearestAncestor, base)
	}

	root, err := gitpkg.RepoRoot()
	if err != nil {
		return fmt.Errorf("not inside a git repository: %w", err)
	}

	// Check if source branch is already in a stack
	if sdfRoot, findErr := stack.FindRoot(); findErr == nil {
		if _, loadErr := stack.LoadByBranch(sdfRoot, fromBranch); loadErr == nil {
			return fmt.Errorf("branch %q is already in a stack — cannot split", fromBranch)
		}
	}

	// Stack name must be available
	if sdfRoot, findErr := stack.FindRoot(); findErr == nil {
		if _, loadErr := stack.LoadStack(sdfRoot, stackName); loadErr == nil {
			return fmt.Errorf("stack %q already exists — choose a different name with --stack", stackName)
		}
	}

	// Must have changed files
	changedFiles, err := gitpkg.DiffNameOnly(base, fromBranch)
	if err != nil {
		return fmt.Errorf("cannot list changes: %w", err)
	}
	if len(changedFiles) == 0 {
		return fmt.Errorf("no changes to split — %s is up to date with %s", fromBranch, base)
	}
	if len(changedFiles) == 1 {
		return fmt.Errorf("only 1 file changed — nothing to split")
	}

	// --- Analysis ---
	fmt.Printf("Analyzing %s...\n", ui.Branch(fromBranch))

	result, err := splitpkg.Analyze(fromBranch, base, os.Stdout)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	// --- Display plan ---
	displaySplitPlan(result.Plan, stackName, base, fromBranch)

	// --- Save plan to disk ---
	planPath := splitpkg.PlanPath(root, stackName)
	if err := splitpkg.SavePlan(planPath, result.Plan); err != nil {
		fmt.Fprintf(os.Stderr, "  %s could not save plan: %v\n", ui.SymWarn, err)
	} else {
		fmt.Printf("  Plan saved to %s\n", planPath)
	}

	if dryRun {
		return nil
	}

	// --- Choose action ---
	plan := result.Plan
	sessionID := result.SessionID

	if !yes {
		for {
			choice := ui.Select("What would you like to do?", []huh.Option[string]{
				huh.NewOption("Execute this split", "execute"),
				huh.NewOption("Refine plan (opens Claude session)", "refine"),
				huh.NewOption("Abort", "abort"),
			})

			switch choice {
			case "execute":
				goto execute
			case "refine":
				if sessionID == "" {
					fmt.Println("No Claude session available for refinement.")
					continue
				}

				fmt.Println("\nOpening Claude session for plan refinement...")
				fmt.Println("(Type /exit when done)")
				fmt.Println()

				refinePrompt := splitpkg.BuildRefinePrompt(plan)
				if err := claudepkg.RunInteractiveResume(sessionID, refinePrompt); err != nil {
					fmt.Fprintf(os.Stderr, "\n%s Claude session exited with error: %v\n", ui.SymWarn, err)
				}

				fmt.Println("\nRe-reading plan from Claude session...")
				newPlan, err := splitpkg.ReExtractPlan(sessionID, "split-analysis", changedFiles, os.Stdout)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\n%s Could not re-extract plan: %v\n", ui.SymWarn, err)
					fmt.Println("Continuing with the previous plan.")
					displaySplitPlan(plan, stackName, base, fromBranch)
					continue
				}

				plan = newPlan
				// Check for shared files in the refined plan
				shared := splitpkg.SharedFiles(plan)
				if len(shared) > 0 {
					fmt.Printf("\n%d file(s) appear in multiple layers — assigning hunks...\n", len(shared))
					dedupeAndContinue := func(reason string) {
						fmt.Fprintf(os.Stderr, "\n%s %s\n", ui.SymWarn, reason)
						fmt.Println("Shared files deduplicated — each kept in its first layer.")
						plan = splitpkg.DeduplicateSharedFiles(plan)
						_ = splitpkg.SavePlan(planPath, plan)
						displaySplitPlan(plan, stackName, base, fromBranch)
					}

					fileDiffs, hunkCounts, err := splitpkg.ParseSharedFileDiffs(base, fromBranch, shared)
					if err != nil {
						dedupeAndContinue(fmt.Sprintf("Could not parse shared file diffs: %v", err))
						continue
					}

					hunkPrompt := splitpkg.BuildHunkPrompt(shared, fileDiffs)
					sr, err := claudepkg.RunPromptStreamingResume("split-analysis", sessionID, hunkPrompt, os.Stdout)
					if err != nil {
						dedupeAndContinue(fmt.Sprintf("Hunk assignment failed: %v", err))
						continue
					}

					resp, err := splitpkg.ParseHunkAssignment(sr.Result)
					if err != nil {
						dedupeAndContinue(fmt.Sprintf("Could not parse hunk assignments: %v", err))
						continue
					}

					validationErrs := splitpkg.ValidateHunkAssignment(resp, shared, hunkCounts)
					if len(validationErrs) > 0 {
						dedupeAndContinue("Hunk assignment validation failed")
						continue
					}

					plan = splitpkg.MergePlan(plan, resp)
				}

				result.Plan = plan

				fmt.Println()
				displaySplitPlan(plan, stackName, base, fromBranch)

				// Update saved plan
				if err := splitpkg.SavePlan(planPath, plan); err != nil {
					fmt.Fprintf(os.Stderr, "  %s could not save updated plan: %v\n", ui.SymWarn, err)
				}

				continue
			default:
				// abort or empty (user canceled)
				fmt.Println("Aborted.")
				return nil
			}
		}
	}
execute:

	// --- Execute ---
	originalBranch, _ := gitpkg.CurrentBranch()

	fmt.Println("\nExecuting split...")
	var branches []string
	if worktreeMode {
		cfg, _ := cfgpkg.Load(root)
		addFn := func(node *stack.Node, createFrom string) error {
			return addWorktreeForNode(cfg, root, node, createFrom)
		}
		branches, err = splitpkg.ExecuteWorktree(plan, stackName, base, fromBranch, root, addFn)
	} else {
		branches, err = splitpkg.Execute(plan, stackName, base, fromBranch, root)
	}
	if err != nil {
		if worktreeMode {
			// Cleanup worktree-mode branches: remove worktrees and stack file.
			s2, _ := stack.LoadStack(root, stackName)
			if s2 != nil {
				for i := range s2.Nodes {
					_ = removeWorktreeForNode(root, &s2.Nodes[i], true)
				}
			}
			for _, b := range branches {
				_ = gitpkg.DeleteBranch(b)
			}
			_ = os.Remove(stack.StackPath(root, stackName))
		} else {
			splitpkg.Cleanup(branches, originalBranch, root, stackName)
		}
		return err
	}

	// Report each layer
	for i, b := range branches {
		layer := plan.Layers[i]
		fmt.Printf("  %s Layer %d: %s — %d files applied\n",
			ui.SymOK, i+1, ui.Branch(b), len(layer.Files)+len(layer.PartialFiles))
	}

	// --- Validate tree identity ---
	lastBranch := branches[len(branches)-1]
	if err := splitpkg.ValidateTree(fromBranch, lastBranch); err != nil {
		if worktreeMode {
			s2, _ := stack.LoadStack(root, stackName)
			if s2 != nil {
				for i := range s2.Nodes {
					_ = removeWorktreeForNode(root, &s2.Nodes[i], true)
				}
			}
			for _, b := range branches {
				_ = gitpkg.DeleteBranch(b)
			}
			_ = os.Remove(stack.StackPath(root, stackName))
		} else {
			splitpkg.Cleanup(branches, originalBranch, root, stackName)
		}
		return err
	}
	fmt.Printf("  %s Tree identity verified — split is lossless\n", ui.SymOK)

	// --- Delete plan file (split succeeded) ---
	if err := splitpkg.DeletePlan(planPath); err != nil {
		fmt.Fprintf(os.Stderr, "  %s could not delete plan: %v\n", ui.SymWarn, err)
	}

	// --- Save session ID ---
	if result.SessionID != "" {
		sessionID := result.SessionID
		_ = stack.WithLocalLock(root, func(ls *stack.LocalState) error {
			if ls.SplitSessions == nil {
				ls.SplitSessions = make(map[string]string)
			}
			ls.SplitSessions[stackName] = sessionID
			return nil
		})
	}

	if noPush {
		if !worktreeMode {
			gitpkg.Checkout(originalBranch)
		}
		fmt.Printf("\n%s Split complete — %d branches created in stack %q (local only)\n",
			ui.SymOK, len(branches), stackName)
		fmt.Println("\nNext steps:")
		fmt.Println("  sdf status           View the new stack")
		fmt.Println("  sdf pr               Create PRs (run from each branch)")
		if result.SessionID != "" {
			fmt.Printf("\nTo refine this split: claude --resume %s\n", result.SessionID)
		}
		return nil
	}

	// --- Push ---
	fmt.Println("\nPushing branches to origin...")
	pushFailed := false
	for _, b := range branches {
		if err := gitpkg.PushNew(b); err != nil {
			fmt.Fprintf(os.Stderr, "  %s could not push %s: %v\n", ui.SymWarn, ui.Branch(b), err) //nolint:gosec // XSS not applicable in CLI output
			pushFailed = true
		} else {
			fmt.Printf("  %s %s\n", ui.SymOK, ui.Branch(b))
		}
	}

	// --- Create PRs ---
	switch {
	case !pushFailed && ghpkg.Available():
		s, err := stack.LoadStack(root, stackName)
		if err == nil {
			cfg, _ := cfgpkg.Load(root)
			fmt.Println("\nCreating pull requests...")
			if err := createSplitPRs(root, s, cfg, fromBranch, plan); err != nil {
				fmt.Fprintf(os.Stderr, "  %s could not create PRs: %v\n", ui.SymWarn, err)
				fmt.Println("  You can create them manually with: sdf pr (from each branch)")
			}
		}
	case pushFailed:
		fmt.Println("\nSkipped PR creation (push failed for some branches).")
		fmt.Println("Push manually, then create PRs with: sdf pr")
	default:
		fmt.Println("\nSkipped PR creation (gh CLI not available).")
		fmt.Println("Install gh from https://cli.github.com, then run: sdf pr")
	}

	// --- Restore + Report ---
	if !worktreeMode {
		gitpkg.Checkout(originalBranch)
	}

	fmt.Printf("\n%s Split complete — %d branches created in stack %q\n",
		ui.SymOK, len(branches), stackName)

	// Print stack chain
	s, _ := stack.LoadStack(root, stackName)
	if s != nil {
		printStackChain(s)
	}

	if result.SessionID != "" {
		fmt.Printf("\nTo refine this split: claude --resume %s\n", result.SessionID)
	}

	return nil
}

// displaySplitPlan shows the plan with per-layer file counts and line stats.
func displaySplitPlan(plan *splitpkg.Plan, stackName, base, source string) {
	fmt.Printf("\nSplit plan for %s (base: %s)\n", ui.Branch(stackName), ui.Branch(base))
	fmt.Println(strings.Repeat("─", 50))

	totalFiles := 0
	totalPartial := 0
	for i, layer := range plan.Layers {
		fileCount := len(layer.Files) + len(layer.PartialFiles)
		totalFiles += fileCount
		totalPartial += len(layer.PartialFiles)

		// Try to get line stats for this layer
		lineInfo := ""
		allPaths := layer.AllFilePaths()
		if len(allPaths) > 0 {
			diff, err := gitpkg.DiffFiles(base, source, allPaths)
			if err == nil {
				adds, dels := countDiffLines(diff)
				if adds > 0 || dels > 0 {
					lineInfo = fmt.Sprintf(", +%d -%d", adds, dels)
				}
			}
		}

		fmt.Printf("\n  Layer %d: %s (%d files%s)\n",
			i+1, ui.Bold.Render(layer.Name), fileCount, lineInfo)
		fmt.Printf("    %s\n", layer.Description)

		for _, pf := range layer.PartialFiles {
			fmt.Printf("    Shared: %s (hunks %s)\n", pf.Path, formatHunkIndices(pf.Hunks))
		}
	}

	summary := fmt.Sprintf("  Total: %d files across %d layers", totalFiles, len(plan.Layers))
	if totalPartial > 0 {
		seen := make(map[string]bool)
		for _, layer := range plan.Layers {
			for _, pf := range layer.PartialFiles {
				seen[pf.Path] = true
			}
		}
		summary += fmt.Sprintf(" (%d file(s) split at hunk level)", len(seen))
	}
	fmt.Printf("\n%s\n\n", summary)
}

// formatHunkIndices formats a slice of ints as a comma-separated string.
func formatHunkIndices(hunks []int) string {
	parts := make([]string, len(hunks))
	for i, h := range hunks {
		parts[i] = fmt.Sprintf("%d", h)
	}
	return strings.Join(parts, ", ")
}

// countDiffLines counts added and removed lines in a diff string.
func countDiffLines(diff string) (adds, dels int) {
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			adds++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			dels++
		}
	}
	return
}

// createSplitPRs creates GitHub PRs for all branches in the split stack.
func createSplitPRs(root string, s *stack.Stack, cfg cfgpkg.Config, originalBranch string, plan *splitpkg.Plan) error {
	for i := range s.Nodes {
		node := &s.Nodes[i]
		base := s.ParentBranch(node.Branch)

		// Use layer description as title basis
		title := node.Branch
		if i < len(plan.Layers) {
			title = cfgpkg.GeneratePRTitle(cfg, s.StackID, node.Branch, []string{plan.Layers[i].Description})
		}

		body := buildSplitPRBody(s, i, originalBranch)

		fmt.Printf("  %s %s (base: %s)...\n", ui.SymPlan, title, ui.Branch(base))

		url, err := ghpkg.PRCreate(title, body, base, node.Branch, false)
		if err != nil {
			return fmt.Errorf("PR for %s: %w", node.Branch, err)
		}

		pr, err := ghpkg.PRView(node.Branch)
		if err == nil {
			node.PR = pr.Number
			node.Status = "open"
		}

		_ = url
	}

	if err := stack.Save(root, s); err != nil {
		return fmt.Errorf("cannot save stack: %w", err)
	}

	fmt.Println("Updating stack navigation...")
	navBus := render.NewBus(os.Stdout, os.Stderr, render.Options{})
	defer func() { _ = navBus.Finish() }()
	if err := updateStackNavForAllPRs(root, s, nil, navBus); err != nil {
		fmt.Fprintf(os.Stderr, "  %s could not update PR navigation: %v\n", ui.SymWarn, err)
	}

	return nil
}

// buildSplitPRBody generates the PR body for a split branch.
func buildSplitPRBody(s *stack.Stack, nodeIndex int, originalBranch string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Part of stack: **%s**\n\n", s.StackID)
	fmt.Fprintf(&b, "Split from `%s` — PR %d of %d\n", originalBranch, nodeIndex+1, len(s.Nodes))

	base := s.ParentBranch(s.Nodes[nodeIndex].Branch)
	fmt.Fprintf(&b, "\nBase: `%s`", base)

	return b.String()
}

// printStackChain prints a visual representation of the stack.
func printStackChain(s *stack.Stack) {
	parts := []string{s.Base}
	for _, node := range s.Nodes {
		label := node.Branch
		if node.PR > 0 {
			label = fmt.Sprintf("#%d %s", node.PR, node.Branch)
		}
		parts = append(parts, label)
	}
	fmt.Printf("\n  %s\n", strings.Join(parts, " ← "))
}

// nearestAncestorBranch returns the closest local ancestor branch of target,
// preferring the one with the fewest commits between ancestor and target.
// Branches that are ancestors of base (i.e., already merged) are skipped.
func nearestAncestorBranch(target, base string) (string, error) {
	branches, err := gitpkg.LocalBranches()
	if err != nil {
		return "", fmt.Errorf("cannot inspect local branches: %w", err)
	}

	best := ""
	bestDistance := math.MaxInt
	for _, b := range branches {
		if b == target {
			continue
		}
		if gitpkg.IsAncestor(b, base) {
			continue // merged into base, skip
		}
		if !gitpkg.IsAncestor(b, target) {
			continue
		}

		countStr, err := gitpkg.CommitCount(b, target)
		if err != nil {
			continue
		}
		distance, err := strconv.Atoi(countStr)
		if err != nil || distance <= 0 {
			continue
		}
		if distance < bestDistance {
			bestDistance = distance
			best = b
		}
	}
	return best, nil
}
