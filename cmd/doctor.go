package cmd

import (
	"encoding/json"
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

// DoctorResult is the structured output of sdf doctor when --json is used.
type DoctorResult struct {
	Dependencies []DependencyResult `json:"dependencies"`
	OK           bool               `json:"ok"`
}

// DependencyResult describes a single dependency check.
type DependencyResult struct {
	Name     string `json:"name"`
	Found    bool   `json:"found"`
	Version  string `json:"version,omitempty"`
	Path     string `json:"path,omitempty"`
	Required bool   `json:"required"`
}

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
	doctorCmd.Flags().Bool("json", false, "output result as JSON")
}

func runDoctor(cmd *cobra.Command, args []string) error {
	var jsonFlag bool
	if cmd != nil {
		jsonFlag, _ = cmd.Flags().GetBool("json")
	}

	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{})
	defer func() { _ = bus.Finish() }()

	bus.Print("sdf doctor — checking dependencies")
	bus.Print("")
	allOk := true

	type depCheck struct {
		name     string
		binary   string
		verArg   string
		required bool
		missing  string // message when not found
	}

	checks := []depCheck{
		{name: "git", binary: gitpkg.Binary, verArg: "--version", required: true, missing: "not found (required)"},
		{name: "gh", binary: ghpkg.Binary, verArg: "version", required: false, missing: "not found (needed for PR operations)"},
		{name: "claude", binary: claudepkg.Binary, verArg: "--version", required: false, missing: "not found (needed for conflict resolution and PR descriptions)"},
	}

	var deps []DependencyResult
	for _, chk := range checks {
		dep := DependencyResult{Name: chk.name, Required: chk.required}
		if path, err := exec.LookPath(chk.binary); err != nil {
			dep.Found = false
			if chk.required {
				bus.Printf("  %s %-10s %s", ui.SymFail, chk.name, chk.missing)
				allOk = false
			} else {
				bus.Printf("  %s %-10s %s", ui.Gray.Render("●"), chk.name, chk.missing)
			}
		} else {
			dep.Found = true
			dep.Path = path
			dep.Version = getVersion(chk.binary, chk.verArg)
			bus.Printf("  %s %-10s %s (%s)", ui.SymOK, chk.name, dep.Version, path)
		}
		deps = append(deps, dep)
	}

	if jsonFlag {
		result := DoctorResult{Dependencies: deps, OK: allOk}
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
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
