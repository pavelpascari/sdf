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

func RunInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	stackName := fs.String("stack", "", "name for the stack (required)")
	base := fs.String("base", "", "base branch (default: auto-detected from origin HEAD)")
	branchFlag := fs.String("branch", "", "name for the first branch (default: stack name)")
	fs.Parse(args)

	if *stackName == "" {
		// If no --stack flag, use positional arg
		if fs.NArg() > 0 {
			*stackName = fs.Arg(0)
		} else {
			fmt.Fprintln(os.Stderr, "usage: sdf init [--base <branch>] [--branch <name>] <stack-name>")
			fmt.Fprintln(os.Stderr, "       sdf init --stack <name> [--base <branch>] [--branch <name>]")
			os.Exit(1)
		}
	}

	root, err := gitpkg.RepoRoot()
	if err != nil {
		return fmt.Errorf("not inside a git repository: %w", err)
	}

	// Migrate legacy layout if needed
	stack.MigrateIfNeeded(root)

	// Check if a stack with this name already exists
	if _, err := stack.LoadStack(root, *stackName); err == nil {
		return fmt.Errorf("stack %q already exists in %s", *stackName, root)
	}

	// Resolve the base branch
	baseBranch := *base
	if baseBranch == "" {
		detected, err := gitpkg.DefaultBranch()
		if err != nil {
			return fmt.Errorf("cannot auto-detect default branch: %w\nSpecify one explicitly with --base <branch>", err)
		}
		baseBranch = detected
	}

	// Validate the base branch exists
	if !gitpkg.BranchExists(baseBranch) && !gitpkg.BranchExists("origin/"+baseBranch) {
		return fmt.Errorf("base branch %q does not exist — check the name or use --base <branch>", baseBranch)
	}

	if err := stack.Init(root, *stackName, baseBranch); err != nil {
		return err
	}

	// Create default config file so it's discoverable
	cfg := cfgpkg.Defaults()
	cfgPath := cfgpkg.RepoPath(root)
	if err := cfgpkg.Save(cfgPath, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create config: %v\n", err)
	}

	// Load the config (may already exist with custom settings)
	cfg, err = cfgpkg.Load(root)
	if err != nil {
		cfg = cfgpkg.Defaults()
	}

	// Determine first branch name
	branchName := *stackName
	if *branchFlag != "" {
		branchName = *branchFlag
	}
	branchName = cfgpkg.ApplyPrefix(cfg, *stackName, branchName)

	// Get the current tip of the base for tracking
	baseTip, err := gitpkg.RevParse(baseBranch)
	if err != nil {
		return fmt.Errorf("cannot resolve base branch %s: %w", baseBranch, err)
	}

	// Create the git branch
	if err := gitpkg.CreateBranch(branchName); err != nil {
		return fmt.Errorf("cannot create branch: %w", err)
	}

	// Load the stack and add the node
	s, err := stack.LoadStack(root, *stackName)
	if err != nil {
		return fmt.Errorf("cannot load stack after init: %w", err)
	}
	s.Nodes = append(s.Nodes, stack.Node{
		Branch:  branchName,
		Status:  "open",
		BaseTip: baseTip,
	})
	if err := stack.Save(root, s); err != nil {
		return err
	}

	// Create stub context document
	if err := ctxpkg.CreateStub(root, branchName, baseBranch); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create context stub: %v\n", err)
	}

	// Push tracking branch to origin
	pushed := true
	if err := gitpkg.PushNew(branchName); err != nil {
		pushed = false
		fmt.Fprintf(os.Stderr, "warning: could not push to origin: %v\n", err)
		fmt.Fprintln(os.Stderr, "  You can push later with: git push -u origin", branchName)
	}
	_ = pushed // used by --json in a future branch

	fmt.Printf("Initialized stack %q (base: %s)\n", *stackName, baseBranch)
	fmt.Printf("Created branch %q (based on %s)\n", branchName, baseBranch)
	fmt.Printf("Context doc: .sdf/context/%s.md\n", branchName)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  sdf context edit     Edit the context doc for this branch")
	fmt.Println("  sdf pr               Create a pull request")
	fmt.Println("  sdf branch <name>    Add another branch to the stack")
	fmt.Println("  sdf status           View stack topology")

	return nil
}
