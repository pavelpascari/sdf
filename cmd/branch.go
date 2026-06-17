package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/spf13/cobra"
)

// NewBranchResult is the structured output of sdf branch when --json is used.
type NewBranchResult struct {
	Branch       string `json:"branch"`
	Stack        string `json:"stack"`
	Parent       string `json:"parent"`
	WorktreePath string `json:"worktree_path,omitempty"`
	Created      bool   `json:"created"`
	ErrorCode    string `json:"error_code,omitempty"`
}

var branchCmd = &cobra.Command{
	Use:         "branch <name>",
	Short:       "Add a new branch to the current stack",
	Annotations: map[string]string{"category": "stack"},
	Args:        cobra.ExactArgs(1),
	RunE:        runBranch,
}

func init() {
	rootCmd.AddCommand(branchCmd)
	branchCmd.Flags().String("stack", "", "stack to add the branch to (default: auto-detect)")
	branchCmd.Flags().Bool("no-prefix", false, "create branch without applying the configured prefix")
	branchCmd.Flags().Bool("json", false, "output result as JSON")
	_ = branchCmd.RegisterFlagCompletionFunc("stack", completeStackNames)
}

// RunBranch is a compatibility wrapper for tests.
func RunBranch(args []string) error {
	rootCmd.SetArgs(append([]string{"branch"}, args...))
	return rootCmd.Execute()
}

func runBranch(cmd *cobra.Command, args []string) error {
	stackFlag, _ := cmd.Flags().GetString("stack")
	noPrefix, _ := cmd.Flags().GetBool("no-prefix")
	jsonFlag, _ := cmd.Flags().GetBool("json")

	branchName := args[0]

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	s, err := resolveStack(root, stackFlag)
	if err != nil {
		return err
	}

	// Load config and apply prefix
	cfg, err := cfgpkg.Load(root)
	if err != nil {
		return fmt.Errorf("cannot load config: %w", err)
	}

	if !noPrefix {
		branchName = cfgpkg.ApplyPrefix(cfg, s.StackID, branchName)
	}

	// Check if branch already exists in this stack — idempotent re-run returns
	// the existing node's state without recreating the branch or worktree.
	if existing := s.FindNode(branchName); existing != nil {
		result := NewBranchResult{
			Branch:       branchName,
			Stack:        s.StackID,
			Parent:       s.ParentBranch(branchName),
			WorktreePath: existing.WorktreePath,
			Created:      false,
		}
		if jsonFlag {
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("cannot marshal result: %w", err)
			}
			fmt.Println(string(data))
			return nil
		}
		fmt.Fprintf(os.Stderr, "branch %q already exists in stack %q — returning current state\n", branchName, s.StackID)
		return nil
	}

	// Check cross-stack uniqueness
	if err := stack.ValidateBranchUniqueness(root, branchName); err != nil {
		return err
	}

	// Detect insertion point from current branch
	currentBranch, _ := gitpkg.CurrentBranch()
	insertAfterIdx := s.NodeIndex(currentBranch)

	// Determine parent based on insertion point.
	// insertAfterIdx is used only to pick the parent here; the actual insertion
	// position is re-derived inside WithLock from the FRESH stack.
	var parent string
	switch {
	case insertAfterIdx >= 0:
		// User is on a stack branch — insert after it
		parent = s.Nodes[insertAfterIdx].Branch
	case len(s.Nodes) > 0:
		// User is on base or unrelated branch — append to end
		parent = s.Nodes[len(s.Nodes)-1].Branch
	default:
		// Empty stack — first branch
		parent = s.Base
	}

	// Resolve parent tip for tracking (works without checking the parent out).
	parentTip, err := gitpkg.RevParse(parent)
	if err != nil {
		return fmt.Errorf("cannot resolve parent branch %s: %w", parent, err)
	}

	newNode := stack.Node{Branch: branchName, Status: "open", BaseTip: parentTip}

	if s.Worktree {
		// Create the branch in its own worktree, branched from the parent.
		// The main repo's checkout is left untouched.
		if err := addWorktreeForNode(cfg, root, &newNode, parent); err != nil {
			return err
		}
	} else {
		if currentBranch != parent {
			if err := gitpkg.Checkout(parent); err != nil {
				return fmt.Errorf("cannot checkout parent %s: %w", parent, err)
			}
		}
		if err := gitpkg.CreateBranch(branchName); err != nil {
			return fmt.Errorf("cannot create branch: %w", err)
		}
	}

	// Insert node into the stack JSON under the advisory lock so concurrent
	// "sdf branch" calls cannot lose each other's writes.
	// newNode (incl. WorktreePath) was built above, outside the lock.
	// Inside the fn we re-find the insertion point in the FRESH stack loaded
	// by WithLock (positions may have shifted under concurrency).
	var insertAt int
	var downstreamPR int
	var downstreamBranch string
	err = stack.WithLock(root, s.StackID, func(ls *stack.Stack) error {
		// Re-find parent in the freshly-loaded stack.
		freshIdx := ls.NodeIndex(parent)
		if freshIdx < 0 {
			insertAt = len(ls.Nodes)
		} else {
			insertAt = freshIdx + 1
		}
		ls.Nodes = slices.Insert(ls.Nodes, insertAt, newNode)
		// Capture downstream PR info while we have the fresh stack.
		if insertAt < len(ls.Nodes)-1 {
			ds := &ls.Nodes[insertAt+1]
			downstreamPR = ds.PR
			downstreamBranch = branchName // new node is now upstream of ds
		}
		return nil
	})
	if err != nil {
		return err
	}

	var rdr render.Renderer
	if jsonFlag {
		rdr = &render.JSONRenderer{}
	}
	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{Renderer: rdr})
	if !jsonFlag {
		defer func() { _ = bus.Finish() }()
	}

	// Push tracking branch to origin
	if err := gitpkg.PushNew(branchName); err != nil {
		bus.Warnf("could not push to origin: %v", err)
		bus.Warnf("  You can push later with: git push -u origin %s", branchName)
	}

	// If there's a downstream node, update its PR base on GitHub
	if downstreamPR > 0 && ghpkg.Available() {
		if err := ghpkg.PREditBase(downstreamPR, downstreamBranch); err != nil {
			bus.Warnf("could not update PR #%d base: %v", downstreamPR, err)
		} else {
			bus.Printf("  Updated PR #%d base → %s", downstreamPR, downstreamBranch)
		}
	}

	if jsonFlag {
		result := NewBranchResult{
			Branch:       branchName,
			Stack:        s.StackID,
			Parent:       parent,
			WorktreePath: newNode.WorktreePath,
			Created:      true,
		}
		_ = bus.Finish()
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("cannot marshal result: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	bus.Printf("Created branch %q in stack %q (based on %s)", branchName, s.StackID, parent)
	if s.Worktree {
		bus.Printf("Worktree: %s", newNode.WorktreePath)
		bus.Printf("  cd %s", newNode.WorktreePath)
	}
	bus.Print("Next: implement your changes, then run `sdf pr`.")
	return nil
}
