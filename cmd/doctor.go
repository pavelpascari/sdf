package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/ui"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:         "doctor",
	Short:       "Check that dependencies are available",
	Long:        `Reports the status of git (required), gh (needed for PR operations), and claude (needed for conflict resolution and PR descriptions).`,
	Example:     `  sdf doctor`,
	Annotations: map[string]string{"category": "utility"},
	RunE:        runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	fmt.Println("sdf doctor — checking dependencies")
	fmt.Println()
	allOk := true

	// git (required)
	if path, err := exec.LookPath(gitpkg.Binary); err != nil {
		fmt.Printf("  %s git        not found (required)\n", ui.SymFail)
		allOk = false
	} else {
		ver := getVersion(gitpkg.Binary, "--version")
		fmt.Printf("  %s git        %s (%s)\n", ui.SymOK, ver, path)
	}

	// gh (required for PR operations)
	if path, err := exec.LookPath(ghpkg.Binary); err != nil {
		fmt.Printf("  %s gh         not found (needed for PR operations)\n", ui.Gray.Render("●"))
	} else {
		ver := getVersion(ghpkg.Binary, "version")
		fmt.Printf("  %s gh         %s (%s)\n", ui.SymOK, ver, path)
	}

	// claude (optional, needed for conflict resolution and PR description generation)
	if path, err := exec.LookPath(claudepkg.Binary); err != nil {
		fmt.Printf("  %s claude     not found (needed for conflict resolution and PR descriptions)\n", ui.Gray.Render("●"))
	} else {
		ver := getVersion(claudepkg.Binary, "--version")
		fmt.Printf("  %s claude     %s (%s)\n", ui.SymOK, ver, path)
	}

	fmt.Println()
	if !allOk {
		return fmt.Errorf("missing required dependencies")
	}
	fmt.Println("All required dependencies are available.")
	return nil
}

func getVersion(name string, arg string) string {
	c := exec.Command(name, arg)
	out, err := c.Output()
	if err != nil {
		return "unknown"
	}
	// Take first line only
	line := strings.Split(strings.TrimSpace(string(out)), "\n")[0]
	return line
}
