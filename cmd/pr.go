package cmd

import (
	"flag"
	"fmt"
	"os"
	"strings"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
)

func RunPR(args []string) error {
	fs := flag.NewFlagSet("pr", flag.ExitOnError)
	title := fs.String("title", "", "PR title (default: branch name)")
	fs.Parse(args)

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

	// Determine base branch
	base := s.ParentBranch(branch)

	// Build default PR body
	body := fmt.Sprintf("Part of stack: **%s**\n\nBase: `%s`", s.StackID, base)

	// Determine title
	prTitle := *title
	if prTitle == "" {
		// Use branch name, replacing slashes and dashes with spaces
		prTitle = branch
		prTitle = strings.ReplaceAll(prTitle, "/", ": ")
		prTitle = strings.ReplaceAll(prTitle, "-", " ")
	}

	// Push current branch first
	fmt.Printf("Pushing %s to origin...\n", branch)
	if err := gitpkg.Push(branch); err != nil {
		// Try regular push if force-with-lease fails
		if err := gitpkg.PushNew(branch); err != nil {
			return fmt.Errorf("cannot push branch: %w", err)
		}
	}

	// Create PR
	fmt.Printf("Creating PR: %s (base: %s)...\n", prTitle, base)
	url, err := ghpkg.PRCreate(prTitle, body, base, branch)
	if err != nil {
		return fmt.Errorf("cannot create PR: %w", err)
	}

	// Try to extract PR number from gh output
	pr, err := ghpkg.PRView(branch)
	if err == nil {
		node.PR = pr.Number
		node.Status = "open"
	}

	if err := stack.Save(root, s); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not update stack: %v\n", err)
	}

	fmt.Println(url)

	// Update stack navigation in all PRs
	fmt.Println("Updating stack navigation in PR descriptions...")
	if err := updateStackNavForAllPRs(root, s); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not update PR descriptions: %v\n", err)
	}

	return nil
}
