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
