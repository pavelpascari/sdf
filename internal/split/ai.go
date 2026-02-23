package split

import (
	"fmt"
	"io"
	"sort"
	"strings"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"gopkg.in/yaml.v3"
)

// MaxRetries is the number of times to retry Claude on validation failure.
const MaxRetries = 3

// AnalysisResult holds the output of a Claude analysis.
type AnalysisResult struct {
	Plan      *Plan
	SessionID string
}

// BuildPrompt constructs the instruction prompt for Claude to analyze a branch.
func BuildPrompt(fromBranch, base string) string {
	return fmt.Sprintf(`You are analyzing a git branch to split it into a stack of smaller, reviewable PRs.

SOURCE BRANCH: %s
BASE BRANCH: %s

Your task:
1. Explore the branch using git commands (git diff --stat, git log, git show, etc.)
2. Understand what changed and why
3. Group the changed files into coherent layers ordered from foundational to dependent
4. Return a YAML split plan

Rules:
- Every changed file must appear in at least one layer
- A file MAY appear in multiple layers if its changes clearly serve different concerns.
  When this happens, we'll assign individual hunks in a follow-up step.
- If all of a file's changes belong to one concern, list it in only one layer
- Order layers so earlier layers don't depend on later ones
- Aim for <500 lines of diff per layer when practical
- Group test files with the production code they test
- Use short, descriptive kebab-case layer names

Return ONLY this YAML (wrapped in `+"`"+`yaml fences):

`+"```"+`yaml
layers:
  - name: <kebab-case-name>
    description: "<one-line summary of what this layer does>"
    files:
      - <path/to/file>
      - ...
`+"```", fromBranch, base)
}

// BuildRetryPrompt constructs a follow-up prompt describing validation errors.
func BuildRetryPrompt(errs []error) string {
	return ValidationSummary(errs)
}

// BuildHunkPrompt constructs the Phase 2 prompt for hunk assignment.
// sharedFiles maps file path to layer names that listed it.
// fileDiffs contains the parsed diffs for shared files.
func BuildHunkPrompt(sharedFiles map[string][]string, fileDiffs []FileDiff) string {
	var b strings.Builder
	b.WriteString("Some files in your plan appear in multiple layers. Assign each hunk to exactly one layer.\n\n")

	for _, fd := range fileDiffs {
		layers, ok := sharedFiles[fd.Path]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "## %s\n", fd.Path)
		fmt.Fprintf(&b, "Layers: %s\n\n", strings.Join(layers, ", "))
		b.WriteString(FormatNumberedHunks(fd))
		b.WriteString("---\n\n")
	}

	b.WriteString("Return ONLY this YAML (wrapped in ```yaml fences):\n\n")
	b.WriteString("```yaml\nhunk_assignments:\n  - file: <filepath>\n    assignments:\n      - hunk: 0\n        layer: <layer-name>\n      - hunk: 1\n        layer: <layer-name>\n```")

	return b.String()
}

// ParseHunkAssignment extracts and parses a HunkAssignmentResponse from Claude's response.
func ParseHunkAssignment(text string) (*HunkAssignmentResponse, error) {
	yamlStr := extractYAML(text)
	var resp HunkAssignmentResponse
	if err := yaml.Unmarshal([]byte(yamlStr), &resp); err != nil {
		return nil, fmt.Errorf("cannot parse hunk assignment YAML: %w", err)
	}
	if len(resp.HunkAssignments) == 0 {
		return nil, fmt.Errorf("no hunk assignments in response")
	}
	return &resp, nil
}

// MergePlan combines a Phase 1 plan with Phase 2 hunk assignments into a final plan.
// Shared files are moved from Files to PartialFiles with their assigned hunk indices.
func MergePlan(plan *Plan, resp *HunkAssignmentResponse) *Plan {
	// Build lookup: file -> layer -> hunk indices
	layerHunks := make(map[string]map[string][]int) // file -> layer -> hunks
	for _, fa := range resp.HunkAssignments {
		if layerHunks[fa.File] == nil {
			layerHunks[fa.File] = make(map[string][]int)
		}
		for _, a := range fa.Assignments {
			layerHunks[fa.File][a.Layer] = append(layerHunks[fa.File][a.Layer], a.Hunk)
		}
	}

	sharedSet := make(map[string]bool)
	for file := range layerHunks {
		sharedSet[file] = true
	}

	merged := &Plan{Layers: make([]Layer, len(plan.Layers))}

	for i, layer := range plan.Layers {
		merged.Layers[i] = Layer{
			Name:        layer.Name,
			Description: layer.Description,
		}

		for _, f := range layer.Files {
			if sharedSet[f] {
				if hunks, ok := layerHunks[f][layer.Name]; ok {
					merged.Layers[i].PartialFiles = append(merged.Layers[i].PartialFiles,
						PartialFile{Path: f, Hunks: hunks})
				}
			} else {
				merged.Layers[i].Files = append(merged.Layers[i].Files, f)
			}
		}
	}

	return merged
}

