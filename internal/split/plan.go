// Package split implements the analysis, planning, and execution of
// branch splitting — decomposing a large branch into a stack of smaller PRs.
package split

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pavelpascari/sdf/internal/stack"
	"gopkg.in/yaml.v3"
)

// Plan represents a split plan returned by Claude or provided by the user.
type Plan struct {
	Layers []Layer `yaml:"layers"`
}

// Layer represents a single layer (future PR) in the split plan.
type Layer struct {
	Name         string        `yaml:"name"`
	Description  string        `yaml:"description"`
	Files        []string      `yaml:"files"`
	PartialFiles []PartialFile `yaml:"partial_files,omitempty"`
}

// PartialFile represents a file split across layers at hunk level.
type PartialFile struct {
	Path  string `yaml:"path"`
	Hunks []int  `yaml:"hunks"`
}

// HunkAssignmentResponse is the Phase 2 response from Claude.
type HunkAssignmentResponse struct {
	HunkAssignments []FileHunkAssignment `yaml:"hunk_assignments"`
}

// FileHunkAssignment maps hunks of a single file to layers.
type FileHunkAssignment struct {
	File        string        `yaml:"file"`
	Assignments []HunkToLayer `yaml:"assignments"`
}

// HunkToLayer assigns a single hunk index to a layer name.
type HunkToLayer struct {
	Hunk  int    `yaml:"hunk"`
	Layer string `yaml:"layer"`
}

// SplitPlansDir is the subdirectory for persisted split plans.
const SplitPlansDir = "split-plans"

// PlanPath returns the file path for a stack's split plan.
// Uses filepath.Base to prevent path traversal from stack names.
func PlanPath(root, stackName string) string {
	return filepath.Join(root, stack.SDFDir, SplitPlansDir, filepath.Base(stackName)+".yaml")
}

// SavePlan serializes a Plan to YAML and writes it to path.
// Creates parent directories if needed.
func SavePlan(path string, plan *Plan) error {
	data, err := yaml.Marshal(plan)
	if err != nil {
		return fmt.Errorf("cannot marshal plan: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("cannot create plan directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("cannot write plan: %w", err)
	}
	return nil
}

// DeletePlan removes a plan file. Returns nil if the file doesn't exist.
func DeletePlan(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot delete plan: %w", err)
	}
	return nil
}

// AllFilePaths returns all file paths in the layer (both whole and partial).
func (l Layer) AllFilePaths() []string {
	result := make([]string, 0, len(l.Files)+len(l.PartialFiles))
	result = append(result, l.Files...)
	for _, pf := range l.PartialFiles {
		result = append(result, pf.Path)
	}
	return result
}

// SharedFiles detects files that appear in multiple layers.
// Returns a map of file path → list of layer names that include it.
func SharedFiles(plan *Plan) map[string][]string {
	counts := make(map[string][]string)
	for _, layer := range plan.Layers {
		for _, f := range layer.Files {
			counts[f] = append(counts[f], layer.Name)
		}
	}
	shared := make(map[string][]string)
	for f, layers := range counts {
		if len(layers) > 1 {
			shared[f] = layers
		}
	}
	return shared
}

// DeduplicateSharedFiles removes duplicate file entries from a Phase 1 plan
// by keeping each shared file only in the first layer that lists it.
// This makes the plan executable when hunk assignment is unavailable.
func DeduplicateSharedFiles(plan *Plan) *Plan {
	seen := make(map[string]bool)
	deduped := &Plan{Layers: make([]Layer, len(plan.Layers))}
	for i, layer := range plan.Layers {
		deduped.Layers[i] = Layer{
			Name:        layer.Name,
			Description: layer.Description,
		}
		for _, f := range layer.Files {
			if !seen[f] {
				deduped.Layers[i].Files = append(deduped.Layers[i].Files, f)
				seen[f] = true
			}
		}
		deduped.Layers[i].PartialFiles = layer.PartialFiles
	}
	return deduped
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

// ValidatePhase1 validates a Phase 1 plan (files may appear in multiple layers).
// This is a relaxed version of ValidatePlan — duplicates are allowed.
func ValidatePhase1(plan *Plan, changedFiles []string) []error {
	var errs []error

	changedSet := make(map[string]bool, len(changedFiles))
	for _, f := range changedFiles {
		changedSet[f] = true
	}

	assignedFiles := make(map[string]bool)

	for _, layer := range plan.Layers {
		if len(layer.Files) == 0 {
			errs = append(errs, fmt.Errorf("layer %q has no files", layer.Name))
		}
		if !validName.MatchString(layer.Name) {
			errs = append(errs, fmt.Errorf("layer name %q is not valid kebab-case", layer.Name))
		}
		for _, f := range layer.Files {
			assignedFiles[f] = true
			if !changedSet[f] {
				errs = append(errs, fmt.Errorf("file %q in layer %q is not in the source branch diff", f, layer.Name))
			}
		}
	}

	for _, f := range changedFiles {
		if !assignedFiles[f] {
			errs = append(errs, fmt.Errorf("file %q is not assigned to any layer", f))
		}
	}

	return errs
}

// ValidateHunkAssignment checks Phase 2 hunk assignments.
// shared maps file path → list of layer names that listed the file.
// hunkCounts maps file path → total number of hunks in the file's diff.
func ValidateHunkAssignment(resp *HunkAssignmentResponse, shared map[string][]string, hunkCounts map[string]int) []error {
	var errs []error

	validLayers := make(map[string]map[string]bool)
	for file, layers := range shared {
		validLayers[file] = make(map[string]bool)
		for _, l := range layers {
			validLayers[file][l] = true
		}
	}

	assigned := make(map[string]map[int]string) // file → hunk → layer

	for _, fa := range resp.HunkAssignments {
		if _, ok := shared[fa.File]; !ok {
			errs = append(errs, fmt.Errorf("file %q is not a shared file", fa.File))
			continue
		}
		if assigned[fa.File] == nil {
			assigned[fa.File] = make(map[int]string)
		}
		maxHunk := hunkCounts[fa.File]
		for _, a := range fa.Assignments {
			if a.Hunk < 0 || a.Hunk >= maxHunk {
				errs = append(errs, fmt.Errorf("hunk %d for %s is out of range (0-%d)", a.Hunk, fa.File, maxHunk-1))
				continue
			}
			if !validLayers[fa.File][a.Layer] {
				errs = append(errs, fmt.Errorf("hunk %d for %s assigned to %q which didn't list the file", a.Hunk, fa.File, a.Layer))
				continue
			}
			if prev, ok := assigned[fa.File][a.Hunk]; ok {
				errs = append(errs, fmt.Errorf("hunk %d for %s assigned to both %q and %q", a.Hunk, fa.File, prev, a.Layer))
				continue
			}
			assigned[fa.File][a.Hunk] = a.Layer
		}
	}

	for file, count := range hunkCounts {
		for i := 0; i < count; i++ {
			if _, ok := assigned[file][i]; !ok {
				errs = append(errs, fmt.Errorf("hunk %d for %s is not assigned to any layer", i, file))
			}
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
