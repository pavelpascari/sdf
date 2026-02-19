package cmd

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	ctxpkg "github.com/pavelpascari/sdf/internal/context"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
)

func RunRegister(args []string) error {
	fs := flag.NewFlagSet("register", flag.ExitOnError)
	stackName := fs.String("stack", "", "name for the stack (default: auto-generated from branches)")
	base := fs.String("base", "", "base branch (default: auto-detected)")
	fs.Parse(args)

	if !ghpkg.Available() {
		return fmt.Errorf("gh CLI is required for stack discovery — install it from https://cli.github.com")
	}

	root, err := gitpkg.RepoRoot()
	if err != nil {
		return fmt.Errorf("not inside a git repository: %w", err)
	}

	// Migrate legacy layout if needed
	stack.MigrateIfNeeded(root)

	// Detect default branch
	defaultBranch := *base
	if defaultBranch == "" {
		detected, err := gitpkg.DefaultBranch()
		if err != nil {
			return fmt.Errorf("cannot detect default branch: %w — use --base to specify it", err)
		}
		defaultBranch = detected
	}

	fmt.Printf("Scanning your open PRs (base: %s)...\n", defaultBranch)

	// Fetch all open PRs by the current user
	prs, err := ghpkg.PRListForCurrentUser()
	if err != nil {
		return fmt.Errorf("cannot list PRs: %w", err)
	}

	if len(prs) == 0 {
		fmt.Println("No open PRs found for your user.")
		fmt.Println("Use `sdf init <name>` to create a new stack from scratch.")
		return nil
	}

	// Convert to stack.PRRecord for the discovery algorithm
	records := make([]stack.PRRecord, len(prs))
	for i, pr := range prs {
		records[i] = stack.PRRecord{
			Number:      pr.Number,
			HeadRefName: pr.HeadRefName,
			BaseRefName: pr.BaseRefName,
		}
	}

	// Discover stacks
	discovered := stack.DiscoverStacks(records, defaultBranch)

	if len(discovered) == 0 {
		fmt.Println("\nNo stacked PRs found (need at least 2 chained PRs).")
		fmt.Println()
		fmt.Println("Your open PRs:")
		for _, pr := range prs {
			fmt.Printf("  #%-4d  %s → %s\n", pr.Number, pr.HeadRefName, pr.BaseRefName)
		}
		fmt.Println()
		fmt.Println("A stack requires PRs to chain: A → main, B → A, C → B, etc.")
		fmt.Println("Use `sdf init <name>` to create a new stack from scratch.")
		return nil
	}

	// Display discovered stacks
	fmt.Printf("\nFound %d potential stack(s):\n\n", len(discovered))

	for i, ds := range discovered {
		fmt.Printf("  [%d] base: %s\n", i+1, ds.Base)
		for j, pr := range ds.Chains {
			prefix := "  ├─"
			if j == len(ds.Chains)-1 {
				prefix = "  └─"
			}
			fmt.Printf("      %s #%-4d  %s\n", prefix, pr.Number, pr.HeadRefName)
		}
		fmt.Println()
	}

	// Let user pick
	choice := 0
	if len(discovered) == 1 {
		choice = 0
		fmt.Printf("Register this stack? [Y/n] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "n" || answer == "no" {
			fmt.Println("Aborted.")
			return nil
		}
	} else {
		fmt.Printf("Which stack to register? [1-%d] ", len(discovered))
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(answer)
		num, err := strconv.Atoi(answer)
		if err != nil || num < 1 || num > len(discovered) {
			return fmt.Errorf("invalid choice: %s", answer)
		}
		choice = num - 1
	}

	selected := discovered[choice]

	// Determine stack name
	name := *stackName
	if name == "" {
		// Auto-generate from the first branch name: take the common prefix
		name = inferStackName(selected.Chains)
	}

	// Ensure branches exist locally (fetch if needed)
	fmt.Println("\nFetching branches...")
	if err := gitpkg.FetchAll(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: fetch failed: %v\n", err)
	}

	for _, pr := range selected.Chains {
		if !gitpkg.BranchExists(pr.HeadRefName) {
			fmt.Printf("  Checking out %s from origin...\n", pr.HeadRefName)
			if err := gitpkg.CheckoutRemote(pr.HeadRefName); err != nil {
				return fmt.Errorf("cannot checkout %s: %w", pr.HeadRefName, err)
			}
		}
	}

	return RegisterStack(root, name, selected)
}

// RegisterStack performs the core registration: creates .sdf/stacks/<name>.json
// with nodes from the discovered stack, creates context stubs, and commits the
// result. This is separated from RunRegister so it can be tested without gh or
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

	// Create context stubs for each branch
	for i, node := range nodes {
		parent := ds.Base
		if i > 0 {
			parent = nodes[i-1].Branch
		}
		if err := ctxpkg.CreateStub(root, node.Branch, parent); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not create context stub for %s: %v\n", node.Branch, err)
		}
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

	fmt.Printf("\nRegistered stack %q with %d branches (base: %s)\n\n", name, len(nodes), ds.Base)
	for i, node := range nodes {
		prefix := "├─"
		if i == len(nodes)-1 {
			prefix = "└─"
		}
		fmt.Printf("  %s %s  (PR #%d)\n", prefix, node.Branch, node.PR)
	}
	fmt.Printf("\nContext stubs created in .sdf/context/\n")
	fmt.Println("Next: run `sdf context edit` on each branch, then `sdf status` to verify.")
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
