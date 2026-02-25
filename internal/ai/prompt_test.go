package ai

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// fakeRoot builds a minimal cobra command tree for testing.
func fakeRoot() *cobra.Command {
	root := &cobra.Command{Use: "sdf"}
	root.AddCommand(
		&cobra.Command{Use: "init <name>", Short: "Create a new stack"},
		&cobra.Command{Use: "branch <name>", Short: "Add a branch to the stack"},
		&cobra.Command{Use: "sync", Short: "Cascade-rebase the stack"},
		&cobra.Command{Use: "status", Short: "Show stack topology"},
		&cobra.Command{Use: "doctor", Short: "Check dependencies"},
	)
	return root
}

func TestBuildIntroPrompt_StaticSections(t *testing.T) {
	prompt := BuildIntroPrompt(fakeRoot())

	checks := []struct {
		name     string
		contains string
	}{
		{"mentions SDF", "SDF"},
		{"mentions stacked PRs", "stacked PRs"},
		{"has sdf branch rule", "sdf branch <name>"},
		{"has sdf sync rule", "sdf sync"},
		{"has sdf pr rule", "sdf pr"},
		{"has sdf merge rule", "sdf merge"},
		{"has when to run section", "When to Run What"},
		{"asks to create skill", "skill"},
		{"mentions SKILL.md path", ".claude/skills/sdf/SKILL.md"},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(prompt, c.contains) {
				t.Errorf("intro prompt missing %q", c.contains)
			}
		})
	}
}

func TestBuildIntroPrompt_DynamicCommandTable(t *testing.T) {
	prompt := BuildIntroPrompt(fakeRoot())

	// Commands from fakeRoot should appear in the table.
	for _, cmd := range []string{"sdf init", "sdf branch", "sdf sync", "sdf status", "sdf doctor"} {
		if !strings.Contains(prompt, cmd) {
			t.Errorf("expected command table to contain %q", cmd)
		}
	}

	// Table header should be present.
	if !strings.Contains(prompt, "| Command | Purpose |") {
		t.Error("expected markdown table header")
	}
}

func TestBuildIntroPrompt_SkipsHiddenAndMeta(t *testing.T) {
	root := &cobra.Command{Use: "sdf"}
	root.AddCommand(
		&cobra.Command{Use: "sync", Short: "Cascade-rebase"},
		&cobra.Command{Use: "ai", Short: "AI commands"},
		&cobra.Command{Use: "version", Short: "Print version"},
		&cobra.Command{Use: "secret", Short: "Hidden cmd", Hidden: true},
	)

	prompt := BuildIntroPrompt(root)

	if !strings.Contains(prompt, "sdf sync") {
		t.Error("expected sync in table")
	}
	if strings.Contains(prompt, "| `sdf ai`") {
		t.Error("ai command should be excluded from table")
	}
	if strings.Contains(prompt, "| `sdf version`") {
		t.Error("version command should be excluded from table")
	}
	if strings.Contains(prompt, "secret") {
		t.Error("hidden command should be excluded from table")
	}
}
