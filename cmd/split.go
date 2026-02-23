package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
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
	splitCmd.MarkFlagRequired("from")
	splitCmd.MarkFlagRequired("stack")
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

	if dryRun {
		return nil
	}

	// --- Confirm ---
	if !yes {
		if !ui.Confirm("Execute this split?") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// --- Execute ---
	originalBranch, _ := gitpkg.CurrentBranch()

	fmt.Println("\nExecuting split...")
	branches, err := splitpkg.Execute(result.Plan, stackName, base, fromBranch, root)
	if err != nil {
		splitpkg.Cleanup(branches, originalBranch, root, stackName)
		return err
	}

	// Report each layer
	for i, b := range branches {
		layer := result.Plan.Layers[i]
		fmt.Printf("  %s Layer %d: %s — %d files applied\n",
			ui.SymOK, i+1, ui.Branch(b), len(layer.Files)+len(layer.PartialFiles))
	}

	// --- Validate tree identity ---
	lastBranch := branches[len(branches)-1]
	if err := splitpkg.ValidateTree(fromBranch, lastBranch); err != nil {
		splitpkg.Cleanup(branches, originalBranch, root, stackName)
		return err
	}
	fmt.Printf("  %s Tree identity verified — split is lossless\n", ui.SymOK)

	// --- Save session ID ---
	if result.SessionID != "" {
		local, _ := stack.LoadLocal(root)
		if local.SplitSessions == nil {
			local.SplitSessions = make(map[string]string)
		}
		local.SplitSessions[stackName] = result.SessionID
		stack.SaveLocal(root, local)
	}

	if noPush {
		gitpkg.Checkout(originalBranch)
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
			fmt.Fprintf(os.Stderr, "  %s could not push %s: %v\n", ui.SymWarn, ui.Branch(b), err)
			pushFailed = true
		} else {
			fmt.Printf("  %s %s\n", ui.SymOK, ui.Branch(b))
		}
	}

	// --- Create PRs ---
	if !pushFailed && ghpkg.Available() {
		s, err := stack.LoadStack(root, stackName)
		if err == nil {
			cfg, _ := cfgpkg.Load(root)
			fmt.Println("\nCreating pull requests...")
			if err := createSplitPRs(root, s, cfg, fromBranch, result.Plan); err != nil {
				fmt.Fprintf(os.Stderr, "  %s could not create PRs: %v\n", ui.SymWarn, err)
				fmt.Println("  You can create them manually with: sdf pr (from each branch)")
			}
		}
	} else if pushFailed {
		fmt.Println("\nSkipped PR creation (push failed for some branches).")
		fmt.Println("Push manually, then create PRs with: sdf pr")
	} else {
		fmt.Println("\nSkipped PR creation (gh CLI not available).")
		fmt.Println("Install gh from https://cli.github.com, then run: sdf pr")
	}

	// --- Restore + Report ---
	gitpkg.Checkout(originalBranch)

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

		url, err := ghpkg.PRCreate(title, body, base, node.Branch)
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
	if err := updateStackNavForAllPRs(root, s); err != nil {
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

