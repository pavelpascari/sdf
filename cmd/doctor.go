package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
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

	// Worktree integrity checks (only when inside an sdf repo).
	if root, err := stack.FindRoot(); err == nil {
		if problems := checkWorktrees(root); len(problems) > 0 {
			bus.Print("")
			bus.Print("Worktree integrity:")
			for _, p := range problems {
				bus.Printf("  %s %s", ui.SymWarn, p)
				allOk = false
			}
		}
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

// normPath resolves symlinks in p, falling back to p on error.
// This prevents false positives on macOS where /tmp is a symlink to /private/tmp.
func normPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

// checkWorktrees verifies worktree-mode stacks against git's worktree list and
// the filesystem. Returns a list of human-readable problems (empty = healthy).
func checkWorktrees(root string) []string {
	var gitPaths []string
	if list, err := gitpkg.WorktreeList(); err == nil {
		for _, w := range list {
			gitPaths = append(gitPaths, w.Path)
		}
	}
	return checkWorktreesWithPaths(root, gitPaths)
}

// checkWorktreesWithPaths is the testable core of checkWorktrees.
// gitPaths is the list of paths from `git worktree list`.
func checkWorktreesWithPaths(root string, gitPaths []string) []string {
	var problems []string
	stacks, err := stack.LoadAll(root)
	if err != nil {
		return problems
	}

	known := map[string]bool{}
	for _, p := range gitPaths {
		known[normPath(p)] = true
	}

	for _, s := range stacks {
		if !s.Worktree {
			continue
		}
		for _, n := range s.Nodes {
			if n.Status == "merged" || n.Status == "closed" {
				continue
			}
			if n.WorktreePath == "" {
				problems = append(problems, fmt.Sprintf("stack %s: branch %s has no worktree (run `sdf worktree enable`)", s.StackID, n.Branch))
				continue
			}
			if _, statErr := os.Stat(n.WorktreePath); os.IsNotExist(statErr) {
				problems = append(problems, fmt.Sprintf("stack %s: worktree for %s is missing at %s", s.StackID, n.Branch, n.WorktreePath))
			} else if !known[normPath(n.WorktreePath)] {
				problems = append(problems, fmt.Sprintf("stack %s: %s is not registered with git (run `git worktree prune` / re-create)", s.StackID, n.WorktreePath))
			}
		}
	}
	return problems
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
