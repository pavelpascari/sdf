// Package stack manages the .sdf/stack.json file and stack topology.
package stack

// PRRecord is a minimal PR representation used for stack discovery.
// It mirrors gh.PRInfo but avoids an import cycle.
type PRRecord struct {
	Number      int
	HeadRefName string
	BaseRefName string
	Status      string // "open", "merged", "closed" — optional, used by Reconcile
}

// DiscoveredStack represents a chain of PRs that form a stack,
// discovered from the GitHub PR graph.
type DiscoveredStack struct {
	Base    string     // root branch (e.g. "main")
	Chains  []PRRecord // ordered from bottom to top of the stack
}

// DiscoverStacks builds a dependency graph from PRs and finds chains
// (sequences of PRs where each PR's base is the previous PR's head).
//
// A chain starts from a root branch (e.g. "main") and follows
// baseRefName → headRefName edges. Only chains of length >= 2 are
// considered stacks (a single PR targeting main is not a stack).
func DiscoverStacks(prs []PRRecord, defaultBranch string) []DiscoveredStack {
	// Build adjacency: baseRefName → list of PRs targeting that base
	children := make(map[string][]PRRecord)
	for _, pr := range prs {
		children[pr.BaseRefName] = append(children[pr.BaseRefName], pr)
	}

	// Also treat any PR whose base is a branch that doesn't appear as
	// anyone's headRefName (and isn't defaultBranch) as a root —
	// handles stacks based on long-lived branches other than main.
	headSet := make(map[string]bool)
	for _, pr := range prs {
		headSet[pr.HeadRefName] = true
	}

	roots := []string{defaultBranch}
	for _, pr := range prs {
		base := pr.BaseRefName
		if base != defaultBranch && !headSet[base] {
			// This base branch is not anyone's head and not the default —
			// it's an alternative root (e.g. "develop").
			already := false
			for _, r := range roots {
				if r == base {
					already = true
					break
				}
			}
			if !already {
				roots = append(roots, base)
			}
		}
	}

	// DFS from each root to find all chains.
	var stacks []DiscoveredStack
	for _, root := range roots {
		chains := findChains(children, root)
		for _, chain := range chains {
			if len(chain) >= 2 {
				stacks = append(stacks, DiscoveredStack{
					Base:   root,
					Chains: chain,
				})
			}
		}
	}

	return stacks
}

// findChains does a DFS from the given root and returns all maximal
// chains (paths) through the PR graph. Each chain is a sequence of
// PRRecords ordered from bottom to top.
func findChains(children map[string][]PRRecord, root string) [][]PRRecord {
	kids := children[root]
	if len(kids) == 0 {
		return nil
	}

	var result [][]PRRecord
	for _, kid := range kids {
		subchains := findChains(children, kid.HeadRefName)
		if len(subchains) == 0 {
			// This kid is a leaf — it's a chain of length 1 by itself
			result = append(result, []PRRecord{kid})
		} else {
			// Prepend this kid to each subchain
			for _, sub := range subchains {
				chain := make([]PRRecord, 0, 1+len(sub))
				chain = append(chain, kid)
				chain = append(chain, sub...)
				result = append(result, chain)
			}
		}
	}
	return result
}
