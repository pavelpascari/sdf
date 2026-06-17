package cmd

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

// NewResult is the structured output of sdf new when --json is used.
type NewResult struct {
	Stack        string `json:"stack"`
	Base         string `json:"base"`
	Branch       string `json:"branch"`
	WorktreePath string `json:"worktree_path,omitempty"`
	Pushed       bool   `json:"pushed"`
	Created      bool   `json:"created"`
	ErrorCode    string `json:"error_code,omitempty"`
}

var newCmd = &cobra.Command{
	Use:   "new [stack-name]",
	Short: "Create a new stack and its first branch",
	Long: `The stack name can be provided as a positional argument or via the --stack flag.
If --branch is omitted, the first branch is named after the stack. The base branch
is auto-detected from origin HEAD if not specified.`,
	Example: `  sdf new my-feature                                # positional stack name
  sdf new --stack my-feature                        # explicit flag
  sdf new my-feature --branch db-schema             # custom first branch name
  sdf new my-feature --base develop                 # custom base branch
  sdf new my-feature --json                         # machine-readable output`,
	Annotations: map[string]string{"category": "stack"},
	RunE:        runNewCmd,
}

func init() {
	rootCmd.AddCommand(newCmd)
	newCmd.Flags().String("stack", "", "name for the stack (alternative to positional arg)")
	newCmd.Flags().String("base", "", "base branch (default: auto-detected from origin HEAD)")
	newCmd.Flags().String("branch", "", "name for the first branch (default: stack name)")
	newCmd.Flags().Bool("json", false, "output machine-readable JSON")
	newCmd.Flags().Bool("worktrees", false, "create each branch as a git worktree (worktree mode)")
	_ = newCmd.RegisterFlagCompletionFunc("base", completeGitBranches)
}

func runNewCmd(cmd *cobra.Command, args []string) error {
	stackName, _ := cmd.Flags().GetString("stack")
	base, _ := cmd.Flags().GetString("base")
	branchFlag, _ := cmd.Flags().GetString("branch")
	jsonFlag, _ := cmd.Flags().GetBool("json")
	worktree, _ := cmd.Flags().GetBool("worktrees")

	if stackName == "" && len(args) > 0 {
		stackName = args[0]
	}
	if stackName == "" {
		return fmt.Errorf("stack name required: sdf new <stack-name>")
	}

	_, err := runNewCore(stackName, base, branchFlag, jsonFlag, worktree)
	return err
}

// RunNew is a compatibility wrapper for main.go and tests.
func RunNew(args []string) error {
	rootCmd.SetArgs(append([]string{"new"}, args...))
	return rootCmd.Execute()
}

// RunNewWithOutput runs new and returns the JSON output string when --json
// is used, or the empty string for human output.
func RunNewWithOutput(args []string) (string, error) {
	// Parse args manually to extract params, then call core directly
	// so we can capture the string return value.
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	stackFlag := fs.String("stack", "", "")
	base := fs.String("base", "", "")
	branch := fs.String("branch", "", "")
	jsonFlag := fs.Bool("json", false, "")
	worktree := fs.Bool("worktrees", false, "")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	name := *stackFlag
	if name == "" && fs.NArg() > 0 {
		name = fs.Arg(0)
	}
	if name == "" {
		return "", fmt.Errorf("stack name required: sdf new <stack-name>")
	}
	return runNewCore(name, *base, *branch, *jsonFlag, *worktree)
}

// runNewCore is the shared implementation used by both Cobra and compat wrappers.
func runNewCore(stackName, base, branchFlag string, jsonFlag, worktree bool) (string, error) {
	root, err := gitpkg.RepoRoot()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository: %w", err)
	}

	// Migrate legacy layout if needed
	stack.MigrateIfNeeded(root)

	// Resolve the base branch before acquiring the lock (no I/O risk here).
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

	// Ensure .sdf/ exists so AcquireLock can create its lock file.
	// This is idempotent — MkdirAll succeeds if the directory already exists.
	if err := os.MkdirAll(filepath.Join(root, stack.SDFDir), 0755); err != nil {
		return "", fmt.Errorf("cannot create .sdf directory: %w", err)
	}

	// Existence check + Init are performed atomically under the stack advisory
	// lock so two concurrent "sdf new <same-stack>" invocations cannot both see
	// "not found" and double-create. AcquireLock is used directly because
	// WithLock calls LoadStack, which fails for stacks that don't exist yet.
	//
	// errStackExists is a local sentinel: the winner returns false/created,
	// the loser (or a sequential re-run) returns the idempotent state.
	errStackExists := errors.New("stack already exists")
	var existingStack *stack.Stack
	lockErr := func() error {
		lock, err := stack.AcquireLock(root, stackName, stack.LockTimeout)
		if err != nil {
			return err
		}
		defer func() { _ = lock.Release() }()

		// In-lock existence re-check.
		if s, loadErr := stack.LoadStack(root, stackName); loadErr == nil {
			existingStack = s
			return errStackExists
		}
		return stack.Init(root, stackName, baseBranch)
	}()
	if errors.Is(lockErr, errStackExists) {
		// Stack already exists (race winner or sequential re-run) — return idempotently.
		var firstNode stack.Node
		if len(existingStack.Nodes) > 0 {
			firstNode = existingStack.Nodes[0]
		}
		if jsonFlag {
			result := NewResult{
				Stack:        stackName,
				Base:         existingStack.Base,
				Branch:       firstNode.Branch,
				WorktreePath: firstNode.WorktreePath,
				Pushed:       false,
				Created:      false,
			}
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return "", fmt.Errorf("cannot marshal result: %w", err)
			}
			output := string(data)
			fmt.Println(output)
			return output, nil
		}
		fmt.Fprintf(os.Stderr, "note: stack %q already exists — returning current state\n", stackName)
		return "", nil
	}
	if lockErr != nil {
		return "", lockErr
	}

	// Mark worktree mode on the freshly-created stack under the advisory lock
	// so the read-modify-write is safe against concurrent access.
	if worktree {
		if err := stack.WithLock(root, stackName, func(ws *stack.Stack) error {
			ws.Worktree = true
			return nil
		}); err != nil {
			return "", fmt.Errorf("cannot enable worktree mode: %w", err)
		}
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

	// Build the node (and perform git-side effects) BEFORE acquiring the lock.
	// Only the JSON read-modify-write goes inside WithLock.
	node := stack.Node{Branch: branchName, Status: "open", BaseTip: baseTip}
	if worktree {
		// First branch lives in a worktree; the main repo stays on the base.
		if err := addWorktreeForNode(cfg, root, &node, baseBranch); err != nil {
			return "", err
		}
	} else {
		if err := gitpkg.CreateBranch(branchName); err != nil {
			return "", fmt.Errorf("cannot create branch: %w", err)
		}
	}

	// Append the node to the stack JSON under the advisory lock.
	if err := stack.WithLock(root, stackName, func(s *stack.Stack) error {
		s.Nodes = append(s.Nodes, node)
		return nil
	}); err != nil {
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
		result := NewResult{
			Stack:        stackName,
			Base:         baseBranch,
			Branch:       branchName,
			WorktreePath: node.WorktreePath,
			Pushed:       pushed,
			Created:      true,
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
