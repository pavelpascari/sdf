package cmd

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
)

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Discover and sync PR stacks from GitHub",
	Long: `Scans your open PRs, detects branch chains, and either registers
a new stack or reconciles an existing one.`,
	Example: `  sdf fetch                          # auto-detect base, scan all open PRs
  sdf fetch --stack my-feature       # name the stack explicitly
  sdf fetch --base develop           # specify base branch`,
	Annotations: map[string]string{"category": "stack"},
	RunE:        runFetchCmd,
}

func init() {
	rootCmd.AddCommand(fetchCmd)
	fetchCmd.Flags().String("stack", "", "name for the stack (default: auto-generated from branches)")
	fetchCmd.Flags().String("base", "", "base branch (default: auto-detected)")
}

func runFetchCmd(cmd *cobra.Command, args []string) error {
	stackName, _ := cmd.Flags().GetString("stack")
	base, _ := cmd.Flags().GetString("base")

	return runFetchLogic(stackName, base)
}

// RunFetch is a compatibility wrapper for callers that use the old interface.
func RunFetch(args []string) error {
	rootCmd.SetArgs(append([]string{"fetch"}, args...))
	return rootCmd.Execute()
}

func runFetchLogic(stackName, base string) error {

	if !ghpkg.Available() {
		return fmt.Errorf("gh CLI is required for stack discovery — install it from https://cli.github.com")
	}

	root, err := gitpkg.RepoRoot()
	if err != nil {
		return fmt.Errorf("not inside a git repository: %w", err)
	}

	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{})
	defer func() { _ = bus.Finish() }()

	stack.MigrateIfNeeded(root)

	// Detect default branch
	defaultBranch := base
	if defaultBranch == "" {
		detected, err := gitpkg.DefaultBranch()
		if err != nil {
			return fmt.Errorf("cannot detect default branch: %w — use --base to specify it", err)
		}
		defaultBranch = detected
	}

	bus.Printf("Scanning your open PRs (base: %s)...", defaultBranch)

	// Fetch all open PRs by the current user
	prs, err := ghpkg.PRListForCurrentUser()
	if err != nil {
		return fmt.Errorf("cannot list PRs: %w", err)
	}

	if len(prs) == 0 {
		bus.Print("No open PRs found for your user.")
		bus.Print("Use `sdf new <name>` to create a new stack from scratch.")
		return nil
	}

	// Convert to stack.PRRecord for the discovery algorithm
	records := make([]stack.PRRecord, len(prs))
	for i, pr := range prs {
		records[i] = stack.PRRecord{
			Number:      pr.Number,
			HeadRefName: pr.HeadRefName,
			BaseRefName: pr.BaseRefName,
			Status:      "open", // PRListForCurrentUser returns open PRs
		}
	}

	// Discover stacks
	discovered := stack.DiscoverStacks(records, defaultBranch)

	if len(discovered) == 0 {
		bus.Print("\nNo stacked PRs found (need at least 2 chained PRs).")
		bus.Print("")
		bus.Print("Your open PRs:")
		for _, pr := range prs {
			bus.Printf("  #%-4d  %s → %s", pr.Number, pr.HeadRefName, pr.BaseRefName)
		}
		bus.Print("")
		bus.Print("A stack requires PRs to chain: A → main, B → A, C → B, etc.")
		bus.Print("Use `sdf new <name>` to create a new stack from scratch.")
		return nil
	}

	// Display discovered stacks
	bus.Printf("\nFound %d potential stack(s):\n", len(discovered))

	for i, ds := range discovered {
		bus.Printf("  [%d] base: %s", i+1, ds.Base)
		for j, pr := range ds.Chains {
			prefix := "  ├─"
			if j == len(ds.Chains)-1 {
				prefix = "  └─"
			}
			bus.Printf("      %s #%-4d  %s", prefix, pr.Number, pr.HeadRefName)
		}
		bus.Print("")
	}

	// Let user pick
	choice := 0
	if len(discovered) == 1 {
		bus.Pause()
		proceed := ui.Confirm("Fetch this stack?")
		bus.Resume()
		if !proceed {
			bus.Print("Aborted.")
			return nil
		}
	} else {
		options := make([]huh.Option[string], len(discovered))
		for i, ds := range discovered {
			label := fmt.Sprintf("base: %s — %d branches", ds.Base, len(ds.Chains))
			options[i] = huh.NewOption(label, fmt.Sprintf("%d", i))
		}
		bus.Pause()
		picked := ui.Select("Which stack to fetch?", options)
		bus.Resume()
		if picked == "" {
			bus.Print("Aborted.")
			return nil
		}
		fmt.Sscanf(picked, "%d", &choice)
	}

	selected := discovered[choice]

	// Ensure branches exist locally (fetch if needed)
	bus.Print("\nFetching branches...")
	if err := gitpkg.FetchAll(); err != nil {
		bus.Warnf("fetch failed: %v", err)
	}

	for _, pr := range selected.Chains {
		if !gitpkg.BranchExists(pr.HeadRefName) {
			bus.Printf("  Checking out %s from origin...", pr.HeadRefName)
			if err := gitpkg.CheckoutRemote(pr.HeadRefName); err != nil {
				return fmt.Errorf("cannot checkout %s: %w", pr.HeadRefName, err)
			}
		}
	}

	// Load all local stacks to find a match
	allStacks, _ := stack.LoadAll(root)

	// Try to match against an existing local stack
	matched := matchLocalStack(allStacks, selected)

	if matched != nil {
		// Reconcile existing stack
		return reconcileStack(root, matched, selected, bus)
	}

	// No match — first-time registration
	name := stackName
	if name == "" {
		name = inferStackName(selected.Chains)
	}
	return RegisterStack(root, name, selected)
}

