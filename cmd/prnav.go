package cmd

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
)

const (
	stackNavOpen  = "<!-- sdf:stack-nav -->"
	stackNavClose = "<!-- /sdf:stack-nav -->"

	descOpen  = "<!-- sdf:description -->"
	descClose = "<!-- /sdf:description -->"
)

// buildStackNav generates the stack navigation markdown section for a PR.
// currentBranch is the branch whose PR this nav will be embedded in.
func buildStackNav(s *stack.Stack, prs map[int]ghpkg.PRInfo, currentBranch string) string {
	var b strings.Builder

	b.WriteString(stackNavOpen)
	b.WriteString("\n---\n")
	fmt.Fprintf(&b, "Stack: `%s`\n", s.StackID)

	for i, node := range s.Nodes {
		if node.PR > 0 {
			pr := prs[node.PR]
			if pr.URL != "" {
				fmt.Fprintf(&b, "%d. %s", i+1, pr.URL)
			} else {
				fmt.Fprintf(&b, "%d. %s", i+1, node.Branch)
			}
		} else {
			fmt.Fprintf(&b, "%d. %s", i+1, node.Branch)
		}

		if node.Branch == currentBranch {
			b.WriteString(" ◀ this PR")
		}
		b.WriteString("\n")
	}

	b.WriteString("\n<sub>This stack is managed with [sdf](https://stacked-diffs-flow.com).</sub>\n")
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

// replaceDescription replaces the content between sdf:description markers
// in a PR body, or inserts markers at the top if none exist.
// Content outside the markers (user-written content) is preserved.
func replaceDescription(body, description string) string {
	section := descOpen + "\n" + description + "\n" + descClose

	openIdx := strings.Index(body, descOpen)
	closeIdx := strings.Index(body, descClose)

	if openIdx >= 0 && closeIdx >= 0 {
		after := body[closeIdx+len(descClose):]
		return body[:openIdx] + section + after
	}

	// No existing markers — insert at the top, before any existing content
	body = strings.TrimSpace(body)
	if body == "" {
		return section
	}
	return section + "\n\n" + body
}

// backfillMissingPRs fills gaps in prMap for nodes that have a PR number but
// were not returned by PRList (e.g. due to GitHub search indexing delay for
// newly created PRs). It calls lookup for each missing branch individually.
func backfillMissingPRs(s *stack.Stack, prMap map[int]ghpkg.PRInfo, lookup func(branch string) (ghpkg.PRInfo, error)) {
	for _, node := range s.Nodes {
		if node.PR == 0 {
			continue
		}
		if _, ok := prMap[node.PR]; ok {
			continue
		}
		pr, err := lookup(node.Branch)
		if err != nil {
			continue
		}
		prMap[pr.Number] = pr
	}
}

// updateStackNavForAllPRs fetches PR info and updates the stack navigation
// section in every PR's description.
func updateStackNavForAllPRs(root string, s *stack.Stack, result *SyncResult, bus *render.Bus) error {
	// Collect branches that have PRs
	var branches []string
	for _, node := range s.Nodes {
		if node.PR > 0 {
			branches = append(branches, node.Branch)
		}
	}

	if len(branches) == 0 {
		return nil
	}

	// Fetch PR info for all branches
	prList, err := ghpkg.PRList(branches)
	if err != nil {
		return fmt.Errorf("cannot fetch PR info: %w", err)
	}

	prMap := make(map[int]ghpkg.PRInfo)
	for _, pr := range prList {
		prMap[pr.Number] = pr
	}

	// Backfill PRs missing from search results (e.g. newly created PRs
	// not yet indexed by GitHub search).
	backfillMissingPRs(s, prMap, func(branch string) (ghpkg.PRInfo, error) {
		pr, err := ghpkg.PRView(branch)
		if err != nil {
			return ghpkg.PRInfo{}, err
		}
		return *pr, nil
	})

	// Pre-compute hashes and filter to only PRs that need updating.
	type navJob struct {
		nodeIndex int
		hash      string
		nav       string
	}
	var jobs []navJob
	for i := range s.Nodes {
		node := &s.Nodes[i]
		if node.PR == 0 {
			continue
		}
		hash := navHash(s, prMap, node.Branch)
		if node.NavHash == hash {
			continue
		}
		jobs = append(jobs, navJob{
			nodeIndex: i,
			hash:      hash,
			nav:       buildStackNav(s, prMap, node.Branch),
		})
	}

	if len(jobs) == 0 {
		return nil
	}

	// Track successful hash updates to apply after all tasks complete.
	var mu sync.Mutex
	hashUpdates := make(map[int]string) // nodeIndex → hash

	bus.SetLabel("Updating PR navigation")
	bus.Print("")
	for _, j := range jobs {
		node := s.Nodes[j.nodeIndex]
		bus.AddTask(render.TaskSpec{
			ID:   fmt.Sprintf("nav-%d", node.PR),
			Name: fmt.Sprintf("PR %s nav", ui.PR(node.PR)),
			Fn: func(ctx context.Context, r *render.Reporter) error {
				currentBody, err := ghpkg.PRViewBody(node.PR)
				if err != nil {
					r.End("failed", fmt.Sprintf("could not read PR #%d body: %v", node.PR, err))
					return nil // non-fatal
				}

				newBody := replaceStackNav(currentBody, j.nav)
				if err := ghpkg.PREditBody(node.PR, newBody); err != nil {
					r.End("failed", fmt.Sprintf("could not update PR #%d: %v", node.PR, err))
					return nil // non-fatal
				}

				mu.Lock()
				hashUpdates[j.nodeIndex] = j.hash
				if result != nil {
					result.PRUpdates = append(result.PRUpdates, PRUpdate{
						PR: node.PR, Field: "nav", Status: "updated",
					})
				}
				mu.Unlock()

				r.End("succeeded", fmt.Sprintf("PR %s nav updated", ui.PR(node.PR)))
				return nil
			},
		})
	}

	if err := bus.RunBatch(context.Background()); err != nil {
		bus.Warnf("some nav updates failed: %v", err)
	}

	// Apply hash updates to stack nodes.
	for idx, hash := range hashUpdates {
		s.Nodes[idx].NavHash = hash
	}

	if len(hashUpdates) > 0 {
		if err := stack.Save(root, s); err != nil {
			return fmt.Errorf("cannot save stack after nav update: %w", err)
		}
		bus.Printf("\nUpdated %d PR description(s).", len(hashUpdates))
	}

	return nil
}