// BuildRefinePrompt constructs the initial prompt for an interactive
// Claude session where the user can refine the split plan.
func BuildRefinePrompt(plan *Plan) string {
	var b strings.Builder
	b.WriteString("The user wants to refine the split plan. Here's the current plan:\n\n")

	for i, layer := range plan.Layers {
		fmt.Fprintf(&b, "Layer %d: %s\n", i+1, layer.Name)
		fmt.Fprintf(&b, "  Description: %s\n", layer.Description)
		for _, f := range layer.Files {
			fmt.Fprintf(&b, "  - %s\n", f)
		}
		for _, pf := range layer.PartialFiles {
			fmt.Fprintf(&b, "  - %s (hunks %v)\n", pf.Path, pf.Hunks)
		}
		b.WriteString("\n")
	}

	b.WriteString("Ask the user what they'd like to change. When they're satisfied, they can\n")
	b.WriteString("type /exit and sdf will re-read the updated plan.\n\n")
	b.WriteString("Remember: every changed file must appear in at least one layer, use kebab-case\n")
	b.WriteString("layer names, and return YAML in the same format when asked.\n")

	return b.String()
}

// BuildReExtractPrompt constructs the prompt to re-extract the plan
// after an interactive refinement session.
func BuildReExtractPrompt() string {
	return `Return the current split plan as YAML (wrapped in ` + "```" + `yaml fences).
Use the exact same format:

` + "```" + `yaml
layers:
  - name: <kebab-case-name>
    description: "<one-line summary>"
    files:
      - <path/to/file>
` + "```" + `

Include any changes discussed in the refinement session.`
}

// Analyze invokes Claude to analyze a branch and produce a split plan.
// Phase 1: file-level grouping (files may appear in multiple layers).
// Phase 2: if shared files exist, assign individual hunks to layers.
// Returns the validated plan with session ID.
func Analyze(fromBranch, base string, display io.Writer) (*AnalysisResult, error) {
	changedFiles, err := gitpkg.DiffNameOnly(base, fromBranch)
	if err != nil {
		return nil, fmt.Errorf("cannot list changed files: %w", err)
	}

	// --- Phase 1: file-level grouping ---
	prompt := BuildPrompt(fromBranch, base)
	name := "split-analysis"

	sr, err := claudepkg.RunPromptStreaming(name, prompt, display)
	if err != nil {
		return nil, fmt.Errorf("claude analysis failed: %w", err)
	}

	sessionID := sr.SessionID
	plan, err := parseAndValidatePhase1(sr.Result, changedFiles, sessionID, name, display)
	if err != nil {
		return nil, err
	}

	// --- Check for shared files ---
	shared := SharedFiles(plan)
	if len(shared) == 0 {
		// No shared files — plan is complete (backward compatible with iteration 3)
		return &AnalysisResult{Plan: plan, SessionID: sessionID}, nil
	}

	// --- Phase 2: hunk assignment ---
	fmt.Fprintf(display, "\n%d file(s) appear in multiple layers — assigning hunks...\n", len(shared))

	fileDiffs, hunkCounts, err := ParseSharedFileDiffs(base, fromBranch, shared)
	if err != nil {
		return nil, err
	}

	hunkPrompt := BuildHunkPrompt(shared, fileDiffs)
	sr, err = claudepkg.RunPromptStreamingResume(name, sessionID, hunkPrompt, display)
	if err != nil {
		return nil, fmt.Errorf("claude hunk assignment failed: %w", err)
	}

	resp, err := parseAndValidatePhase2(sr.Result, shared, hunkCounts, sessionID, name, display)
	if err != nil {
		return nil, err
	}

	merged := MergePlan(plan, resp)
	return &AnalysisResult{Plan: merged, SessionID: sessionID}, nil
}

// parseAndValidatePhase1 parses and validates Phase 1 with retries.
func parseAndValidatePhase1(result string, changedFiles []string, sessionID, name string, display io.Writer) (*Plan, error) {
	sr := claudepkg.StreamResult{Result: result, SessionID: sessionID}

	for attempt := 0; attempt <= MaxRetries; attempt++ {
		plan, parseErr := ParsePlan(sr.Result)
		if parseErr != nil {
			if attempt == MaxRetries {
				return nil, fmt.Errorf("claude returned invalid plan after %d attempts: %w", MaxRetries+1, parseErr)
			}
			retryPrompt := fmt.Sprintf("Your response could not be parsed: %s\n\nPlease return a valid YAML plan.", parseErr.Error())
			fmt.Fprintf(display, "\n⚠ Parse error, retrying (%d/%d)...\n", attempt+1, MaxRetries)
			var err error
			sr, err = claudepkg.RunPromptStreamingResume(name, sessionID, retryPrompt, display)
			if err != nil {
				return nil, fmt.Errorf("claude retry failed: %w", err)
			}
			continue
		}

		validationErrs := ValidatePhase1(plan, changedFiles)
		if len(validationErrs) == 0 {
			return plan, nil
		}

		if attempt == MaxRetries {
			var errMsgs []string
			for _, e := range validationErrs {
				errMsgs = append(errMsgs, e.Error())
			}
			return nil, fmt.Errorf("plan validation failed after %d attempts:\n  %s",
				MaxRetries+1, strings.Join(errMsgs, "\n  "))
		}

		retryPrompt := BuildRetryPrompt(validationErrs)
		fmt.Fprintf(display, "\n⚠ Validation failed, retrying (%d/%d)...\n", attempt+1, MaxRetries)
		var err error
		sr, err = claudepkg.RunPromptStreamingResume(name, sessionID, retryPrompt, display)
		if err != nil {
			return nil, fmt.Errorf("claude retry failed: %w", err)
		}
	}

	return nil, fmt.Errorf("analysis failed")
}

