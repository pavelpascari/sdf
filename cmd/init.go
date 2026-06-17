package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// initCmd is a backward-compatible alias for "sdf new".
var initCmd = &cobra.Command{
	Use:         "init [stack-name]",
	Short:       "Create a new stack and its first branch (use \"sdf new\" instead)",
	Hidden:      true,
	Deprecated:  "use `sdf new` instead",
	Annotations: map[string]string{"category": "stack"},
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(os.Stderr, "note: `sdf init` has been renamed to `sdf new` — please use `sdf new` going forward")

		// Forward all flags and args to the new command
		stackFlag, _ := cmd.Flags().GetString("stack")
		base, _ := cmd.Flags().GetString("base")
		branchFlag, _ := cmd.Flags().GetString("branch")
		jsonFlag, _ := cmd.Flags().GetBool("json")

		stackName := stackFlag
		if stackName == "" && len(args) > 0 {
			stackName = args[0]
		}
		if stackName == "" {
			return fmt.Errorf("stack name required: sdf new <stack-name>")
		}

		_, err := runNewCore(stackName, base, branchFlag, jsonFlag, false)
		return err
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().String("stack", "", "name for the stack (alternative to positional arg)")
	initCmd.Flags().String("base", "", "base branch (default: auto-detected from origin HEAD)")
	initCmd.Flags().String("branch", "", "name for the first branch (default: stack name)")
	initCmd.Flags().Bool("json", false, "output machine-readable JSON")
}
