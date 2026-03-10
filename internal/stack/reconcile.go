package stack

import (
	"fmt"
	"strings"
)

// ReconcileChange describes a single difference between local and discovered state.
type ReconcileChange struct {
	Kind      string // "status", "pr-number", "append", "insert", "remove", "reorder", "base-change", "base-mismatch", "pr-missing", "new-child-pr"
	Branch    string
	Detail    string // human-readable, e.g. "PR #21: open → merged"
	Notable   bool   // true = ⚠ (needs attention), false = ✓ (routine)
	NewStatus string // populated for "status" changes (used by ApplyRoutineChange)
	NewPR     int    // populated for "pr-number" changes (used by ApplyRoutineChange)
}

// PRState mirrors gh.PRInfo fields needed for lightweight reconciliation.
// Avoids importing the gh package from the stack package.
type PRState struct {
	Number      int
	HeadRefName string
	BaseRefName string
	State       string // "OPEN", "MERGED", "CLOSED"
}

// Reconcile compares local stack state against a discovered PR chain from GitHub
// and returns the list of changes needed to bring local in sync.
// It does NOT mutate the stack — call ApplyChanges to apply.
func Reconcile(local *Stack, discovered DiscoveredStack) []ReconcileChange {
	var changes []ReconcileChange

	// Check base branch change
	if local.Base != discovered.Base {
		changes = append(changes, ReconcileChange{
			Kind:    "base-change",
			Detail:  fmt.Sprintf("base: %s → %s", local.Base, discovered.Base),
			Notable: true,
		})
	}

	// Build lookup maps
	localByBranch := make(map[string]int) // branch → index in local.Nodes
	for i, n := range local.Nodes {
		localByBranch[n.Branch] = i
	}

	discoveredByBranch := make(map[string]PRRecord)
	for _, pr := range discovered.Chains {
		discoveredByBranch[pr.HeadRefName] = pr
	}

	// Walk discovered chain to detect additions, status/PR changes, and reorders
	for discIdx, pr := range discovered.Chains {
		localIdx, exists := localByBranch[pr.HeadRefName]
		if !exists {
			// Branch not in local — is it appended (at end) or inserted (in middle)?
			isAppend := discIdx >= len(local.Nodes)
			if isAppend {
				changes = append(changes, ReconcileChange{
					Kind:    "append",
					Branch:  pr.HeadRefName,
					Detail:  fmt.Sprintf("new branch %s (PR #%d)", pr.HeadRefName, pr.Number),
					Notable: false,
				})
			} else {
				changes = append(changes, ReconcileChange{
					Kind:    "insert",
					Branch:  pr.HeadRefName,
					Detail:  fmt.Sprintf("new branch %s (PR #%d) inserted at position %d", pr.HeadRefName, pr.Number, discIdx),
					Notable: true,
				})
			}
			continue
		}

		// Branch exists locally — check for changes
		node := &local.Nodes[localIdx]

		// Status change
		discStatus := prStateToNodeStatus(pr)
		if node.Status != discStatus && discStatus != "" {
			changes = append(changes, ReconcileChange{
				Kind:    "status",
				Branch:  pr.HeadRefName,
				Detail:  fmt.Sprintf("PR #%d: %s → %s", pr.Number, node.Status, discStatus),
				Notable: false,
			})
		}

		// PR number fill (local had 0, discovered has a number)
		if node.PR == 0 && pr.Number != 0 {
			changes = append(changes, ReconcileChange{
				Kind:    "pr-number",
				Branch:  pr.HeadRefName,
				Detail:  fmt.Sprintf("%s: PR #%d discovered", pr.HeadRefName, pr.Number),
				Notable: false,
			})
		}

		// Position change (reorder)
		if localIdx != discIdx {
			changes = append(changes, ReconcileChange{
				Kind:    "reorder",
				Branch:  pr.HeadRefName,
				Detail:  fmt.Sprintf("%s moved from position %d → %d", pr.HeadRefName, localIdx, discIdx),
				Notable: true,
			})
		}
	}

	// Walk local nodes to detect removals (skip merged — they won't
	// appear in the open-PR discovery and that's expected).
	for _, n := range local.Nodes {
		if _, exists := discoveredByBranch[n.Branch]; !exists && n.Status != "merged" {
			changes = append(changes, ReconcileChange{
				Kind:    "remove",
				Branch:  n.Branch,
				Detail:  fmt.Sprintf("branch %s no longer in PR chain", n.Branch),
				Notable: true,
			})
		}
	}

	return changes
}

