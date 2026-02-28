package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
	"github.com/spf13/cobra"
)

// PRResult is the structured output of sdf pr when --json is used.
type PRResult struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	Title  string `json:"title"`
}

var prCmd = &cobra.Command{
	Use:         "pr",
	Short:       "Create a GitHub PR for the current branch",
	Annotations: map[string]string{"category": "stack"},
	RunE:        runPR,
}

func init() {
	rootCmd.AddCommand(prCmd)
	prCmd.Flags().String("title", "", "PR title (default: auto-generated from branch name)")
	prCmd.Flags().Bool("json", false, "output result as JSON")
}

// RunPR is a compatibility wrapper for callers that use the old interface.
func RunPR(args []string) error {
	rootCmd.SetArgs(append([]string{"pr"}, args...))
	return rootCmd.Execute()
}

func runPR(cmd *cobra.Command, args []string) error {
	title, _ := cmd.Flags().GetString("title")
	jsonFlag, _ := cmd.Flags().GetBool("json")

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	branch, err := gitpkg.CurrentBranch()
	if err != nil {
		return fmt.Errorf("cannot determine current branch: %w", err)
	}

	s, err := resolveStack(root, "")
	if err != nil {
		return err
	}

	node := s.FindNode(branch)
	if node == nil {
		return fmt.Errorf("branch %q is not in stack %q — run `sdf branch` to add it", branch, s.StackID)
	}

	if node.PR > 0 {
		return fmt.Errorf("branch %q already has PR #%d", branch, node.PR)
	}

	if !ghpkg.Available() {
		return fmt.Errorf("gh CLI is required to create PRs — install it from https://cli.github.com")
	}

	// Load config for title generation
	cfg, err := cfgpkg.Load(root)
	if err != nil {
		cfg = cfgpkg.Defaults()
	}

	// Determine base branch
	base := s.ParentBranch(branch)

	// Build default PR body
	body := fmt.Sprintf("Part of stack: **%s**\n\nBase: `%s`", s.StackID, base)

	// Determine title
	prTitle := title
	if prTitle == "" {
		// Get commit subjects for conventional commit detection
		var subjects []string
		if cfg.ConventionalCommitsEnabled() {
			subjects, _ = gitpkg.LogSubjects(base, branch)
		}
		prTitle = cfgpkg.GeneratePRTitle(cfg, s.StackID, branch, subjects)
	}

	// Push current branch first
	if !jsonFlag {
		fmt.Printf("Pushing %s to origin...\n", ui.Branch(branch))
	}
	if err := gitpkg.Push(branch); err != nil {
		if err := gitpkg.PushNew(branch); err != nil {
			return fmt.Errorf("cannot push branch: %w", err)
		}
	}

	// Create PR
	if !jsonFlag {
		fmt.Printf("Creating PR: %s (base: %s)...\n", prTitle, ui.Branch(base))
	}
	url, err := ghpkg.PRCreate(prTitle, body, base, branch)
	if err != nil {
		return fmt.Errorf("cannot create PR: %w", err)
	}

	// Get PR details
	pr, err := ghpkg.PRView(branch)
	if err == nil {
		node.PR = pr.Number
		node.Status = "open"
	}

	if err := stack.Save(root, s); err != nil {
		if !jsonFlag {
			fmt.Fprintf(os.Stderr, "warning: could not update stack: %v\n", err)
		}
	}

	if jsonFlag {
		result := PRResult{
			Number: node.PR,
			URL:    url,
			Title:  prTitle,
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("cannot marshal result: %w", err)
		}
		fmt.Println(string(data))
	} else {
		fmt.Println(url)

		// Update stack navigation in all PRs
		fmt.Println("Updating stack navigation in PR descriptions...")
		navBus := render.NewBus(os.Stdout, os.Stderr, render.Options{})
		defer func() { _ = navBus.Finish() }()
		if err := updateStackNavForAllPRs(root, s, nil, navBus); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not update PR descriptions: %v\n", err)
		}
	}

	return nil
}
