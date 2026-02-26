package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

// ── Completion helper functions ───────────────────────────────────

// completeStackNames returns stack names for shell completion.
func completeStackNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
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

// completeStackBranches returns branch names from all sdf stacks.
func completeStackBranches(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	root, err := stack.FindRoot()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	stacks, err := stack.LoadAll(root)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var branches []string
	for _, s := range stacks {
		for _, n := range s.Nodes {
			branches = append(branches, n.Branch+"\t"+s.StackID)
		}
	}
	return branches, cobra.ShellCompDirectiveNoFileComp
}

// completeAllBranches returns all local git branch names.
func completeAllBranches(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	branches, err := gitpkg.ListBranches()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return branches, cobra.ShellCompDirectiveNoFileComp
}

// completeConfigSetArgs completes positional arguments for `sdf config set`.
func completeConfigSetArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		// First arg: config key
		keys := cfgpkg.ConfigKeys()
		var names []string
		for _, k := range keys {
			names = append(names, k.Key+"\t"+k.Description)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	case 1:
		// Second arg: value — suggest based on key type
		key := args[0]
		for _, k := range cfgpkg.ConfigKeys() {
			if k.Key == key && k.Type == "bool" {
				return []string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp
			}
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeMergeMethods returns valid merge methods.
func completeMergeMethods(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"squash\tSquash and merge (default)",
		"merge\tCreate a merge commit",
		"rebase\tRebase and merge",
	}, cobra.ShellCompDirectiveNoFileComp
}

// ── Completion command ────────────────────────────────────────────

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for sdf.

Completions are auto-installed on first run, but you can use this
command to generate scripts manually or reinstall them.`,
	Example: `  sdf completion bash               # output bash completion script
  sdf completion zsh                # output zsh completion script
  sdf completion fish               # output fish completion script
  sdf completion install            # auto-detect shell and install`,
	Annotations: map[string]string{"category": "utility"},
	Args:        cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return []string{"bash", "zsh", "fish", "install"}, cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: runCompletion,
}

func init() {
	rootCmd.AddCommand(completionCmd)
}

func runCompletion(cmd *cobra.Command, args []string) error {
	switch args[0] {
	case "bash":
		return rootCmd.GenBashCompletionV2(os.Stdout, true)
	case "zsh":
		return rootCmd.GenZshCompletion(os.Stdout)
	case "fish":
		return rootCmd.GenFishCompletion(os.Stdout, true)
	case "install":
		return runCompletionInstall()
	default:
		return fmt.Errorf("unsupported shell %q — use bash, zsh, fish, or install", args[0])
	}
}

func runCompletionInstall() error {
	shell := detectShell()
	if shell == "" {
		return fmt.Errorf("cannot detect shell from $SHELL — specify one: sdf completion bash | zsh | fish")
	}

	path, err := installCompletionForShell(shell)
	if err != nil {
		return fmt.Errorf("cannot install %s completions: %w", shell, err)
	}

	// Write marker so auto-install doesn't redo it
	writeCompletionMarker()

	fmt.Printf("Installed %s completions to %s\n", shell, path)
	if shell == "zsh" {
		fmt.Println("Restart your terminal or run: exec zsh")
	} else {
		fmt.Println("Restart your terminal to enable tab completion.")
	}
	return nil
}

// ── Auto-install logic ────────────────────────────────────────────

// completionMarkerVersion is bumped when the completion script format changes
// or when new commands/flags are added that affect completion.
const completionMarkerVersion = "1"

// autoInstallCompletions detects the user's shell and installs completion
// scripts to the standard auto-load directory. Runs only once per marker version.
func autoInstallCompletions() {
	marker := completionMarkerPath()
	if marker == "" {
		return
	}

	// Fast path: already installed for this version
	if data, err := os.ReadFile(marker); err == nil {
		if strings.TrimSpace(string(data)) == completionMarkerVersion {
			return
		}
	}

	shell := detectShell()
	if shell == "" {
		return
	}

	path, err := installCompletionForShell(shell)
	if err != nil {
		return // silently fail — user can run `sdf completion install` manually
	}

	writeCompletionMarker()
	fmt.Fprintf(os.Stderr, "sdf: shell completions installed for %s (%s)\n", shell, path)
	fmt.Fprintf(os.Stderr, "sdf: restart your terminal to enable tab completion\n")
}

func completionMarkerPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "sdf", "completion-installed")
}

func writeCompletionMarker() {
	path := completionMarkerPath()
	if path == "" {
		return
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(completionMarkerVersion), 0644)
}

func detectShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return ""
	}
	base := filepath.Base(shell)
	switch {
	case strings.Contains(base, "zsh"):
		return "zsh"
	case strings.Contains(base, "bash"):
		return "bash"
	case strings.Contains(base, "fish"):
		return "fish"
	}
	return ""
}

// installCompletionForShell generates and writes the completion script for the
// given shell to the appropriate auto-load directory. Returns the path written.
func installCompletionForShell(shell string) (string, error) {
	switch shell {
	case "bash":
		return installBashCompletion()
	case "zsh":
		return installZshCompletion()
	case "fish":
		return installFishCompletion()
	default:
		return "", fmt.Errorf("unsupported shell: %s", shell)
	}
}

func installBashCompletion() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// Use the XDG-standard user completion directory (auto-loaded by bash-completion 2.x)
	dir := filepath.Join(home, ".local", "share", "bash-completion", "completions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := rootCmd.GenBashCompletionV2(&buf, true); err != nil {
		return "", err
	}

	path := filepath.Join(dir, "sdf")
	return path, os.WriteFile(path, buf.Bytes(), 0644)
}

func installZshCompletion() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// Write completion function to ~/.zfunc/_sdf
	dir := filepath.Join(home, ".zfunc")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := rootCmd.GenZshCompletion(&buf); err != nil {
		return "", err
	}

	path := filepath.Join(dir, "_sdf")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return "", err
	}

	// Ensure ~/.zfunc is in fpath via .zshrc
	ensureZshFpath(home)

	return path, nil
}

// ensureZshFpath adds ~/.zfunc to fpath in .zshrc if not already present.
func ensureZshFpath(home string) {
	zshrc := filepath.Join(home, ".zshrc")
	data, _ := os.ReadFile(zshrc)
	content := string(data)

	// Check if .zfunc is already referenced in fpath
	if strings.Contains(content, ".zfunc") {
		return
	}

	snippet := "\n# sdf shell completions\nfpath=(~/.zfunc $fpath)\nautoload -Uz compinit && compinit\n"
	f, err := os.OpenFile(zshrc, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(snippet)
}

func installFishCompletion() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// Fish auto-loads completions from this directory
	dir := filepath.Join(home, ".config", "fish", "completions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := rootCmd.GenFishCompletion(&buf, true); err != nil {
		return "", err
	}

	path := filepath.Join(dir, "sdf.fish")
	return path, os.WriteFile(path, buf.Bytes(), 0644)
}

// isCompletionRequest returns true if the current invocation is a shell
// completion callback (e.g., the shell calling `sdf __complete ...`).
func isCompletionRequest() bool {
	for _, arg := range os.Args[1:] {
		if arg == "__complete" || arg == "__completeNoDesc" {
			return true
		}
	}
	return false
}
