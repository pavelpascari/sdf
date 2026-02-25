package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

// InitResult is the structured output of sdf init when --json is used.
type InitResult struct {
	Stack  string `json:"stack"`
	Base   string `json:"base"`
	Branch string `json:"branch"`
	Pushed bool   `json:"pushed"`
}

var initCmd = &cobra.Command{
	Use:   "init [stack-name]",
	Short: "Create a new stack and its first branch",
	Long: `The stack name can be provided as a positional argument or via the --stack flag.
If --branch is omitted, the first branch is named after the stack. The base branch
is auto-detected from origin HEAD if not specified.`,
	Example: `  sdf init my-feature                                # positional stack name
  sdf init --stack my-feature                        # explicit flag
  sdf init my-feature --branch db-schema             # custom first branch name
  sdf init my-feature --base develop                 # custom base branch
  sdf init my-feature --json                         # machine-readable output`,
	Annotations: map[string]string{"category": "stack"},
	RunE:        runInitCmd,
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().String("stack", "", "name for the stack (alternative to positional arg)")
	initCmd.Flags().String("base", "", "base branch (default: auto-detected from origin HEAD)")
	initCmd.Flags().String("branch", "", "name for the first branch (default: stack name)")
	initCmd.Flags().Bool("json", false, "output machine-readable JSON")
}

func runInitCmd(cmd *cobra.Command, args []string) error {
	stackName, _ := cmd.Flags().GetString("stack")
	base, _ := cmd.Flags().GetString("base")
	branchFlag, _ := cmd.Flags().GetString("branch")
	jsonFlag, _ := cmd.Flags().GetBool("json")

	if stackName == "" && len(args) > 0 {
		stackName = args[0]
	}
	if stackName == "" {
		return fmt.Errorf("stack name required: sdf init <stack-name>")
	}

	_, err := runInitCore(stackName, base, branchFlag, jsonFlag)
	return err
}

// RunInit is a compatibility wrapper for main.go and tests.
func RunInit(args []string) error {
	rootCmd.SetArgs(append([]string{"init"}, args...))
	return rootCmd.Execute()
}

// RunInitWithOutput runs init and returns the JSON output string when --json
// is used, or the empty string for human output.
func RunInitWithOutput(args []string) (string, error) {
	// Parse args manually to extract params, then call core directly
	// so we can capture the string return value.
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	stackFlag := fs.String("stack", "", "")
	base := fs.String("base", "", "")
	branch := fs.String("branch", "", "")
	jsonFlag := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	name := *stackFlag
	if name == "" && fs.NArg() > 0 {
		name = fs.Arg(0)
	}
	if name == "" {
		return "", fmt.Errorf("stack name required: sdf init <stack-name>")
	}
	return runInitCore(name, *base, *branch, *jsonFlag)
}

// runInitCore is the shared implementation used by both Cobra and compat wrappers.
func runInitCore(stackName, base, branchFlag string, jsonFlag bool) (string, error) {
	root, err := gitpkg.RepoRoot()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository: %w", err)
	}

	// Migrate legacy layout if needed
	stack.MigrateIfNeeded(root)

	// Check if a stack with this name already exists
	if _, err := stack.LoadStack(root, stackName); err == nil {
		return "", fmt.Errorf("stack %q already exists in %s", stackName, root)
	}

	// Resolve the base branch
	baseBranch := base
	if baseBranch == "" {
		detected, err := gitpkg.DefaultBranch()
		if err != nil {
			return "", fmt.Errorf("cannot auto-detect default branch: %w\nSpecify one explicitly with --base <branch>", err)
		}
		baseBranch = detected
	}

	// Validate the base branch exists
	if !gitpkg.BranchExists(baseBranch) && !gitpkg.BranchExists("origin/"+baseBranch) {
		return "", fmt.Errorf("base branch %q does not exist — check the name or use --base <branch>", baseBranch)
	}

	if err := stack.Init(root, stackName, baseBranch); err != nil {
		return "", err
	}

	// Create default config file only if one doesn't already exist,
	// so we never overwrite user-customized settings.
	cfgPath := cfgpkg.RepoPath(root)
	if _, statErr := os.Stat(cfgPath); os.IsNotExist(statErr) {
		if err := cfgpkg.Save(cfgPath, cfgpkg.Defaults()); err != nil {
			if !jsonFlag {
				fmt.Fprintf(os.Stderr, "warning: could not create config: %v\n", err)
			}
		}
	}

	// Load the config (merges global + repo, applies defaults for unset fields)
	cfg, err := cfgpkg.Load(root)
	if err != nil {
		cfg = cfgpkg.Defaults()
	}

	// Determine first branch name
	branchName := stackName
	if branchFlag != "" {
		branchName = branchFlag
	}
	branchName = cfgpkg.ApplyPrefix(cfg, stackName, branchName)

	// Get the current tip of the base for tracking
	baseTip, err := gitpkg.RevParse(baseBranch)
	if err != nil {
		return "", fmt.Errorf("cannot resolve base branch %s: %w", baseBranch, err)
	}

	// Create the git branch
	if err := gitpkg.CreateBranch(branchName); err != nil {
		return "", fmt.Errorf("cannot create branch: %w", err)
	}

	// Load the stack and add the node
	s, err := stack.LoadStack(root, stackName)
	if err != nil {
		return "", fmt.Errorf("cannot load stack after init: %w", err)
	}
	s.Nodes = append(s.Nodes, stack.Node{
		Branch:  branchName,
		Status:  "open",
		BaseTip: baseTip,
	})
	if err := stack.Save(root, s); err != nil {
		return "", err
	}

	// Push tracking branch to origin
	pushed := true
	if err := gitpkg.PushNew(branchName); err != nil {
		pushed = false
		if !jsonFlag {
			fmt.Fprintf(os.Stderr, "warning: could not push to origin: %v\n", err)
			fmt.Fprintln(os.Stderr, "  You can push later with: git push -u origin", branchName)
		}
	}

	if jsonFlag {
		result := InitResult{
			Stack:  stackName,
			Base:   baseBranch,
			Branch: branchName,
			Pushed: pushed,
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "", fmt.Errorf("cannot marshal result: %w", err)
		}
		output := string(data)
		fmt.Println(output)
		return output, nil
	}

	fmt.Printf("Initialized stack %q (base: %s)\n", stackName, baseBranch)
	fmt.Printf("Created branch %q (based on %s)\n", branchName, baseBranch)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  sdf pr               Create a pull request")
	fmt.Println("  sdf branch <name>    Add another branch to the stack")
	fmt.Println("  sdf status           View stack topology")

	return "", nil
}
