package ai

import (
	"strings"
	"testing"
)

func TestBuildIntroPrompt(t *testing.T) {
	prompt := BuildIntroPrompt()

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
		{"has sdf ls command", "sdf ls"},
		{"has sdf status command", "sdf status"},
		{"has sdf init command", "sdf init"},
		{"has sdf switch command", "sdf switch"},
		{"has sdf move command", "sdf move"},
		{"has sdf fetch command", "sdf fetch"},
		{"has sdf doctor command", "sdf doctor"},
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

