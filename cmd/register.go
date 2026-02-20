package cmd

import (
	"fmt"
	"os"
	"strings"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
)

// RunRegister is a deprecation wrapper that delegates to RunFetch.
func RunRegister(args []string) error {
	fmt.Println("Note: `sdf register` has been renamed to `sdf fetch`.")
	fmt.Println()
	return RunFetch(args)
}

// RegisterStack performs the core registration: creates .sdf/stacks/<name>.json
// with nodes from the discovered stack, sets up config, and prints the result.
// This is separated from RunFetch so it can be tested without gh or
// interactive stdin.
func RegisterStack(root, name string, ds stack.DiscoveredStack) error {
	// Check if a stack with this name already exists
	if _, err := stack.LoadStack(root, name); err == nil {
		return fmt.Errorf("stack %q already exists in %s — choose a different name or remove it first", name, root)
	}

	// Build the stack nodes
	nodes := make([]stack.Node, len(ds.Chains))
	for i, pr := range ds.Chains {
		parent := ds.Base
		if i > 0 {
			parent = ds.Chains[i-1].HeadRefName
		}

		parentTip, _ := gitpkg.RevParse(parent)

		nodes[i] = stack.Node{
			Branch:  pr.HeadRefName,
			PR:      pr.Number,
			Status:  "open",
			BaseTip: parentTip,
		}
	}

	s := &stack.Stack{
		StackID: name,
		Base:    ds.Base,
		Nodes:   nodes,
	}

	// Initialize .sdf directory and save
	if err := stack.Init(root, name, ds.Base); err != nil {
		return err
	}
	if err := stack.Save(root, s); err != nil {
		return err
	}

	// Infer prefix config from registered branch names
	cfg := cfgpkg.Defaults()
	if prefix, sep := inferBranchPrefix(nodes, name); prefix != "" {
		cfg.BranchPrefix.Prefix = prefix
		cfg.BranchPrefix.Separator = sep
	}
	if err := cfgpkg.Save(cfgpkg.RepoPath(root), cfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create config: %v\n", err)
	}

	fmt.Printf("\nRegistered stack %q with %d branches (base: %s)\n\n", name, len(nodes), ui.Branch(ds.Base))
	for i, node := range nodes {
		prefix := "├─"
		if i == len(nodes)-1 {
			prefix = "└─"
		}
		fmt.Printf("  %s %s  (PR %s)\n", prefix, ui.Branch(node.Branch), ui.PR(node.PR))
	}
	fmt.Println("\nNext: run `sdf status` to verify, then `sdf pr` on each branch to create PRs.")
	return nil
}

// inferStackName generates a stack name from the branch names in the chain.
// If branches share a common prefix (e.g. "users/db-schema", "users/repo"),
// the prefix is used. Otherwise, the first branch name is used.
func inferStackName(chain []stack.PRRecord) string {
	if len(chain) == 0 {
		return "my-stack"
	}

	names := make([]string, len(chain))
	for i, pr := range chain {
		names[i] = pr.HeadRefName
	}

	// Find common prefix up to a separator (/ or -)
	prefix := names[0]
	for _, name := range names[1:] {
		prefix = commonPrefix(prefix, name)
	}

	// Trim trailing separator
	prefix = strings.TrimRight(prefix, "/-_")

	if prefix == "" || len(prefix) < 3 {
		// No useful common prefix — use the first branch name
		name := names[0]
		// Replace slashes with dashes for a cleaner name
		name = strings.ReplaceAll(name, "/", "-")
		return name
	}

	return strings.ReplaceAll(prefix, "/", "-")
}

func commonPrefix(a, b string) string {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	i := 0
	for i < minLen && a[i] == b[i] {
		i++
	}
	return a[:i]
}

// inferBranchPrefix examines the registered branch names and tries to detect
// a common prefix with separator. If all branches start with stackName+"/"
// or stackName+"-", that prefix and separator are returned. Otherwise returns
// empty strings, meaning the default (stack_id with /) will be used for new branches.
func inferBranchPrefix(nodes []stack.Node, stackName string) (prefix, separator string) {
	if len(nodes) == 0 {
		return "", ""
	}

	// Check if all branches start with stackName + a separator
	for _, sep := range []string{"/", "-"} {
		full := stackName + sep
		allMatch := true
		for _, n := range nodes {
			if !strings.HasPrefix(n.Branch, full) {
				allMatch = false
				break
			}
		}
		if allMatch {
			return stackName, sep
		}
	}

	return "", ""
}
