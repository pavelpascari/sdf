package cmd

import (
	"flag"
	"fmt"
	"os"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

func RunInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	stackName := fs.String("stack", "", "name for the stack (required)")
	base := fs.String("base", "", "base branch (default: auto-detected from origin HEAD)")
	fs.Parse(args)

	if *stackName == "" {
		// If no --stack flag, use positional arg
		if fs.NArg() > 0 {
			*stackName = fs.Arg(0)
		} else {
			fmt.Fprintln(os.Stderr, "usage: sdf init [--base <branch>] <name>")
			fmt.Fprintln(os.Stderr, "       sdf init --stack <name> [--base <branch>]")
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
	cfgPath := cfgpkg.RepoPath(root)
	if err := cfgpkg.Save(cfgPath, cfgpkg.Defaults()); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create config: %v\n", err)
	}

	fmt.Printf("Initialized stack %q (base: %s) in %s/.sdf/\n", *stackName, baseBranch, root)
	fmt.Println()
	fmt.Println("The stack is rooted at the base branch — all new branches created")
	fmt.Println("with `sdf branch` will chain from it, regardless of which branch")
	fmt.Println("you're currently on.")
	fmt.Println()
	fmt.Println("Next: run `sdf branch <name>` to create the first branch in the stack.")
	return nil
}
