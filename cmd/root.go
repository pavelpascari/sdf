package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is set at build time via SetVersion.
var version = "dev"

var rootCmd = &cobra.Command{
	Use:           "sdf",
	Short:         "Stacked Diffs Flow — manage chains of dependent PRs",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// SetVersion sets the version string on the root command.
func SetVersion(v string) {
	version = v
	rootCmd.Version = v
}

// Execute runs the root command. On error it prints the message to stderr
// and exits with code 1.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// RootCmd returns the root cobra.Command (useful for doc generation).
func RootCmd() *cobra.Command {
	return rootCmd
}
