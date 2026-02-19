package cmd

import (
	"flag"
	"fmt"
	"os"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

func RunInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	stackName := fs.String("stack", "", "name for the stack (required)")
	base := fs.String("base", "main", "base branch for the stack")
	fs.Parse(args)

	if *stackName == "" {
		// If no --stack flag, use positional arg
		if fs.NArg() > 0 {
			*stackName = fs.Arg(0)
		} else {
			fmt.Fprintln(os.Stderr, "usage: sdf init --stack <name>")
			fmt.Fprintln(os.Stderr, "       sdf init <name>")
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

	if err := stack.Init(root, *stackName, *base); err != nil {
		return err
	}

	fmt.Printf("Initialized stack %q (base: %s) in %s/.sdf/\n", *stackName, *base, root)
	fmt.Println("Next: run `sdf branch <name>` to create the first branch in the stack.")
	return nil
}
