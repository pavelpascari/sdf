package cmd

import (
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
)

// commitInfo holds metadata about a single commit for split analysis.
type commitInfo struct {
	SHA     string
	Subject string
	Files   []string
}

// splitGroup represents a proposed group of commits that will become one PR.
type splitGroup struct {
	Title   string
	Commits []commitInfo
}

var splitCmd = &cobra.Command{
	Use:   "split",
	Short: "Split a branch into a stack of smaller PRs",
	Long: `Analyzes the current branch's commits and proposes a split into
multiple branches, each targeting a coherent set of changes. The result
is a standard sdf stack that works with all other sdf commands.

The original branch is never modified.`,
	Example: `  sdf split                        # auto-analyze and split
  sdf split --parts 3              # split into exactly 3 parts
  sdf split --stack my-feature     # name the new stack
  sdf split --dry-run              # show plan without executing`,
	Annotations: map[string]string{"category": "stack"},
	RunE:        runSplitCmd,
}

func init() {
	rootCmd.AddCommand(splitCmd)
	splitCmd.Flags().Bool("dry-run", false, "show the split plan without executing")
	splitCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")
	splitCmd.Flags().String("stack", "", "name for the new stack (default: branch name)")
	splitCmd.Flags().String("base", "", "base branch (default: auto-detected)")
	splitCmd.Flags().IntP("parts", "n", 0, "number of parts to split into (0 = auto)")
	splitCmd.Flags().Bool("no-push", false, "create branches locally without pushing or creating PRs")
}

// RunSplit is a compatibility wrapper for tests.
func RunSplit(args []string) error {
	rootCmd.SetArgs(append([]string{"split"}, args...))
	return rootCmd.Execute()
}

