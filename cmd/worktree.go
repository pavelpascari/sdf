// cmd/worktree.go
package cmd

import (
	"fmt"
	"os"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/spf13/cobra"
)

var worktreeCmd = &cobra.Command{
	Use:         "worktree",
	Short:       "Manage worktree mode for a stack",
	Annotations: map[string]string{"category": "stack"},
}

var worktreeEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable worktree mode and materialize worktrees for open branches",
	RunE:  runWorktreeEnable,
}

func init() {
	rootCmd.AddCommand(worktreeCmd)
	worktreeCmd.AddCommand(worktreeEnableCmd)
	worktreeEnableCmd.Flags().String("stack", "", "stack to enable (default: auto-detect)")
	_ = worktreeEnableCmd.RegisterFlagCompletionFunc("stack", completeStackNames)
}

// RunWorktree is a compatibility wrapper for tests.
func RunWorktree(args []string) error {
	rootCmd.SetArgs(append([]string{"worktree"}, args...))
	return rootCmd.Execute()
}

func runWorktreeEnable(cmd *cobra.Command, args []string) error {
	stackFlag, _ := cmd.Flags().GetString("stack")

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}
	s, err := resolveStack(root, stackFlag)
	if err != nil {
		return err
	}
	cfg, err := cfgpkg.Load(root)
	if err != nil {
		cfg = cfgpkg.Defaults()
	}

	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{})
	defer func() { _ = bus.Finish() }()

	// Free all stack branches from the main repo so they can be checked out
	// in worktrees. Do this before acquiring the lock (it's a git op, not a
	// stack mutation).
	if cur, _ := gitpkg.CurrentBranch(); s.FindNode(cur) != nil {
		if err := gitpkg.Checkout(s.Base); err != nil {
			return fmt.Errorf("cannot switch main repo to base %s: %w", s.Base, err)
		}
	}

	// All mutations run under the lock. We persist after each successful
	// worktree add so that a mid-loop failure never leaves orphaned worktrees
	// (worktrees on disk with no recorded WorktreePath).
	var enableErr error
	err = stack.WithLock(root, s.StackID, func(fresh *stack.Stack) error {
		fresh.Worktree = true
		// Persist the Worktree=true flag immediately so it survives even if the
		// first worktree add fails.
		if err := stack.Save(root, fresh); err != nil {
			return err
		}

		existing := existingWorktreePaths()

		for i := range fresh.Nodes {
			node := &fresh.Nodes[i]
			if node.Status == "merged" || node.Status == "closed" {
				continue
			}
			wantPath := cfg.WorktreePathFor(root, node.Branch)
			if existing[wantPath] || node.WorktreePath == wantPath {
				node.WorktreePath = wantPath // already materialized — idempotent
				continue
			}
			// Existing branch → check out into a worktree (createFrom == "").
			if addErr := addWorktreeForNode(cfg, root, node, ""); addErr != nil {
				// The worktrees created so far are already saved. Return an
				// informative error naming the conflicting branch.
				enableErr = fmt.Errorf(
					"cannot add worktree for branch %q: %w\n"+
						"(branch may be checked out elsewhere — run `sdf doctor` or free the branch)",
					node.Branch, addErr,
				)
				return enableErr
			}
			bus.Printf("  %s → %s", node.Branch, node.WorktreePath)
			// Persist this node's WorktreePath immediately so it's recorded even
			// if a later node fails.
			if err := stack.Save(root, fresh); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	bus.Printf("Worktree mode enabled for stack %q", s.StackID)
	return nil
}

// existingWorktreePaths returns the set of worktree paths git already knows about.
func existingWorktreePaths() map[string]bool {
	set := map[string]bool{}
	list, err := gitpkg.WorktreeList()
	if err != nil {
		return set
	}
	for _, w := range list {
		set[w.Path] = true
	}
	return set
}
