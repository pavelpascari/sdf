package cmd

import (
	"fmt"
	"os"

	verpkg "github.com/pavelpascari/sdf/internal/version"
	"github.com/spf13/cobra"
)

// version is set at build time via SetVersion.
var version = "dev"

var rootCmd = &cobra.Command{
	Use:           "sdf",
	Short:         "Stacked Diffs Flow — manage chains of dependent PRs",
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.ArbitraryArgs,
	// Suggest stack branch names for `sdf <branch>` tab-completion
	ValidArgsFunction: completeStackBranches,
	// Handle `sdf <branch>` as shorthand for `sdf switch <branch>`
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			if err := TrySwitch(args[0]); err == nil {
				return nil
			}
		}
		return cmd.Help()
	},
}

var versionCmd = &cobra.Command{
	Use:         "version",
	Short:       "Print version",
	Annotations: map[string]string{"category": "utility"},
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("sdf %s\n", version)
		verpkg.Check(version)
	},
}

func init() {
	// Hide Cobra's default completion command — we provide our own
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.AddCommand(versionCmd)
}

// SetVersion sets the version string on the root command.
func SetVersion(v string) {
	version = v
	rootCmd.Version = v
}

// Execute runs the root command. On error it prints the message to stderr
// and exits with code 1.
func Execute() {
	// Auto-install shell completions on first run (skip during completion callbacks)
	if !isCompletionRequest() {
		autoInstallCompletions()
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// RootCmd returns the root cobra.Command (useful for doc generation).
func RootCmd() *cobra.Command {
	return rootCmd
}
