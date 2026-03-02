// Package gh provides shell-out helpers for the GitHub CLI (gh).
package gh

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// CheckRun represents a single CI check from GitHub's statusCheckRollup.
type CheckRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`     // COMPLETED, IN_PROGRESS, QUEUED, etc.
	Conclusion string `json:"conclusion"` // SUCCESS, FAILURE, SKIPPED, etc.
}

// PRInfo represents pull request information from gh.
type PRInfo struct {
	Number         int        `json:"number"`
	HeadRefName    string     `json:"headRefName"`
	State          string     `json:"state"` // "OPEN", "MERGED", "CLOSED"
	BaseRefName    string     `json:"baseRefName"`
	URL            string     `json:"url"`
	MergeCommit    string     `json:"mergeCommit"`
	StatusChecks   []CheckRun `json:"statusCheckRollup"`
	ReviewDecision string     `json:"reviewDecision"` // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, ""
	Mergeable      string     `json:"mergeable"`      // MERGEABLE, CONFLICTING, UNKNOWN
	IsDraft        bool       `json:"isDraft"`
}

// Binary is the name (or path) of the gh executable.
// Tests can override this to point at a fake binary.
var Binary = "gh"

// run executes a gh command and returns its trimmed stdout.
func run(args ...string) (string, error) {
	cmd := exec.Command(Binary, args...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))

	exitCode := 0
	if err != nil {
		exitCode = 1
	}
	recordRun(args, output, exitCode)

	if err != nil {
		return output, fmt.Errorf("gh %s: %s", strings.Join(args, " "), output)
	}
	return output, nil
}

// Available returns true if the gh CLI is installed and accessible.
func Available() bool {
	_, err := exec.LookPath(Binary)
	return err == nil
}

// Version returns the gh CLI version string.
func Version() (string, error) {
	return run("version")
}

// PRList returns PR information for the given branches.
// It uses GitHub search qualifiers to scope the query to only the requested
// branches, avoiding a full scan of all repository PRs.
func PRList(branches []string) ([]PRInfo, error) {
	if len(branches) == 0 {
		return nil, nil
	}

	// Build a search query that scopes to only the branches we need.
	// GitHub search supports: head:branch1 OR head:branch2 ...
	searchParts := make([]string, len(branches))
	for i, b := range branches {
		searchParts[i] = "head:" + b
	}
	searchQuery := strings.Join(searchParts, " ")

	// Use a limit proportional to the number of branches (with headroom for
	// duplicate PRs per branch, e.g. closed + reopened).
	limit := len(branches) * 3
	if limit < 10 {
		limit = 10
	}

	out, err := run("pr", "list",
		"--state", "all",
		"--json", "number,headRefName,state,baseRefName,url,statusCheckRollup,reviewDecision,mergeable,isDraft",
		"--search", searchQuery,
		"--limit", fmt.Sprintf("%d", limit),
	)
	if err != nil {
		return nil, err
	}

	var prs []PRInfo
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return nil, fmt.Errorf("cannot parse gh pr list output: %w", err)
	}

	// Filter to only branches we care about (safety net — the search query
	// should already scope results, but GitHub search can be fuzzy).
	branchSet := make(map[string]bool)
	for _, b := range branches {
		branchSet[b] = true
	}

	// When multiple PRs exist for the same branch (e.g. one closed, one open
	// after a re-creation), keep only the most relevant one per branch.
	// Priority: OPEN > MERGED > CLOSED.
	best := make(map[string]PRInfo)
	for _, pr := range prs {
		if !branchSet[pr.HeadRefName] {
			continue
		}
		existing, exists := best[pr.HeadRefName]
		if !exists || prStatePriority(pr.State) > prStatePriority(existing.State) {
			best[pr.HeadRefName] = pr
		}
	}

	filtered := make([]PRInfo, 0, len(best))
	for _, pr := range best {
		filtered = append(filtered, pr)
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
	out, err := run("pr", "merge", fmt.Sprintf("%d", prNumber),
		"--"+method, "--delete-branch")
	if err != nil && isRemoteBranchAlreadyDeleted(out) {
		return nil
	}
	return err
}

func isRemoteBranchAlreadyDeleted(output string) bool {
	return strings.Contains(output, "failed to delete remote branch") &&
		(strings.Contains(output, "HTTP 404") || strings.Contains(output, "Reference does not exist"))
}

// ReleaseInfo holds the tag name and URL of a GitHub release.
type ReleaseInfo struct {
	TagName string `json:"tagName"`
	URL     string `json:"url"`
}

// LatestRelease returns the latest published release for the current repo.
func LatestRelease() (*ReleaseInfo, error) {
	out, err := run("release", "view", "--latest", "--json", "tagName,url")
	if err != nil {
		return nil, err
	}

	var rel ReleaseInfo
	if err := json.Unmarshal([]byte(out), &rel); err != nil {
		return nil, fmt.Errorf("cannot parse gh release view output: %w", err)
	}
	return &rel, nil
}

// AggregateCheckStatus computes an overall CI status from individual check runs.
// Returns "pass", "fail", "pending", or "" (no checks).
func AggregateCheckStatus(checks []CheckRun) string {
	if len(checks) == 0 {
		return ""
	}
	hasPending := false
	for _, c := range checks {
		switch strings.ToUpper(c.Status) {
		case "IN_PROGRESS", "QUEUED", "WAITING", "PENDING", "REQUESTED":
			hasPending = true
		case "COMPLETED":
			switch strings.ToUpper(c.Conclusion) {
			case "FAILURE", "CANCELED", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "STALE": //nolint:misspell // GitHub API uses British spelling
				return "fail"
			}
		}
	}
	if hasPending {
		return "pending"
	}
	return "pass"
}

// prStatePriority returns a sort priority for PR states.
// Higher value = more relevant when multiple PRs exist for the same branch.
func prStatePriority(state string) int {
	switch strings.ToUpper(state) {
	case "OPEN":
		return 3
	case "MERGED":
		return 2
	case "CLOSED":
		return 1
	default:
		return 0
	}
}
