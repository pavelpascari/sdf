package split

import (
	"strings"
	"testing"
)

func TestParsePlan_FencedYAML(t *testing.T) {
	input := "Here is the plan:\n```yaml\nlayers:\n  - name: db-schema\n    description: \"Add users table\"\n    files:\n      - migrations/001.sql\n      - internal/models/user.go\n```\nDone."

	plan, err := ParsePlan(input)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	if len(plan.Layers) != 1 {
		t.Fatalf("expected 1 layer, got %d", len(plan.Layers))
	}
	if plan.Layers[0].Name != "db-schema" {
		t.Errorf("name: got %q, want %q", plan.Layers[0].Name, "db-schema")
	}
	if len(plan.Layers[0].Files) != 2 {
		t.Errorf("files: got %d, want 2", len(plan.Layers[0].Files))
	}
}

func TestParsePlan_BareYAML(t *testing.T) {
	input := "layers:\n  - name: api\n    description: \"REST endpoints\"\n    files:\n      - handler.go\n"

	plan, err := ParsePlan(input)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	if len(plan.Layers) != 1 {
		t.Fatalf("expected 1 layer, got %d", len(plan.Layers))
	}
}

func TestParsePlan_InvalidYAML(t *testing.T) {
	_, err := ParsePlan("this is not yaml at all: [[[")
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestParsePlan_NoLayers(t *testing.T) {
	_, err := ParsePlan("layers: []")
	if err == nil {
		t.Fatal("expected error for empty layers")
	}
}

func TestValidatePlan_Valid(t *testing.T) {
	plan := &Plan{
		Layers: []Layer{
			{Name: "db", Description: "schema", Files: []string{"a.go", "b.go"}},
			{Name: "api", Description: "endpoints", Files: []string{"c.go"}},
		},
	}
	changedFiles := []string{"a.go", "b.go", "c.go"}

	errs := ValidatePlan(plan, changedFiles)
	if len(errs) > 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestValidatePlan_MissingFile(t *testing.T) {
	plan := &Plan{
		Layers: []Layer{
			{Name: "db", Description: "schema", Files: []string{"a.go"}},
		},
	}
	changedFiles := []string{"a.go", "b.go"}

	errs := ValidatePlan(plan, changedFiles)
	if len(errs) == 0 {
		t.Fatal("expected error for missing file")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "b.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error mentioning b.go, got: %v", errs)
	}
}

func TestValidatePlan_DuplicateFile(t *testing.T) {
	plan := &Plan{
		Layers: []Layer{
			{Name: "db", Description: "schema", Files: []string{"a.go"}},
			{Name: "api", Description: "endpoints", Files: []string{"a.go"}},
		},
	}
	changedFiles := []string{"a.go"}

	errs := ValidatePlan(plan, changedFiles)
	if len(errs) == 0 {
		t.Fatal("expected error for duplicate file")
	}
}

func TestValidatePlan_ExtraFile(t *testing.T) {
	plan := &Plan{
		Layers: []Layer{
			{Name: "db", Description: "schema", Files: []string{"a.go", "z.go"}},
		},
	}
	changedFiles := []string{"a.go"}

	errs := ValidatePlan(plan, changedFiles)
	if len(errs) == 0 {
		t.Fatal("expected error for extra file")
	}
}

func TestValidatePlan_EmptyLayer(t *testing.T) {
	plan := &Plan{
		Layers: []Layer{
			{Name: "db", Description: "schema", Files: []string{}},
		},
	}
	changedFiles := []string{}

	errs := ValidatePlan(plan, changedFiles)
	if len(errs) == 0 {
		t.Fatal("expected error for empty layer")
	}
}

func TestValidatePlan_InvalidName(t *testing.T) {
	plan := &Plan{
		Layers: []Layer{
			{Name: "has spaces", Description: "bad", Files: []string{"a.go"}},
		},
	}
	changedFiles := []string{"a.go"}

	errs := ValidatePlan(plan, changedFiles)
	if len(errs) == 0 {
		t.Fatal("expected error for invalid layer name")
	}
}

func TestAllFilePaths(t *testing.T) {
	layer := Layer{
		Name:  "test",
		Files: []string{"a.go", "b.go"},
		PartialFiles: []PartialFile{
			{Path: "shared.go", Hunks: []int{0, 2}},
		},
	}
	paths := layer.AllFilePaths()
	if len(paths) != 3 {
		t.Fatalf("expected 3 paths, got %d", len(paths))
	}
	if paths[2] != "shared.go" {
		t.Errorf("expected shared.go at index 2, got %q", paths[2])
	}
}

func TestSharedFiles_NoneShared(t *testing.T) {
	plan := &Plan{
		Layers: []Layer{
			{Name: "a", Files: []string{"one.go"}},
			{Name: "b", Files: []string{"two.go"}},
		},
	}
	shared := SharedFiles(plan)
	if len(shared) != 0 {
		t.Errorf("expected no shared files, got %v", shared)
	}
}

func TestSharedFiles_OneShared(t *testing.T) {
	plan := &Plan{
		Layers: []Layer{
			{Name: "db", Files: []string{"shared.go", "a.go"}},
			{Name: "api", Files: []string{"shared.go", "b.go"}},
		},
	}
	shared := SharedFiles(plan)
	if len(shared) != 1 {
		t.Fatalf("expected 1 shared file, got %d", len(shared))
	}
	layers, ok := shared["shared.go"]
	if !ok {
		t.Fatal("shared.go not found")
	}
	if len(layers) != 2 {
		t.Errorf("expected 2 layers, got %d", len(layers))
	}
}

func TestValidatePhase1_AllowsSharedFiles(t *testing.T) {
	plan := &Plan{
		Layers: []Layer{
			{Name: "db", Description: "schema", Files: []string{"shared.go", "a.go"}},
			{Name: "api", Description: "endpoints", Files: []string{"shared.go", "b.go"}},
		},
	}
	changedFiles := []string{"shared.go", "a.go", "b.go"}

	errs := ValidatePhase1(plan, changedFiles)
	if len(errs) != 0 {
		t.Errorf("Phase 1 should allow shared files, got errors: %v", errs)
	}
}

func TestValidatePhase1_StillCatchesMissing(t *testing.T) {
	plan := &Plan{
		Layers: []Layer{
			{Name: "db", Description: "schema", Files: []string{"a.go"}},
		},
	}
	changedFiles := []string{"a.go", "missing.go"}

	errs := ValidatePhase1(plan, changedFiles)
	if len(errs) == 0 {
		t.Fatal("should catch missing file")
	}
}

func TestValidatePhase1_CatchesExtras(t *testing.T) {
	plan := &Plan{
		Layers: []Layer{
			{Name: "db", Description: "schema", Files: []string{"a.go", "extra.go"}},
		},
	}
	changedFiles := []string{"a.go"}

	errs := ValidatePhase1(plan, changedFiles)
	if len(errs) == 0 {
		t.Fatal("should catch extra file")
	}
}

func TestValidateHunkAssignment_Valid(t *testing.T) {
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
	shared := map[string][]string{"shared.go": {"db", "api"}}
	hunkCounts := map[string]int{"shared.go": 2}

	errs := ValidateHunkAssignment(resp, shared, hunkCounts)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestValidateHunkAssignment_MissingHunk(t *testing.T) {
	resp := &HunkAssignmentResponse{
		HunkAssignments: []FileHunkAssignment{
			{
				File: "shared.go",
				Assignments: []HunkToLayer{
					{Hunk: 0, Layer: "db"},
				},
			},
		},
	}
	shared := map[string][]string{"shared.go": {"db", "api"}}
	hunkCounts := map[string]int{"shared.go": 2}

	errs := ValidateHunkAssignment(resp, shared, hunkCounts)
	if len(errs) == 0 {
		t.Fatal("should catch missing hunk")
	}
}

func TestValidateHunkAssignment_DuplicateHunk(t *testing.T) {
	resp := &HunkAssignmentResponse{
		HunkAssignments: []FileHunkAssignment{
			{
				File: "shared.go",
				Assignments: []HunkToLayer{
					{Hunk: 0, Layer: "db"},
					{Hunk: 0, Layer: "api"},
					{Hunk: 1, Layer: "api"},
				},
			},
		},
	}
	shared := map[string][]string{"shared.go": {"db", "api"}}
	hunkCounts := map[string]int{"shared.go": 2}

	errs := ValidateHunkAssignment(resp, shared, hunkCounts)
	if len(errs) == 0 {
		t.Fatal("should catch duplicate hunk")
	}
}

func TestValidateHunkAssignment_WrongLayer(t *testing.T) {
	resp := &HunkAssignmentResponse{
		HunkAssignments: []FileHunkAssignment{
			{
				File: "shared.go",
				Assignments: []HunkToLayer{
					{Hunk: 0, Layer: "db"},
					{Hunk: 1, Layer: "unknown"},
				},
			},
		},
	}
	shared := map[string][]string{"shared.go": {"db", "api"}}
	hunkCounts := map[string]int{"shared.go": 2}

	errs := ValidateHunkAssignment(resp, shared, hunkCounts)
	if len(errs) == 0 {
		t.Fatal("should catch wrong layer")
	}
}
