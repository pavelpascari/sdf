// Package split implements the analysis, planning, and execution of
// branch splitting — decomposing a large branch into a stack of smaller PRs.
package split

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Plan represents a split plan returned by Claude or provided by the user.
type Plan struct {
	Layers []Layer `yaml:"layers"`
}

// Layer represents a single layer (future PR) in the split plan.
type Layer struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Files       []string `yaml:"files"`
}

// validName matches kebab-case identifiers suitable for branch names.
var validName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ParsePlan extracts and parses a YAML Plan from Claude's response text.
// It looks for ```yaml fences first, then falls back to parsing the entire text.
func ParsePlan(text string) (*Plan, error) {
	yamlStr := extractYAML(text)

	var plan Plan
	if err := yaml.Unmarshal([]byte(yamlStr), &plan); err != nil {
		return nil, fmt.Errorf("cannot parse YAML: %w", err)
	}

	if len(plan.Layers) == 0 {
		return nil, fmt.Errorf("plan has no layers")
	}

	return &plan, nil
}

// extractYAML pulls YAML content from fenced code blocks, or returns the input as-is.
func extractYAML(text string) string {
	// Try to find ```yaml ... ``` block
	start := strings.Index(text, "```yaml")
	if start == -1 {
		start = strings.Index(text, "```yml")
	}
	if start != -1 {
		// Skip the opening fence line
		contentStart := strings.Index(text[start:], "\n")
		if contentStart == -1 {
			return text
		}
		contentStart += start + 1

		end := strings.Index(text[contentStart:], "```")
		if end != -1 {
			return text[contentStart : contentStart+end]
		}
	}

	return text
}

// ValidatePlan checks all hard-gate constraints on a plan.
// changedFiles is the list from git diff --name-only base...source.
// Returns a slice of errors (empty if valid).
func ValidatePlan(plan *Plan, changedFiles []string) []error {
	var errs []error

	changedSet := make(map[string]bool, len(changedFiles))
	for _, f := range changedFiles {
		changedSet[f] = true
	}

	assignedFiles := make(map[string]string) // file → layer name

	for _, layer := range plan.Layers {
		// Non-empty layers
		if len(layer.Files) == 0 {
			errs = append(errs, fmt.Errorf("layer %q has no files", layer.Name))
		}

		// Valid names
		if !validName.MatchString(layer.Name) {
			errs = append(errs, fmt.Errorf("layer name %q is not valid kebab-case", layer.Name))
		}

		for _, f := range layer.Files {
			// No duplicates
			if prev, ok := assignedFiles[f]; ok {
				errs = append(errs, fmt.Errorf("file %q appears in both %q and %q", f, prev, layer.Name))
			}
			assignedFiles[f] = layer.Name

			// No extras
			if !changedSet[f] {
				errs = append(errs, fmt.Errorf("file %q in layer %q is not in the source branch diff", f, layer.Name))
			}
		}
	}

	// Completeness
	for _, f := range changedFiles {
		if _, ok := assignedFiles[f]; !ok {
			errs = append(errs, fmt.Errorf("file %q is not assigned to any layer", f))
		}
	}

	return errs
}

// ValidationSummary formats validation errors into a string suitable for
// sending back to Claude as a retry prompt.
func ValidationSummary(errs []error) string {
	var b strings.Builder
	b.WriteString("The plan has validation errors:\n")
	for _, e := range errs {
		fmt.Fprintf(&b, "- %s\n", e.Error())
	}
	b.WriteString("\nPlease fix these issues and return the corrected YAML plan.")
	return b.String()
}
