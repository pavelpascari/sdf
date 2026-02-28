package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/render"
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
	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{})
	defer func() { _ = bus.Finish() }()

	bus.Print("sdf doctor — checking dependencies")
	bus.Print("")
	allOk := true

	// git (required)
	if path, err := exec.LookPath(gitpkg.Binary); err != nil {
		bus.Printf("  %s git        not found (required)", ui.SymFail)
		allOk = false
	} else {
		ver := getVersion(gitpkg.Binary, "--version")
		bus.Printf("  %s git        %s (%s)", ui.SymOK, ver, path)
	}

	// gh (required for PR operations)
	if path, err := exec.LookPath(ghpkg.Binary); err != nil {
		bus.Printf("  %s gh         not found (needed for PR operations)", ui.Gray.Render("●"))
	} else {
		ver := getVersion(ghpkg.Binary, "version")
		bus.Printf("  %s gh         %s (%s)", ui.SymOK, ver, path)
	}

	// claude (optional, needed for conflict resolution and PR description generation)
	if path, err := exec.LookPath(claudepkg.Binary); err != nil {
		bus.Printf("  %s claude     not found (needed for conflict resolution and PR descriptions)", ui.Gray.Render("●"))
	} else {
		ver := getVersion(claudepkg.Binary, "--version")
		bus.Printf("  %s claude     %s (%s)", ui.SymOK, ver, path)
	}

	bus.Print("")
	if !allOk {
		return fmt.Errorf("missing required dependencies")
	}
	bus.Print("All required dependencies are available.")
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