// ParseSharedFileDiffs extracts and parses diffs for shared files.
func ParseSharedFileDiffs(base, source string, shared map[string][]string) ([]FileDiff, map[string]int, error) {
	var sharedPaths []string
	for f := range shared {
		sharedPaths = append(sharedPaths, f)
	}
	sort.Strings(sharedPaths)

	diff, err := gitpkg.DiffFiles(base, source, sharedPaths)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot extract diff for shared files: %w", err)
	}

	allFileDiffs := ParseDiff(diff)

	var fileDiffs []FileDiff
	hunkCounts := make(map[string]int)
	for _, fd := range allFileDiffs {
		if _, ok := shared[fd.Path]; ok {
			fileDiffs = append(fileDiffs, fd)
			hunkCounts[fd.Path] = len(fd.Hunks)
		}
	}

	return fileDiffs, hunkCounts, nil
}

// parseAndValidatePhase2 parses and validates Phase 2 hunk assignments with retries.
func parseAndValidatePhase2(result string, shared map[string][]string, hunkCounts map[string]int, sessionID, name string, display io.Writer) (*HunkAssignmentResponse, error) {
	sr := claudepkg.StreamResult{Result: result, SessionID: sessionID}

	for attempt := 0; attempt <= MaxRetries; attempt++ {
		resp, parseErr := ParseHunkAssignment(sr.Result)
		if parseErr != nil {
			if attempt == MaxRetries {
				return nil, fmt.Errorf("claude returned invalid hunk assignment after %d attempts: %w", MaxRetries+1, parseErr)
			}
			retryPrompt := fmt.Sprintf("Your response could not be parsed: %s\n\nPlease return valid YAML hunk assignments.", parseErr.Error())
			fmt.Fprintf(display, "\n⚠ Parse error, retrying (%d/%d)...\n", attempt+1, MaxRetries)
			var err error
			sr, err = claudepkg.RunPromptStreamingResume(name, sessionID, retryPrompt, display)
			if err != nil {
				return nil, fmt.Errorf("claude retry failed: %w", err)
			}
			continue
		}

		validationErrs := ValidateHunkAssignment(resp, shared, hunkCounts)
		if len(validationErrs) == 0 {
			return resp, nil
		}

		if attempt == MaxRetries {
			var errMsgs []string
			for _, e := range validationErrs {
				errMsgs = append(errMsgs, e.Error())
			}
			return nil, fmt.Errorf("hunk assignment validation failed after %d attempts:\n  %s",
				MaxRetries+1, strings.Join(errMsgs, "\n  "))
		}

		retryPrompt := ValidationSummary(validationErrs)
		fmt.Fprintf(display, "\n⚠ Hunk assignment validation failed, retrying (%d/%d)...\n", attempt+1, MaxRetries)
		var err error
		sr, err = claudepkg.RunPromptStreamingResume(name, sessionID, retryPrompt, display)
		if err != nil {
			return nil, fmt.Errorf("claude retry failed: %w", err)
		}
	}

	return nil, fmt.Errorf("hunk assignment failed")
}

// ReExtractPlan resumes a Claude session in print mode to re-extract
// the plan after an interactive refinement. Returns the parsed plan.
// changedFiles is used for Phase 1 validation of the re-extracted plan.
func ReExtractPlan(sessionID, name string, changedFiles []string, display io.Writer) (*Plan, error) {
	prompt := BuildReExtractPrompt()
	sr, err := claudepkg.RunPromptStreamingResume(name, sessionID, prompt, display)
	if err != nil {
		return nil, fmt.Errorf("plan re-extraction failed: %w", err)
	}

	plan, err := ParsePlan(sr.Result)
	if err != nil {
		// Retry once
		retryPrompt := fmt.Sprintf("Could not parse your response: %s\n\nPlease return ONLY the YAML plan.", err.Error())
		sr, err = claudepkg.RunPromptStreamingResume(name, sessionID, retryPrompt, display)
		if err != nil {
			return nil, fmt.Errorf("plan re-extraction retry failed: %w", err)
		}
		plan, err = ParsePlan(sr.Result)
		if err != nil {
			return nil, fmt.Errorf("cannot parse re-extracted plan: %w", err)
		}
	}

	validationErrs := ValidatePhase1(plan, changedFiles)
	if len(validationErrs) > 0 {
		var msgs []string
		for _, e := range validationErrs {
			msgs = append(msgs, e.Error())
		}
		return nil, fmt.Errorf("re-extracted plan has validation errors:\n  %s", strings.Join(msgs, "\n  "))
	}

	return plan, nil
}
