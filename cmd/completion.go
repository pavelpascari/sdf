package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
)

var completionCmd = &cobra.Command{
	Use:   "completion <bash|zsh|fish|powershell>",
	Short: "Generate or install shell completion scripts",
	Long: `Generate shell completion scripts for sdf.

To load completions manually:

  Bash:
    $ sdf completion bash > ~/.local/share/bash-completion/completions/sdf

  Zsh:
    $ sdf completion zsh > "${fpath[1]}/_sdf"

  Fish:
    $ sdf completion fish > ~/.config/fish/completions/sdf.fish

  PowerShell:
    PS> sdf completion powershell | Out-String | Invoke-Expression

Or use 'sdf completion install' to auto-detect your shell and install.`,
	Annotations:           map[string]string{"category": "utility"},
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletionV2(os.Stdout, true)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		default:
			return fmt.Errorf("unsupported shell: %s", args[0])
		}
	},
}

var completionInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Auto-detect shell and install completion scripts",
	Long: `Detects your current shell from $SHELL and writes completions to
the standard location so they load automatically in new sessions.

Supported shells: bash, zsh, fish.`,
	RunE: runCompletionInstall,
}

func init() {
	rootCmd.AddCommand(completionCmd)
	completionCmd.AddCommand(completionInstallCmd)
}

func runCompletionInstall(cmd *cobra.Command, args []string) error {
	shell := detectShell()
	if shell == "" {
		return fmt.Errorf("could not detect your shell from $SHELL\n\n  Generate manually:\n    sdf completion bash\n    sdf completion zsh\n    sdf completion fish")
	}

	switch shell {
	case "bash":
		return installBashCompletion()
	case "zsh":
		return installZshCompletion()
	case "fish":
		return installFishCompletion()
	default:
		return fmt.Errorf("auto-install is not supported for %s\n\n  Generate manually: sdf completion %s > <path>", shell, shell)
	}
}

func detectShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return ""
	}
	base := filepath.Base(shell)
	switch {
	case strings.Contains(base, "bash"):
		return "bash"
	case strings.Contains(base, "zsh"):
		return "zsh"
	case strings.Contains(base, "fish"):
		return "fish"
	default:
		return base
	}
}

func installBashCompletion() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	dir := filepath.Join(home, ".local", "share", "bash-completion", "completions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}

	path := filepath.Join(dir, "sdf")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	defer f.Close()

	if err := rootCmd.GenBashCompletionV2(f, true); err != nil {
		return err
	}

	fmt.Printf("%s Bash completions installed to %s\n", ui.SymOK, path)
	fmt.Println("  Restart your shell or run: source", path)
	return nil
}

func installZshCompletion() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	dir := filepath.Join(home, ".zsh", "completions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}

	path := filepath.Join(dir, "_sdf")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	defer f.Close()

	if err := rootCmd.GenZshCompletion(f); err != nil {
		return err
	}

	fmt.Printf("%s Zsh completions installed to %s\n", ui.SymOK, path)

	// Check if fpath already includes our directory in .zshrc
	zshrc := filepath.Join(home, ".zshrc")
	if data, readErr := os.ReadFile(zshrc); readErr == nil {
		if strings.Contains(string(data), dir) {
			fmt.Println("  Restart your shell to activate.")
			return nil
		}
	}

	// Append fpath setup to .zshrc
	snippet := fmt.Sprintf("\n# sdf shell completions\nfpath=(%s $fpath)\nautoload -Uz compinit && compinit\n", dir)
	rc, err := os.OpenFile(zshrc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("  Add this to your .zshrc manually:\n    fpath=(%s $fpath)\n    autoload -Uz compinit && compinit\n", dir)
		return nil
	}
	defer rc.Close()
	if _, err := fmt.Fprint(rc, snippet); err != nil {
		return fmt.Errorf("cannot write to %s: %w", zshrc, err)
	}

	fmt.Printf("%s Updated %s with fpath entry\n", ui.SymOK, zshrc)
	fmt.Println("  Restart your shell to activate.")
	return nil
}

func installFishCompletion() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	dir := filepath.Join(home, ".config", "fish", "completions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}

	path := filepath.Join(dir, "sdf.fish")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	defer f.Close()

	if err := rootCmd.GenFishCompletion(f, true); err != nil {
		return err
	}

	fmt.Printf("%s Fish completions installed to %s\n", ui.SymOK, path)
	fmt.Println("  Completions will load automatically in new shells.")
	return nil
}

// --- Dynamic completion functions ---
// These are used by ValidArgsFunction and RegisterFlagCompletionFunc
// to provide context-aware completions at runtime.

// completeStackNames returns all known stack names for shell completion.
func completeStackNames(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	root, err := stack.FindRoot()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names, err := stack.ListStacks(root)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeStackBranches returns all branch names across all stacks.
// Used for `sdf switch <TAB>` and the `sdf <branch>` shorthand.
func completeStackBranches(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	root, err := stack.FindRoot()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	stacks, err := stack.LoadAll(root)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	localBranches, err := gitpkg.LocalBranches()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	localSet := make(map[string]bool, len(localBranches))
	for _, b := range localBranches {
		localSet[b] = true
	}

	var branches []string
	for _, s := range stacks {
		for _, n := range s.Nodes {
			if n.Status != "merged" && localSet[n.Branch] {
				branches = append(branches, n.Branch)
			}
		}
	}
	return branches, cobra.ShellCompDirectiveNoFileComp
}

// completeGitBranches returns local git branch names for completion.
// Used for --base and --from flags.
func completeGitBranches(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	branches, err := gitpkg.LocalBranches()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return branches, cobra.ShellCompDirectiveNoFileComp
}

// configKeys is the list of valid configuration keys.
var configKeys = []string{
	"branch_prefix.enabled",
	"branch_prefix.scope",
	"branch_prefix.separator",
	"pr_title.conventional_commits",
	"pr_title.ticket_pattern",
	"sync.with_content",
}

// completeConfigKeys returns valid config keys for `sdf config set`.
func completeConfigKeys(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) >= 1 {
		// Second arg is the value — suggest based on key type
		return completeConfigValues(args[0]), cobra.ShellCompDirectiveNoFileComp
	}
	return configKeys, cobra.ShellCompDirectiveNoFileComp
}

// completeConfigValues returns suggested values for a config key.
func completeConfigValues(key string) []string {
	switch key {
	case "branch_prefix.enabled", "pr_title.conventional_commits", "sync.with_content":
		return []string{"true", "false"}
	case "branch_prefix.separator":
		return []string{"/", "-"}
	default:
		return nil
	}
}

// mergeMethods is the list of valid merge methods.
var mergeMethods = []string{"squash", "merge", "rebase"}

// completeMergeMethods returns valid merge methods for --method flag.
func completeMergeMethods(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return mergeMethods, cobra.ShellCompDirectiveNoFileComp
}
