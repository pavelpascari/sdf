package cmd

import (
	"flag"
	"fmt"
	"os"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	ctxpkg "github.com/pavelpascari/sdf/internal/context"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

func RunBranch(args []string) error {
	fs := flag.NewFlagSet("branch", flag.ExitOnError)
	stackFlag := fs.String("stack", "", "stack to add the branch to (default: auto-detect)")
	noPrefix := fs.Bool("no-prefix", false, "create branch without applying the configured prefix")
	fs.Parse(args)

	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: sdf branch [--stack <name>] [--no-prefix] <branch>")
		os.Exit(1)
	}

	branchName := fs.Arg(0)

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	s, err := resolveStack(root, *stackFlag)
	if err != nil {
		return err
	}

	// Load config and apply prefix
	cfg, err := cfgpkg.Load(root)
	if err != nil {
		return fmt.Errorf("cannot load config: %w", err)
	}

	if !*noPrefix {
		branchName = cfgpkg.ApplyPrefix(cfg, s.StackID, branchName)
	}

	// Check if branch already exists in the stack
	if s.FindNode(branchName) != nil {
		return fmt.Errorf("branch %q already exists in stack %q", branchName, s.StackID)
	}

	// Determine parent: the last node in the stack, or the base
	parent := s.Base
	if len(s.Nodes) > 0 {
		parent = s.Nodes[len(s.Nodes)-1].Branch
	}

	// Get the current tip of the parent for tracking
	parentTip, err := gitpkg.RevParse(parent)
	if err != nil {
		return fmt.Errorf("cannot resolve parent branch %s: %w", parent, err)
	}

	// Create the git branch
	if err := gitpkg.CreateBranch(branchName); err != nil {
		return fmt.Errorf("cannot create branch: %w", err)
	}

	// Add node to stack
	s.Nodes = append(s.Nodes, stack.Node{
		Branch:  branchName,
		Status:  "open",
		BaseTip: parentTip,
	})

	if err := stack.Save(root, s); err != nil {
		return err
	}

	// Create stub context document
	if err := ctxpkg.CreateStub(root, branchName, parent); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create context stub: %v\n", err)
	}

	// Push tracking branch to origin
	if err := gitpkg.PushNew(branchName); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not push to origin: %v\n", err)
		fmt.Fprintln(os.Stderr, "  You can push later with: git push -u origin", branchName)
	}

	fmt.Printf("Created branch %q in stack %q (based on %s)\n", branchName, s.StackID, parent)
	fmt.Printf("Context doc: .sdf/context/%s.md\n", branchName)
	fmt.Println("Next: implement your changes, then run `sdf context edit` and `sdf pr`.")
	return nil
}