// matchLocalStack finds the local stack with the most branch overlap with
// the discovered chain. Returns nil if no overlap exists.
func matchLocalStack(stacks []*stack.Stack, discovered stack.DiscoveredStack) *stack.Stack {
	discoveredBranches := make(map[string]bool)
	for _, pr := range discovered.Chains {
		discoveredBranches[pr.HeadRefName] = true
	}

	var best *stack.Stack
	bestOverlap := 0

	for _, s := range stacks {
		overlap := 0
		for _, n := range s.Nodes {
			if discoveredBranches[n.Branch] {
				overlap++
			}
		}
		if overlap > bestOverlap {
			best = s
			bestOverlap = overlap
		}
	}

	return best
}

// reconcileStack compares the local stack against the discovered chain,
// prints changes, and applies them.
func reconcileStack(root string, local *stack.Stack, discovered stack.DiscoveredStack, bus *render.Bus) error {
	changes := stack.Reconcile(local, discovered)

	if len(changes) == 0 {
		bus.Printf("\n%s Stack %s is up to date.", ui.SymOK, ui.Bold.Render(local.StackID))
		return nil
	}

	bus.Printf("\nReconciling stack %s:", ui.Bold.Render(local.StackID))
	for _, c := range changes {
		sym := ui.SymOK
		if c.Notable {
			sym = ui.SymWarn
		}
		bus.Printf("  %s %s", sym, c.Detail)
	}

	// Validate new branches for uniqueness
	for _, c := range changes {
		if c.Kind == "append" || c.Kind == "insert" {
			if err := stack.ValidateBranchUniqueness(root, c.Branch); err != nil {
				return fmt.Errorf("cannot reconcile: %w", err)
			}
		}
	}

	stack.ApplyChanges(local, discovered, changes)
	if err := stack.Save(root, local); err != nil {
		return fmt.Errorf("cannot save stack: %w", err)
	}

	bus.Printf("\n%s Stack %s reconciled (%d change(s))", ui.SymOK, ui.Bold.Render(local.StackID), len(changes))
	return nil
}