func runSplitCmd(cmd *cobra.Command, args []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	stackName, _ := cmd.Flags().GetString("stack")
	baseFlag, _ := cmd.Flags().GetString("base")
	parts, _ := cmd.Flags().GetInt("parts")
	noPush, _ := cmd.Flags().GetBool("no-push")

	// Get current branch
	branch, err := gitpkg.CurrentBranch()
	if err != nil {
		return fmt.Errorf("cannot determine current branch: %w", err)
	}

	// Check working tree is clean
	clean, err := gitpkg.IsClean()
	if err != nil {
		return err
	}
	if !clean {
		return fmt.Errorf("working tree has uncommitted changes — commit or stash them first")
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

	if branch == base {
		return fmt.Errorf("cannot split the base branch %q — checkout a feature branch first", base)
	}

	root, err := gitpkg.RepoRoot()
	if err != nil {
		return fmt.Errorf("not inside a git repository: %w", err)
	}

	// Check if branch is already in a stack
	if sdfRoot, findErr := stack.FindRoot(); findErr == nil {
		if _, loadErr := stack.LoadByBranch(sdfRoot, branch); loadErr == nil {
			return fmt.Errorf("branch %q is already in a stack — cannot split", branch)
		}
	}

	// Determine stack name
	if stackName == "" {
		stackName = branch
	}

	// Check if stack name is already taken
	if sdfRoot, findErr := stack.FindRoot(); findErr == nil {
		if _, loadErr := stack.LoadStack(sdfRoot, stackName); loadErr == nil {
			return fmt.Errorf("stack %q already exists — choose a different name with --stack", stackName)
		}
	}

	// Analyze commits
	commits, err := analyzeBranch(base, branch)
	if err != nil {
		return err
	}
	if len(commits) == 0 {
		return fmt.Errorf("no commits to split — %s is up to date with %s", branch, base)
	}
	if len(commits) < 2 {
		return fmt.Errorf("only 1 commit — nothing to split")
	}

	// Group commits
	var groups []splitGroup
	if parts > 0 {
		if parts > len(commits) {
			return fmt.Errorf("cannot split %d commits into %d parts", len(commits), parts)
		}
		groups = equalSplit(commits, parts)
	} else {
		groups = autoGroup(commits)
	}

	if len(groups) < 2 {
		fmt.Println("All commits are tightly coupled — no natural split points found.")
		fmt.Println("Use --parts N to force a split into N parts.")
		return nil
	}

	// Display plan
	displayPlan(groups, stackName, base, len(commits))

	if dryRun {
		return nil
	}

	// Confirm
	if !yes {
		if !ui.Confirm("Execute this split?") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Execute
	createdBranches, err := executeSplit(root, groups, stackName, base)
	if err != nil {
		// Cleanup on failure
		cleanupBranches(createdBranches, branch)
		return err
	}

	// Validate tree identity
	lastBranch := createdBranches[len(createdBranches)-1]
	diff, err := gitpkg.DiffFull(branch, lastBranch)
	if err != nil {
		cleanupBranches(createdBranches, branch)
		return fmt.Errorf("cannot verify split: %w", err)
	}
	if diff != "" {
		cleanupBranches(createdBranches, branch)
		return fmt.Errorf("split validation failed — tree differs from original branch (this is a bug)")
	}

	fmt.Printf("\n%s Tree identity verified — split is lossless\n", ui.SymOK)

	if noPush {
		gitpkg.Checkout(branch)
		fmt.Printf("\n%s Split complete — %d branches created in stack %q (local only)\n",
			ui.SymOK, len(createdBranches), stackName)
		fmt.Println("\nNext steps:")
		fmt.Println("  sdf status           View the new stack")
		fmt.Println("  sdf pr               Create PRs (run from each branch)")
		return nil
	}

	// Push all branches
	fmt.Println("\nPushing branches to origin...")
	pushFailed := false
	for _, b := range createdBranches {
		if err := gitpkg.PushNew(b); err != nil {
			fmt.Fprintf(os.Stderr, "  %s could not push %s: %v\n", ui.SymWarn, ui.Branch(b), err)
			pushFailed = true
		} else {
			fmt.Printf("  %s %s\n", ui.SymOK, ui.Branch(b))
		}
	}

	// Create PRs (requires gh CLI and successful push)
	if !pushFailed && ghpkg.Available() {
		s, err := stack.LoadStack(root, stackName)
		if err == nil {
			cfg, _ := cfgpkg.Load(root)
			fmt.Println("\nCreating pull requests...")
			if err := createSplitPRs(root, s, cfg, branch, groups); err != nil {
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

	// Restore original branch
	gitpkg.Checkout(branch)

	fmt.Printf("\n%s Split complete — %d branches created in stack %q\n", ui.SymOK, len(createdBranches), stackName)
	return nil
}

// analyzeBranch gathers per-commit metadata between base and head.
func analyzeBranch(base, head string) ([]commitInfo, error) {
	shas, err := gitpkg.LogCommits(base, head)
	if err != nil {
		return nil, fmt.Errorf("cannot list commits: %w", err)
	}

	commits := make([]commitInfo, 0, len(shas))
	for _, sha := range shas {
		subject, err := gitpkg.ShowSubject(sha)
		if err != nil {
			return nil, fmt.Errorf("cannot read commit %s: %w", sha[:8], err)
		}

		files, err := gitpkg.ShowFiles(sha)
		if err != nil {
			return nil, fmt.Errorf("cannot list files for %s: %w", sha[:8], err)
		}

		commits = append(commits, commitInfo{
			SHA:     sha,
			Subject: subject,
			Files:   files,
		})
	}

	return commits, nil
}

// autoGroup groups consecutive commits by directory affinity.
// Adjacent commits that share no directories start a new group.
// Returns a single group (no split) if all commits are interconnected.
func autoGroup(commits []commitInfo) []splitGroup {
	n := len(commits)
	if n <= 2 {
		return []splitGroup{{Title: deriveTitle(commits), Commits: commits}}
	}

	// Find boundaries where adjacent commits share no directories
	var breaks []int
	for i := 0; i < n-1; i++ {
		if !dirsOverlap(commits[i].Files, commits[i+1].Files) {
			breaks = append(breaks, i)
		}
	}

	// No natural breaks — everything is interconnected
	if len(breaks) == 0 {
		return []splitGroup{{Title: deriveTitle(commits), Commits: commits}}
	}

	// Build groups from break points
	var groups []splitGroup
	start := 0
	for _, b := range breaks {
		end := b + 1
		g := make([]commitInfo, end-start)
		copy(g, commits[start:end])
		groups = append(groups, splitGroup{
			Title:   deriveTitle(g),
			Commits: g,
		})
		start = end
	}
	// Last group
	g := make([]commitInfo, n-start)
	copy(g, commits[start:])
	groups = append(groups, splitGroup{
		Title:   deriveTitle(g),
		Commits: g,
	})

	return groups
}

// equalSplit divides commits into exactly n contiguous groups.
func equalSplit(commits []commitInfo, n int) []splitGroup {
	total := len(commits)
	groups := make([]splitGroup, n)

	base := total / n
	extra := total % n

	idx := 0
	for i := 0; i < n; i++ {
		size := base
		if i < extra {
			size++
		}
		g := make([]commitInfo, size)
		copy(g, commits[idx:idx+size])
		groups[i] = splitGroup{
			Title:   deriveTitle(g),
			Commits: g,
		}
		idx += size
	}

	return groups
}

// dirsOverlap returns true if any file directory is shared between two file lists.
func dirsOverlap(filesA, filesB []string) bool {
	dirsA := fileDirs(filesA)
	for _, f := range filesB {
		dir := path.Dir(f)
		if dirsA[dir] {
			return true
		}
	}
	return false
}

// fileDirs returns the set of directories from a file list.
func fileDirs(files []string) map[string]bool {
	dirs := make(map[string]bool, len(files))
	for _, f := range files {
		dirs[path.Dir(f)] = true
	}
	return dirs
}

// deriveTitle generates a short title from the most common directory in a group.
func deriveTitle(commits []commitInfo) string {
	dirCount := map[string]int{}
	for _, c := range commits {
		for _, f := range c.Files {
			dirCount[path.Dir(f)]++
		}
	}

	if len(dirCount) == 0 {
		return "changes"
	}

	// Find most common directory
	type dirEntry struct {
		dir   string
		count int
	}
	var entries []dirEntry
	for dir, count := range dirCount {
		entries = append(entries, dirEntry{dir, count})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].count > entries[j].count
	})

	topDir := entries[0].dir
	name := path.Base(topDir)
	if name == "." {
		name = "root"
	}

	return name
}

// displayPlan prints the split plan for user review.
func displayPlan(groups []splitGroup, stackName, base string, totalCommits int) {
	fmt.Printf("\nSplit plan for %s (base: %s, %d commits)\n",
		ui.Branch(stackName), ui.Branch(base), totalCommits)
	fmt.Println(strings.Repeat("─", 50))

	for i, g := range groups {
		fmt.Printf("\n  Part %d: %s (%d commits)\n", i+1, ui.Bold.Render(g.Title), len(g.Commits))
		for _, c := range g.Commits {
			short := c.SHA
			if len(short) > 7 {
				short = short[:7]
			}
			fmt.Printf("    %s  %s\n", ui.Gray.Render(short), c.Subject)
		}
	}

	fmt.Printf("\n  Stack: %q → %d branches based on %s\n\n",
		stackName, len(groups), ui.Branch(base))
}

// executeSplit creates branches and cherry-picks commits for each group.
// Returns the list of created branch names.
func executeSplit(root string, groups []splitGroup, stackID, base string) ([]string, error) {
	stack.MigrateIfNeeded(root)

	if err := stack.Init(root, stackID, base); err != nil {
		return nil, fmt.Errorf("cannot initialize stack: %w", err)
	}

	// Load config for branch naming
	cfg, err := cfgpkg.Load(root)
	if err != nil {
		cfg = cfgpkg.Defaults()
	}

	s, err := stack.LoadStack(root, stackID)
	if err != nil {
		return nil, fmt.Errorf("cannot load stack: %w", err)
	}

	var createdBranches []string
	parent := base

	for i, g := range groups {
		shortName := fmt.Sprintf("%d-%s", i+1, sanitizeBranchComponent(g.Title))
		branchName := cfgpkg.ApplyPrefix(cfg, stackID, shortName)

		fmt.Printf("  Creating %s...\n", ui.Branch(branchName))

		if err := gitpkg.Checkout(parent); err != nil {
			return createdBranches, fmt.Errorf("cannot checkout %s: %w", parent, err)
		}

		if err := gitpkg.CreateBranch(branchName); err != nil {
			return createdBranches, fmt.Errorf("cannot create branch %s: %w", branchName, err)
		}
		createdBranches = append(createdBranches, branchName)

		// Cherry-pick all commits in this group
		shas := make([]string, len(g.Commits))
		for j, c := range g.Commits {
			shas[j] = c.SHA
		}
		if err := gitpkg.CherryPick(shas...); err != nil {
			return createdBranches, fmt.Errorf("cherry-pick failed on %s: %w", branchName, err)
		}

		// Record node
		parentTip, _ := gitpkg.RevParse(parent)
		s.Nodes = append(s.Nodes, stack.Node{
			Branch:  branchName,
			Status:  "open",
			BaseTip: parentTip,
		})

		parent = branchName
	}

	if err := stack.Save(root, s); err != nil {
		return createdBranches, fmt.Errorf("cannot save stack: %w", err)
	}

	return createdBranches, nil
}

// cleanupBranches deletes created branches and restores the original branch.
func cleanupBranches(branches []string, restoreTo string) {
	gitpkg.Checkout(restoreTo)
	for _, b := range branches {
		gitpkg.DeleteBranch(b)
	}
}

// createSplitPRs creates GitHub PRs for all branches in the split stack.
// It generates titles from config, builds bodies with split context, and
// updates stack navigation in all PR descriptions.
func createSplitPRs(root string, s *stack.Stack, cfg cfgpkg.Config, originalBranch string, groups []splitGroup) error {
	for i := range s.Nodes {
		node := &s.Nodes[i]
		base := s.ParentBranch(node.Branch)

		// Generate title
		var subjects []string
		if i < len(groups) {
			for _, c := range groups[i].Commits {
				subjects = append(subjects, c.Subject)
			}
		}
		title := cfgpkg.GeneratePRTitle(cfg, s.StackID, node.Branch, subjects)

		// Build body
		body := buildSplitPRBody(s, i, originalBranch)

		fmt.Printf("  %s %s (base: %s)...\n", ui.SymPlan, title, ui.Branch(base))

		url, err := ghpkg.PRCreate(title, body, base, node.Branch)
		if err != nil {
			return fmt.Errorf("PR for %s: %w", node.Branch, err)
		}

		// Fetch PR details
		pr, err := ghpkg.PRView(node.Branch)
		if err == nil {
			node.PR = pr.Number
			node.Status = "open"
		}

		_ = url // displayed by gh
	}

	if err := stack.Save(root, s); err != nil {
		return fmt.Errorf("cannot save stack: %w", err)
	}

	// Update stack navigation in all PR descriptions
	fmt.Println("Updating stack navigation...")
	if err := updateStackNavForAllPRs(root, s); err != nil {
		fmt.Fprintf(os.Stderr, "  %s could not update PR navigation: %v\n", ui.SymWarn, err)
	}

	return nil
}

// buildSplitPRBody generates the initial PR body for a split branch.
// Includes split context and a placeholder for the stack nav section
// (which gets populated by updateStackNavForAllPRs).
func buildSplitPRBody(s *stack.Stack, nodeIndex int, originalBranch string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Part of stack: **%s**\n\n", s.StackID)
	fmt.Fprintf(&b, "Split from `%s` — PR %d of %d\n", originalBranch, nodeIndex+1, len(s.Nodes))

	base := s.ParentBranch(s.Nodes[nodeIndex].Branch)
	fmt.Fprintf(&b, "\nBase: `%s`", base)

	return b.String()
}

// sanitizeBranchComponent produces a branch-name-safe string.
func sanitizeBranchComponent(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, s)
	s = strings.Trim(s, "-")
	if s == "" {
		s = "changes"
	}
	return s
}
