// Package gh provides shell-out helpers for the GitHub CLI (gh).
package gh

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// PRInfo represents pull request information from gh.
type PRInfo struct {
	Number        int    `json:"number"`
	HeadRefName   string `json:"headRefName"`
	State         string `json:"state"`         // "OPEN", "MERGED", "CLOSED"
	BaseRefName   string `json:"baseRefName"`
	URL           string `json:"url"`
	MergeCommit   string `json:"mergeCommit"`
	StatusChecks  string `json:"statusCheckRollup"`
}

// run executes a gh command and returns its trimmed stdout.
func run(args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		return output, fmt.Errorf("gh %s: %s", strings.Join(args, " "), output)
	}
	return output, nil
}

// Available returns true if the gh CLI is installed and accessible.
func Available() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// Version returns the gh CLI version string.
func Version() (string, error) {
	return run("version")
}

// PRList returns PR information for the given branches.
func PRList(branches []string) ([]PRInfo, error) {
	out, err := run("pr", "list",
		"--state", "all",
		"--json", "number,headRefName,state,baseRefName,url",
		"--limit", "100",
	)
	if err != nil {
		return nil, err
	}

	var prs []PRInfo
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return nil, fmt.Errorf("cannot parse gh pr list output: %w", err)
	}

	// Filter to only branches we care about
	branchSet := make(map[string]bool)
	for _, b := range branches {
		branchSet[b] = true
	}

	var filtered []PRInfo
	for _, pr := range prs {
		if branchSet[pr.HeadRefName] {
			filtered = append(filtered, pr)
		}
	}

	return filtered, nil
}

// PRCreate creates a PR with the given parameters.
func PRCreate(title, body, base, head string) (string, error) {
	args := []string{"pr", "create",
		"--title", title,
		"--body", body,
		"--head", head,
	}
	if base != "" {
		args = append(args, "--base", base)
	}
	return run(args...)
}

// PREditBase updates the base branch of a PR.
func PREditBase(prNumber int, newBase string) error {
	_, err := run("pr", "edit", fmt.Sprintf("%d", prNumber), "--base", newBase)
	return err
}

// PRListForCurrentUser returns all open PRs authored by the current user in this repo.
func PRListForCurrentUser() ([]PRInfo, error) {
	out, err := run("pr", "list",
		"--author", "@me",
		"--state", "open",
		"--json", "number,headRefName,state,baseRefName,url",
		"--limit", "100",
	)
	if err != nil {
		return nil, err
	}

	var prs []PRInfo
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return nil, fmt.Errorf("cannot parse gh pr list output: %w", err)
	}
	return prs, nil
}

// PRView returns PR info for a specific branch.
func PRView(branch string) (*PRInfo, error) {
	out, err := run("pr", "view", branch,
		"--json", "number,headRefName,state,baseRefName,url",
	)
	if err != nil {
		return nil, err
	}

	var pr PRInfo
	if err := json.Unmarshal([]byte(out), &pr); err != nil {
		return nil, fmt.Errorf("cannot parse gh pr view output: %w", err)
	}
	return &pr, nil
}

// PRViewBody returns the body (description) of a PR by number.
func PRViewBody(prNumber int) (string, error) {
	out, err := run("pr", "view", fmt.Sprintf("%d", prNumber),
		"--json", "body",
	)
	if err != nil {
		return "", err
	}

	var result struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return "", fmt.Errorf("cannot parse gh pr view output: %w", err)
	}
	return result.Body, nil
}

// PRViewTitle returns the title of a PR.
func PRViewTitle(prNumber int) (string, error) {
	out, err := run("pr", "view", fmt.Sprintf("%d", prNumber),
		"--json", "title",
	)
	if err != nil {
		return "", err
	}

	var result struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return "", fmt.Errorf("cannot parse gh pr view output: %w", err)
	}
	return result.Title, nil
}

// PREditBody updates the body (description) of a PR.
func PREditBody(prNumber int, body string) error {
	_, err := run("pr", "edit", fmt.Sprintf("%d", prNumber), "--body", body)
	return err
}

// PREditTitle updates the title of a PR.
func PREditTitle(prNumber int, title string) error {
	_, err := run("pr", "edit", fmt.Sprintf("%d", prNumber), "--title", title)
	return err
}

// PRMerge merges a PR using the specified method ("squash", "merge", or "rebase").
// Also deletes the remote branch after merging.
func PRMerge(prNumber int, method string) error {
	_, err := run("pr", "merge", fmt.Sprintf("%d", prNumber),
		"--"+method, "--delete-branch")
	return err
}
