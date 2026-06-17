package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	Number    int    `json:"number"`
	Pr        int    `json:"pr"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	Draft     bool   `json:"draft"`
	Created   bool   `json:"created"`
	ErrorCode string `json:"error_code,omitempty"`
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
	prCmd.Flags().Bool("draft", false, "open the PR as a draft")
	prCmd.Flags().Bool("ready", false, "mark the branch's draft PR as ready for review")
	prCmd.Flags().String("branch", "", "target branch (default: current)")
}

// RunPR is a compatibility wrapper for callers that use the old interface.
func RunPR(args []string) error {
	rootCmd.SetArgs(append([]string{"pr"}, args...))
	return rootCmd.Execute()
}

// emitPRResult outputs a PRResult in JSON or human-readable form.
// Task 6 (--ready) will reuse this helper.
func emitPRResult(res PRResult, jsonFlag bool) error {
	if jsonFlag {
		data, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return fmt.Errorf("cannot marshal result: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}
	fmt.Println(res.URL)
	return nil
}

func runPR(cmd *cobra.Command, args []string) error {
	title, _ := cmd.Flags().GetString("title")
	jsonFlag, _ := cmd.Flags().GetBool("json")
	draft, _ := cmd.Flags().GetBool("draft")
	ready, _ := cmd.Flags().GetBool("ready")
	branchFlag, _ := cmd.Flags().GetString("branch")

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	var branch string
	if branchFlag != "" {
		branch = branchFlag
	} else {
		branch, err = gitpkg.CurrentBranch()
		if err != nil {
			return fmt.Errorf("cannot determine current branch: %w", err)
		}
	}

	s, err := resolveStack(root, "")
	if err != nil {
		return err
	}

	node := s.FindNode(branch)
	if node == nil {
		return fmt.Errorf("branch %q is not in stack %q — run `sdf branch` to add it", branch, s.StackID)
	}

	// --ready: flip a draft PR to ready-for-review. Idempotent.
	if ready {
		if node.PR == 0 {
			return fmt.Errorf("no PR for branch %q — run `sdf pr` first", branch)
		}
		if ghpkg.Available() {
			if err := ghpkg.PRReady(node.PR); err != nil {
				return fmt.Errorf("cannot mark PR #%d ready: %w", node.PR, err)
			}
		}
		res := PRResult{Number: node.PR, Pr: node.PR, Draft: false, Created: false}
		if pv, e := ghpkg.PRView(branch); e == nil {
			res.URL = pv.URL
		}
		return emitPRResult(res, jsonFlag)
	}

	// Idempotent: if a PR already exists for this branch, return its details
	// instead of erroring. This allows `sdf pr` to be re-run safely.
	if node.PR > 0 {
		res := PRResult{
			Number:  node.PR,
			Pr:      node.PR,
			Created: false,
		}
		// Best-effort: enrich with live details from gh when available.
		if ghpkg.Available() {
			if info, viewErr := ghpkg.PRView(branch); viewErr == nil {
				res.URL = info.URL
				res.Draft = info.IsDraft
			}
		}
		return emitPRResult(res, jsonFlag)
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

	commitsAhead, err := gitpkg.CommitCount(base, branch)
	if err != nil {
		return fmt.Errorf("cannot determine commit distance between %s and %s: %w", base, branch, err)
	}
	if commitsAhead == "0" {
		return fmt.Errorf("%s has no commits ahead of %s — nothing to open a PR for", branch, base)
	}

	// Build default PR body
	stackBody := fmt.Sprintf("Part of stack: **%s**\n\nBase: `%s`", s.StackID, base)
	body := stackBody
	if tmpl := loadPRTemplate(root); tmpl != "" {
		body = tmpl + "\n\n---\n\n" + stackBody
	}

	// Warn if branch_prefix.scope was previously influencing PR titles
	if cfg.BranchPrefix.Scope != "" && !jsonFlag {
		fmt.Fprintf(os.Stderr, "note: branch_prefix.scope no longer affects PR titles — PR titles now use the stack name as scope\n")
	}

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
	url, err := ghpkg.PRCreate(prTitle, body, base, branch, draft)
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

	result := PRResult{
		Number:  node.PR,
		Pr:      node.PR,
		URL:     url,
		Title:   prTitle,
		Draft:   draft,
		Created: true,
	}

	if jsonFlag {
		return emitPRResult(result, true)
	}

	fmt.Println(url)

	// Update stack navigation in all PRs
	fmt.Println("Updating stack navigation in PR descriptions...")
	navBus := render.NewBus(os.Stdout, os.Stderr, render.Options{})
	defer func() { _ = navBus.Finish() }()
	if err := updateStackNavForAllPRs(root, s, nil, navBus); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not update PR descriptions: %v\n", err)
	}

	return nil
}

// loadPRTemplate finds and returns the repository's PR template content.
// Returns "" if no template is found.
func loadPRTemplate(root string) string {
	candidates := []string{
		filepath.Join(root, ".github", "pull_request_template.md"),
		filepath.Join(root, ".github", "PULL_REQUEST_TEMPLATE.md"),
		filepath.Join(root, "docs", "pull_request_template.md"),
		filepath.Join(root, "PULL_REQUEST_TEMPLATE.md"),
	}
	for _, p := range candidates {
		if data, err := os.ReadFile(p); err == nil {
			return strings.TrimSpace(string(data))
		}
	}

	dir := filepath.Join(root, ".github", "PULL_REQUEST_TEMPLATE")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext != ".md" && ext != ".markdown" {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)
	for _, p := range files {
		if data, err := os.ReadFile(p); err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}