// ApplyChanges rebuilds the stack from the discovered chain order,
// preserving local-only fields (BaseTip, NavHash) for existing nodes.
// Locally-merged nodes that are absent from the discovered chain are
// kept at the front of the list — they won't appear in open-PR
// discovery, but removing them would lose stack history.
func ApplyChanges(s *Stack, discovered DiscoveredStack, changes []ReconcileChange) {
	// Update base if changed
	for _, c := range changes {
		if c.Kind == "base-change" {
			s.Base = discovered.Base
			break
		}
	}

	// Build lookup of existing nodes for field preservation
	existingByBranch := make(map[string]Node)
	for _, n := range s.Nodes {
		existingByBranch[n.Branch] = n
	}

	// Collect merged nodes absent from the discovered chain
	discoveredSet := make(map[string]bool)
	for _, pr := range discovered.Chains {
		discoveredSet[pr.HeadRefName] = true
	}

	var mergedPrefix []Node
	for _, n := range s.Nodes {
		if n.Status == "merged" && !discoveredSet[n.Branch] {
			mergedPrefix = append(mergedPrefix, n)
		}
	}

	// Rebuild active nodes from discovered order
	activeNodes := make([]Node, len(discovered.Chains))
	for i, pr := range discovered.Chains {
		if existing, ok := existingByBranch[pr.HeadRefName]; ok {
			// Preserve existing node, update from discovered
			activeNodes[i] = existing
			if pr.Number != 0 {
				activeNodes[i].PR = pr.Number
			}
			status := prStateToNodeStatus(pr)
			if status != "" {
				activeNodes[i].Status = status
			}
		} else {
			// New node
			activeNodes[i] = Node{
				Branch: pr.HeadRefName,
				PR:     pr.Number,
				Status: "open",
			}
		}
	}

	// Merged nodes first, then active nodes from discovery
	s.Nodes = append(mergedPrefix, activeNodes...) //nolint:gocritic // intentional: combining two slices into s.Nodes
}

// prStateToNodeStatus converts a PRRecord to a node status string.
// PRRecord from discovery doesn't carry state directly, so we infer
// from context. Returns empty string if no status can be determined.
func prStateToNodeStatus(pr PRRecord) string {
	return pr.Status
}

// ReconcileFromPRs compares local stack state against PR data from GitHub.
// Unlike Reconcile(), this works from per-branch PR info (as returned by
// ghpkg.PRList) without requiring a full DiscoverStacks call.
//
// It detects routine changes (status updates, PR number fills) and
// structural drift (base mismatches, missing PRs). Structural changes
// that require full reconciliation emit notable warnings.
func ReconcileFromPRs(s *Stack, prs []PRState) []ReconcileChange {
	var changes []ReconcileChange

	// Build lookup: branch → PRState
	prByBranch := make(map[string]PRState)
	for _, pr := range prs {
		prByBranch[pr.HeadRefName] = pr
	}

	localByBranch := make(map[string]bool)
	for _, node := range s.Nodes {
		localByBranch[node.Branch] = true
	}

	for i := range s.Nodes {
		node := &s.Nodes[i]

		pr, found := prByBranch[node.Branch]
		if !found {
			// Node has a PR locally but GitHub didn't return it
			if node.PR > 0 {
				changes = append(changes, ReconcileChange{
					Kind:    "pr-missing",
					Branch:  node.Branch,
					Detail:  fmt.Sprintf("PR #%d (%s) not found on GitHub", node.PR, node.Branch),
					Notable: true,
				})
			}
			continue
		}

		// Status change
		newStatus := ghStateToNodeStatus(pr.State)
		if newStatus != "" && node.Status != newStatus {
			changes = append(changes, ReconcileChange{
				Kind:      "status",
				Branch:    node.Branch,
				Detail:    fmt.Sprintf("PR #%d: %s → %s", pr.Number, node.Status, newStatus),
				Notable:   false,
				NewStatus: newStatus,
			})
		}

		// PR number fill
		if node.PR == 0 && pr.Number != 0 {
			changes = append(changes, ReconcileChange{
				Kind:   "pr-number",
				Branch: node.Branch,
				Detail: fmt.Sprintf("%s: PR #%d discovered", node.Branch, pr.Number),
				NewPR:  pr.Number,
			})
		}

		// Base mismatch: check if GitHub's baseRefName matches expected parent
		expectedParent := s.ParentBranch(node.Branch)
		if pr.BaseRefName != "" && pr.BaseRefName != expectedParent {
			changes = append(changes, ReconcileChange{
				Kind:    "base-mismatch",
				Branch:  node.Branch,
				Detail:  fmt.Sprintf("PR #%d (%s) base is %s, expected %s", pr.Number, node.Branch, pr.BaseRefName, expectedParent),
				Notable: true,
			})
		}
	}

	// Detect child PRs that target known stack branches but are not yet in
	// local topology (e.g., collaborator added branch on top or in the middle).
	for _, pr := range prs {
		if localByBranch[pr.HeadRefName] {
			continue
		}
		if pr.BaseRefName == "" || !localByBranch[pr.BaseRefName] {
			continue
		}
		changes = append(changes, ReconcileChange{
			Kind:    "new-child-pr",
			Branch:  pr.HeadRefName,
			Detail:  fmt.Sprintf("new PR #%d (%s) targets %s but is not in local stack", pr.Number, pr.HeadRefName, pr.BaseRefName),
			Notable: true,
		})
	}

	return changes
}

// ApplyRoutineChange applies a single non-notable change to the stack.
func ApplyRoutineChange(s *Stack, c ReconcileChange) {
	node := s.FindNode(c.Branch)
	if node == nil {
		return
	}
	switch c.Kind {
	case "status":
		if c.NewStatus != "" {
			node.Status = c.NewStatus
		}
	case "pr-number":
		if c.NewPR != 0 {
			node.PR = c.NewPR
		}
	}
}

// ghStateToNodeStatus converts a GitHub PR state string to a node status.
func ghStateToNodeStatus(state string) string {
	switch strings.ToUpper(state) {
	case "MERGED":
		return "merged"
	case "CLOSED":
		return "closed"
	case "OPEN":
		return "open"
	default:
		return ""
	}
}
