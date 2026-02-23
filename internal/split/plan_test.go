package split

import (
	"os"
	"path/filepath"
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

func TestDeduplicateSharedFiles(t *testing.T) {
	plan := &Plan{
		Layers: []Layer{
			{Name: "db", Description: "schema", Files: []string{"shared.go", "a.go"}},
			{Name: "api", Description: "endpoints", Files: []string{"shared.go", "b.go"}},
		},
	}

	deduped := DeduplicateSharedFiles(plan)

	// shared.go should only be in the first layer
	if len(deduped.Layers[0].Files) != 2 {
		t.Errorf("layer 0 files: got %d, want 2", len(deduped.Layers[0].Files))
	}
	if len(deduped.Layers[1].Files) != 1 {
		t.Errorf("layer 1 files: got %d, want 1", len(deduped.Layers[1].Files))
	}
	if deduped.Layers[1].Files[0] != "b.go" {
		t.Errorf("layer 1 file: got %q, want %q", deduped.Layers[1].Files[0], "b.go")
	}
	// Descriptions preserved
	if deduped.Layers[0].Description != "schema" {
		t.Errorf("layer 0 description: got %q", deduped.Layers[0].Description)
	}
	if deduped.Layers[1].Description != "endpoints" {
		t.Errorf("layer 1 description: got %q", deduped.Layers[1].Description)
	}
}

func TestPlanPath_Sanitized(t *testing.T) {
	// filepath.Base should prevent path traversal
	path := PlanPath("/repo", "../../../etc/evil")
	if filepath.Dir(path) != filepath.Join("/repo", ".sdf", "split-plans") {
		t.Errorf("PlanPath should sanitize: got dir %q", filepath.Dir(path))
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

func TestSavePlan_WritesValidYAML(t *testing.T) {
	plan := &Plan{
		Layers: []Layer{
			{Name: "db", Description: "schema", Files: []string{"a.go", "b.go"}},
			{Name: "api", Description: "endpoints", Files: []string{"c.go"},
				PartialFiles: []PartialFile{{Path: "shared.go", Hunks: []int{0, 2}}}},
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "plan.yaml")

	if err := SavePlan(path, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Round-trip: parse back
	got, err := ParsePlan(string(data))
	if err != nil {
		t.Fatalf("ParsePlan round-trip: %v", err)
	}
	if len(got.Layers) != 2 {
		t.Fatalf("expected 2 layers, got %d", len(got.Layers))
	}
	if got.Layers[0].Name != "db" {
		t.Errorf("layer 0 name: got %q, want %q", got.Layers[0].Name, "db")
	}
	if len(got.Layers[1].PartialFiles) != 1 {
		t.Errorf("layer 1 partial files: got %d, want 1", len(got.Layers[1].PartialFiles))
	}
	if got.Layers[1].PartialFiles[0].Path != "shared.go" {
		t.Errorf("partial file path: got %q, want %q", got.Layers[1].PartialFiles[0].Path, "shared.go")
	}
}

func TestPlanPath(t *testing.T) {
	path := PlanPath("/repo", "my-feature")
	want := filepath.Join("/repo", ".sdf", "split-plans", "my-feature.yaml")
	if path != want {
		t.Errorf("PlanPath: got %q, want %q", path, want)
	}
}

func TestDeletePlan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.yaml")
	os.WriteFile(path, []byte("test"), 0644)

	if err := DeletePlan(path); err != nil {
		t.Fatalf("DeletePlan existing: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should be deleted")
	}

	// Deleting non-existent file should not error
	if err := DeletePlan(path); err != nil {
		t.Errorf("DeletePlan non-existent: %v", err)
	}
}
