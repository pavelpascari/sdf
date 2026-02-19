package cmd

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
)

const (
	stackNavOpen  = "<!-- sdf:stack-nav -->"
	stackNavClose = "<!-- /sdf:stack-nav -->"
)

// buildStackNav generates the stack navigation markdown section for a PR.
// currentBranch is the branch whose PR this nav will be embedded in.
func buildStackNav(s *stack.Stack, prs map[int]ghpkg.PRInfo, currentBranch string) string {
	var b strings.Builder

	b.WriteString(stackNavOpen)
	b.WriteString("\n---\n")
	fmt.Fprintf(&b, "Stack: `%s`\n", s.StackID)

	for i, node := range s.Nodes {
		status := strings.ToLower(node.Status)
		if pr, ok := prs[node.PR]; ok {
			status = strings.ToLower(pr.State)
		}

		if node.PR > 0 {
			pr := prs[node.PR]
			url := pr.URL
			if url == "" {
				url = "#"
			}
			fmt.Fprintf(&b, "%d. [#%d %s](%s) - %s", i+1, node.PR, node.Branch, url, status)
		} else {
			fmt.Fprintf(&b, "%d. %s - %s", i+1, node.Branch, status)
		}

		if node.Branch == currentBranch {
			b.WriteString(" ◀ this PR")
		}
		b.WriteString("\n")
	}

	b.WriteString(stackNavClose)
	return b.String()
}

// navHash computes a short hash of the nav content for a given branch.
// Used to skip PR description updates when nothing changed.
func navHash(s *stack.Stack, prs map[int]ghpkg.PRInfo, currentBranch string) string {
	nav := buildStackNav(s, prs, currentBranch)
	h := sha256.Sum256([]byte(nav))
	return fmt.Sprintf("%x", h[:8])
}

// replaceStackNav replaces the stack nav section in a PR body, or appends it
// if no existing section is found. The nav section is always at the bottom.
func replaceStackNav(body, nav string) string {
	openIdx := strings.Index(body, stackNavOpen)
	closeIdx := strings.Index(body, stackNavClose)

	if openIdx >= 0 && closeIdx >= 0 {
		// Replace existing section
		return body[:openIdx] + nav
	}

	// Append at the bottom
	return strings.TrimRight(body, "\n") + "\n\n" + nav
}

// updateStackNavForAllPRs fetches PR info and updates the stack navigation
// section in every PR's description.
func updateStackNavForAllPRs(root string, s *stack.Stack) error {
	// Collect branches that have PRs
	var branches []string
	for _, node := range s.Nodes {
		if node.PR > 0 {
			branches = append(branches, node.Branch)
		}
	}

	if len(branches) == 0 {
		fmt.Println("  No branches with PRs found.")
		return nil
	}
	fmt.Printf("  Checking nav for %d PR(s)...\n", len(branches))

	// Fetch PR info for all branches
	prList, err := ghpkg.PRList(branches)
	if err != nil {
		return fmt.Errorf("cannot fetch PR info: %w", err)
	}

	prMap := make(map[int]ghpkg.PRInfo)
	for _, pr := range prList {
		prMap[pr.Number] = pr
	}

	// Update each PR's description, skipping when nav hasn't changed
	updated := 0
	for i := range s.Nodes {
		node := &s.Nodes[i]
		if node.PR == 0 {
			continue
		}

		hash := navHash(s, prMap, node.Branch)
		if node.NavHash == hash {
			continue
		}

		nav := buildStackNav(s, prMap, node.Branch)

		// Fetch current body
		currentBody, err := ghpkg.PRViewBody(node.PR)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not read PR #%d body: %v\n", node.PR, err)
			continue
		}

		newBody := replaceStackNav(currentBody, nav)

		if err := ghpkg.PREditBody(node.PR, newBody); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not update PR #%d: %v\n", node.PR, err)
			continue
		}

		node.NavHash = hash
		updated++
	}

	if updated > 0 {
		fmt.Printf("Updated %d PR description(s).\n", updated)
		if err := stack.Save(root, s); err != nil {
			return fmt.Errorf("cannot save stack after nav update: %w", err)
		}
	}

	return nil
}
