package stack

import "fmt"

// ReconcileChange describes a single difference between local and discovered state.
type ReconcileChange struct {
	Kind    string // "status", "pr-number", "append", "insert", "remove", "reorder", "base-change"
	Branch  string
	Detail  string // human-readable, e.g. "PR #21: open → merged"
	Notable bool   // true = ⚠ (needs attention), false = ✓ (routine)
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

	// Walk local nodes to detect removals
	for _, n := range local.Nodes {
		if _, exists := discoveredByBranch[n.Branch]; !exists {
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

	// Rebuild nodes from discovered order
	newNodes := make([]Node, len(discovered.Chains))
	for i, pr := range discovered.Chains {
		if existing, ok := existingByBranch[pr.HeadRefName]; ok {
			// Preserve existing node, update from discovered
			newNodes[i] = existing
			if pr.Number != 0 {
				newNodes[i].PR = pr.Number
			}
			status := prStateToNodeStatus(pr)
			if status != "" {
				newNodes[i].Status = status
			}
		} else {
			// New node
			newNodes[i] = Node{
				Branch: pr.HeadRefName,
				PR:     pr.Number,
				Status: "open",
			}
		}
	}

	s.Nodes = newNodes
}

// prStateToNodeStatus converts a PRRecord to a node status string.
// PRRecord from discovery doesn't carry state directly, so we infer
// from context. Returns empty string if no status can be determined.
func prStateToNodeStatus(pr PRRecord) string {
	return pr.Status
}
