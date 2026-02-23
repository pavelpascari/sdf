package split

import (
	"fmt"
	"strings"
	"testing"
)

func TestBuildPrompt(t *testing.T) {
	prompt := BuildPrompt("feature/big-change", "main")

	if !strings.Contains(prompt, "feature/big-change") {
		t.Error("prompt should contain source branch")
	}
	if !strings.Contains(prompt, "main") {
		t.Error("prompt should contain base branch")
	}
	if !strings.Contains(prompt, "git diff") {
		t.Error("prompt should instruct Claude to use git diff")
	}
	if !strings.Contains(prompt, "yaml") {
		t.Error("prompt should show expected YAML format")
	}
	if !strings.Contains(prompt, "layers:") {
		t.Error("prompt should show layers structure")
	}
}

func TestBuildPrompt_AllowsSharedFiles(t *testing.T) {
	prompt := BuildPrompt("feature/big-change", "main")
	if !strings.Contains(prompt, "multiple layers") {
		t.Error("prompt should mention files can appear in multiple layers")
	}
}

func TestBuildHunkPrompt(t *testing.T) {
	sharedFiles := map[string][]string{
		"user.go": {"db", "api"},
	}
	files := []FileDiff{
		{
			Path:   "user.go",
			Header: "diff --git a/user.go b/user.go\n",
			Hunks: []Hunk{
				{Header: "@@ -1,5 +1,6 @@\n", Body: " context\n+Email\n"},
				{Header: "@@ -10,3 +11,5 @@\n", Body: " context\n+Validate\n"},
			},
		},
	}

	prompt := BuildHunkPrompt(sharedFiles, files)

	if !strings.Contains(prompt, "user.go") {
		t.Error("prompt should contain file path")
	}
	if !strings.Contains(prompt, "Hunk 0:") {
		t.Error("prompt should have numbered hunks")
	}
	if !strings.Contains(prompt, "Hunk 1:") {
		t.Error("prompt should have hunk 1")
	}
	if !strings.Contains(prompt, "db") || !strings.Contains(prompt, "api") {
		t.Error("prompt should mention layer names")
	}
	if !strings.Contains(prompt, "hunk_assignments") {
		t.Error("prompt should show expected YAML format")
	}
}

func TestParseHunkAssignment(t *testing.T) {
	text := "Here are the assignments:\n```yaml\nhunk_assignments:\n  - file: user.go\n    assignments:\n      - hunk: 0\n        layer: db\n      - hunk: 1\n        layer: api\n```"

	resp, err := ParseHunkAssignment(text)
	if err != nil {
		t.Fatalf("ParseHunkAssignment: %v", err)
	}
	if len(resp.HunkAssignments) != 1 {
		t.Fatalf("expected 1 file assignment, got %d", len(resp.HunkAssignments))
	}
	if resp.HunkAssignments[0].File != "user.go" {
		t.Errorf("file: got %q", resp.HunkAssignments[0].File)
	}
	if len(resp.HunkAssignments[0].Assignments) != 2 {
		t.Fatalf("expected 2 hunk assignments, got %d", len(resp.HunkAssignments[0].Assignments))
	}
}

func TestMergePlan(t *testing.T) {
	plan := &Plan{
		Layers: []Layer{
			{Name: "db", Description: "schema", Files: []string{"shared.go", "a.go"}},
			{Name: "api", Description: "endpoints", Files: []string{"shared.go", "b.go"}},
		},
	}
	resp := &HunkAssignmentResponse{
		HunkAssignments: []FileHunkAssignment{
			{
				File: "shared.go",
				Assignments: []HunkToLayer{
					{Hunk: 0, Layer: "db"},
					{Hunk: 1, Layer: "api"},
				},
			},
		},
	}

	merged := MergePlan(plan, resp)

	// Layer "db" should have a.go as whole file and shared.go as partial
	if len(merged.Layers[0].Files) != 1 || merged.Layers[0].Files[0] != "a.go" {
		t.Errorf("db layer files: %v", merged.Layers[0].Files)
	}
	if len(merged.Layers[0].PartialFiles) != 1 {
		t.Fatalf("db layer partial files: %v", merged.Layers[0].PartialFiles)
	}
	if merged.Layers[0].PartialFiles[0].Path != "shared.go" {
		t.Errorf("db partial path: %q", merged.Layers[0].PartialFiles[0].Path)
	}
	if len(merged.Layers[0].PartialFiles[0].Hunks) != 1 || merged.Layers[0].PartialFiles[0].Hunks[0] != 0 {
		t.Errorf("db partial hunks: %v", merged.Layers[0].PartialFiles[0].Hunks)
	}

	// Layer "api" should have b.go as whole file and shared.go hunk 1
	if len(merged.Layers[1].Files) != 1 || merged.Layers[1].Files[0] != "b.go" {
		t.Errorf("api layer files: %v", merged.Layers[1].Files)
	}
	if len(merged.Layers[1].PartialFiles) != 1 {
		t.Fatalf("api layer partial files: %v", merged.Layers[1].PartialFiles)
	}
	if merged.Layers[1].PartialFiles[0].Hunks[0] != 1 {
		t.Errorf("api partial hunks: %v", merged.Layers[1].PartialFiles[0].Hunks)
	}
}

func TestBuildRetryPrompt(t *testing.T) {
	errs := []error{
		fmt.Errorf("file %q is not assigned to any layer", "missing.go"),
		fmt.Errorf("file %q appears in both %q and %q", "dup.go", "a", "b"),
	}

	prompt := BuildRetryPrompt(errs)

	if !strings.Contains(prompt, "missing.go") {
		t.Error("retry prompt should mention missing file")
	}
	if !strings.Contains(prompt, "dup.go") {
		t.Error("retry prompt should mention duplicate file")
	}
}
