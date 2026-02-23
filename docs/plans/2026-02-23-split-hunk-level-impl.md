# Hunk-Level Decomposition Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extend `sdf split` to split individual files across layers at the diff hunk level via a two-phase Claude analysis.

**Architecture:** Phase 1 groups files into layers (relaxing the one-file-per-layer constraint). Phase 2 runs only for "shared files" (files in >1 layer) — sdf parses their diffs into numbered hunks and asks Claude to assign each hunk. The execution engine applies filtered patches per layer.

**Tech Stack:** Go 1.24, `gopkg.in/yaml.v3`, Claude CLI (streaming JSON), git diff/apply

**Design doc:** `docs/plans/2026-02-23-split-hunk-level-design.md`

---

### Task 1: Hunk Parsing — ParseDiff, FilterHunks, FormatNumberedHunks

**Files:**
- Create: `internal/split/hunk.go`
- Create: `internal/split/hunk_test.go`

**Step 1: Write failing tests in `internal/split/hunk_test.go`**

```go
package split

import (
	"strings"
	"testing"
)

const testDiffTwoHunks = `diff --git a/user.go b/user.go
index abc123..def456 100644
--- a/user.go
+++ b/user.go
@@ -1,5 +1,6 @@
 package models

 type User struct {
+	Email string
 	Name  string
 }
@@ -10,3 +11,7 @@ func (u *User) String() string {
 	return u.Name
 }
+
+func (u *User) Validate() error {
+	return nil
+}
`

const testDiffTwoFiles = `diff --git a/one.go b/one.go
index aaa..bbb 100644
--- a/one.go
+++ b/one.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"
 func main() {}
diff --git a/two.go b/two.go
index ccc..ddd 100644
--- a/two.go
+++ b/two.go
@@ -1,2 +1,3 @@
 package main
+func helper() {}
`

func TestParseDiff_SingleFileTwoHunks(t *testing.T) {
	files := ParseDiff(testDiffTwoHunks)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	fd := files[0]
	if fd.Path != "user.go" {
		t.Errorf("path: got %q, want %q", fd.Path, "user.go")
	}
	if len(fd.Hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(fd.Hunks))
	}
	if !strings.HasPrefix(fd.Hunks[0].Header, "@@ -1,5 +1,6") {
		t.Errorf("hunk 0 header: %q", fd.Hunks[0].Header)
	}
	if !strings.HasPrefix(fd.Hunks[1].Header, "@@ -10,3 +11,7") {
		t.Errorf("hunk 1 header: %q", fd.Hunks[1].Header)
	}
	if !strings.Contains(fd.Hunks[0].Body, "Email") {
		t.Error("hunk 0 body should contain Email")
	}
	if !strings.Contains(fd.Hunks[1].Body, "Validate") {
		t.Error("hunk 1 body should contain Validate")
	}
}

func TestParseDiff_TwoFiles(t *testing.T) {
	files := ParseDiff(testDiffTwoFiles)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].Path != "one.go" {
		t.Errorf("file 0 path: %q", files[0].Path)
	}
	if files[1].Path != "two.go" {
		t.Errorf("file 1 path: %q", files[1].Path)
	}
	if len(files[0].Hunks) != 1 {
		t.Errorf("file 0 hunks: got %d, want 1", len(files[0].Hunks))
	}
	if len(files[1].Hunks) != 1 {
		t.Errorf("file 1 hunks: got %d, want 1", len(files[1].Hunks))
	}
}

func TestParseDiff_Empty(t *testing.T) {
	files := ParseDiff("")
	if len(files) != 0 {
		t.Errorf("expected 0 files for empty diff, got %d", len(files))
	}
}

func TestFilterHunks_SelectOne(t *testing.T) {
	files := ParseDiff(testDiffTwoHunks)
	fd := files[0]

	// Select only hunk 1 (the Validate function)
	patch := FilterHunks(fd, []int{1})

	if !strings.Contains(patch, "diff --git") {
		t.Error("patch should have file header")
	}
	if !strings.Contains(patch, "Validate") {
		t.Error("patch should contain hunk 1 content")
	}
	if strings.Contains(patch, "Email") {
		t.Error("patch should NOT contain hunk 0 content")
	}
}

func TestFilterHunks_SelectAll(t *testing.T) {
	files := ParseDiff(testDiffTwoHunks)
	fd := files[0]

	all := FilterHunks(fd, []int{0, 1})
	// Should contain both hunks
	if !strings.Contains(all, "Email") {
		t.Error("all-hunks patch should contain hunk 0")
	}
	if !strings.Contains(all, "Validate") {
		t.Error("all-hunks patch should contain hunk 1")
	}
}

func TestFormatNumberedHunks(t *testing.T) {
	files := ParseDiff(testDiffTwoHunks)
	fd := files[0]

	formatted := FormatNumberedHunks(fd)
	if !strings.Contains(formatted, "Hunk 0:") {
		t.Error("should contain Hunk 0 label")
	}
	if !strings.Contains(formatted, "Hunk 1:") {
		t.Error("should contain Hunk 1 label")
	}
	if !strings.Contains(formatted, "Email") {
		t.Error("should contain hunk 0 content")
	}
	if !strings.Contains(formatted, "Validate") {
		t.Error("should contain hunk 1 content")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/split/ -run TestParseDiff -v && go test ./internal/split/ -run TestFilterHunks -v && go test ./internal/split/ -run TestFormatNumberedHunks -v`
Expected: FAIL — functions not defined

**Step 3: Implement `internal/split/hunk.go`**

```go
package split

import (
	"fmt"
	"strings"
)

// Hunk represents a single diff hunk within a file.
type Hunk struct {
	Header string // the @@ line (with trailing newline)
	Body   string // lines after the header, up to next hunk or file
}

// FileDiff represents all hunks for a single file in a unified diff.
type FileDiff struct {
	Header string // diff --git, index, ---, +++ lines (with newlines)
	Path   string // file path extracted from diff --git header
	Hunks  []Hunk
}

// ParseDiff splits a unified diff string into per-file sections with numbered hunks.
func ParseDiff(diff string) []FileDiff {
	if diff == "" {
		return nil
	}

	lines := strings.Split(diff, "\n")
	// Remove trailing empty string from Split if diff ends with newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var result []FileDiff
	var current *FileDiff
	var currentHunk *Hunk

	flush := func() {
		if currentHunk != nil && current != nil {
			current.Hunks = append(current.Hunks, *currentHunk)
			currentHunk = nil
		}
		if current != nil {
			result = append(result, *current)
			current = nil
		}
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			current = &FileDiff{Header: line + "\n"}
			parts := strings.SplitN(line, " ", 4)
			if len(parts) >= 3 {
				current.Path = strings.TrimPrefix(parts[2], "a/")
			}
			continue
		}

		if strings.HasPrefix(line, "@@") && current != nil {
			if currentHunk != nil {
				current.Hunks = append(current.Hunks, *currentHunk)
			}
			currentHunk = &Hunk{Header: line + "\n"}
			continue
		}

		if currentHunk != nil {
			currentHunk.Body += line + "\n"
		} else if current != nil {
			current.Header += line + "\n"
		}
	}

	flush()
	return result
}

// FilterHunks reconstructs a valid patch containing only the specified hunks
// from a FileDiff. The returned string is suitable for git apply.
func FilterHunks(fd FileDiff, indices []int) string {
	var b strings.Builder
	b.WriteString(fd.Header)
	for _, idx := range indices {
		if idx >= 0 && idx < len(fd.Hunks) {
			b.WriteString(fd.Hunks[idx].Header)
			b.WriteString(fd.Hunks[idx].Body)
		}
	}
	return b.String()
}

// FormatNumberedHunks formats a file's hunks with numbered labels,
// suitable for including in the hunk assignment prompt.
func FormatNumberedHunks(fd FileDiff) string {
	var b strings.Builder
	for i, h := range fd.Hunks {
		fmt.Fprintf(&b, "Hunk %d:\n", i)
		b.WriteString(h.Header)
		b.WriteString(h.Body)
	}
	return b.String()
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/split/ -run "TestParseDiff|TestFilterHunks|TestFormatNumberedHunks" -v`
Expected: PASS (all 6 tests)

**Step 5: Commit**

```bash
git add internal/split/hunk.go internal/split/hunk_test.go
git commit -m "feat(split): add hunk parsing — ParseDiff, FilterHunks, FormatNumberedHunks"
```

---

### Task 2: Plan Types Extension — PartialFile, HunkAssignment, Helpers

**Files:**
- Modify: `internal/split/plan.go`
- Modify: `internal/split/plan_test.go`

**Step 1: Add new types and helpers to `internal/split/plan.go`**

Add after the `Layer` struct (after line 23):

```go
// PartialFile represents a file that is split across layers at hunk level.
// Only the specified hunk indices belong to this layer.
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
	File        string           `yaml:"file"`
	Assignments []HunkToLayer    `yaml:"assignments"`
}

// HunkToLayer assigns a single hunk index to a layer name.
type HunkToLayer struct {
	Hunk  int    `yaml:"hunk"`
	Layer string `yaml:"layer"`
}
```

Update the `Layer` struct to add `PartialFiles`:

```go
type Layer struct {
	Name         string        `yaml:"name"`
	Description  string        `yaml:"description"`
	Files        []string      `yaml:"files"`
	PartialFiles []PartialFile `yaml:"partial_files,omitempty"`
}
```

Add helper methods:

```go
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
```

**Step 2: Add tests for helpers to `internal/split/plan_test.go`**

Append these tests:

```go
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
```

**Step 3: Run tests**

Run: `go test ./internal/split/ -run "TestAllFilePaths|TestSharedFiles" -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/split/plan.go internal/split/plan_test.go
git commit -m "feat(split): add PartialFile type, SharedFiles detection, AllFilePaths helper"
```

---

### Task 3: New Validation Functions

**Files:**
- Modify: `internal/split/plan.go`
- Modify: `internal/split/plan_test.go`

**Step 1: Write failing tests in `internal/split/plan_test.go`**

Append:

```go
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
					// Hunk 1 is missing
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
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/split/ -run "TestValidatePhase1|TestValidateHunkAssignment" -v`
Expected: FAIL — functions not defined

**Step 3: Implement validation functions in `internal/split/plan.go`**

Add after `ValidatePlan`:

```go
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

	// Completeness — every changed file must appear in at least one layer
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

	// Build set of valid layers per file
	validLayers := make(map[string]map[string]bool)
	for file, layers := range shared {
		validLayers[file] = make(map[string]bool)
		for _, l := range layers {
			validLayers[file][l] = true
		}
	}

	// Track which hunks have been assigned
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
			// Valid index
			if a.Hunk < 0 || a.Hunk >= maxHunk {
				errs = append(errs, fmt.Errorf("hunk %d for %s is out of range (0-%d)", a.Hunk, fa.File, maxHunk-1))
				continue
			}
			// Valid layer
			if !validLayers[fa.File][a.Layer] {
				errs = append(errs, fmt.Errorf("hunk %d for %s assigned to %q which didn't list the file", a.Hunk, fa.File, a.Layer))
				continue
			}
			// No duplicates
			if prev, ok := assigned[fa.File][a.Hunk]; ok {
				errs = append(errs, fmt.Errorf("hunk %d for %s assigned to both %q and %q", a.Hunk, fa.File, prev, a.Layer))
				continue
			}
			assigned[fa.File][a.Hunk] = a.Layer
		}
	}

	// Completeness — every hunk must be assigned
	for file, count := range hunkCounts {
		for i := 0; i < count; i++ {
			if _, ok := assigned[file][i]; !ok {
				errs = append(errs, fmt.Errorf("hunk %d for %s is not assigned to any layer", i, file))
			}
		}
	}

	return errs
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/split/ -run "TestValidatePhase1|TestValidateHunkAssignment" -v`
Expected: PASS (all 7 tests)

**Step 5: Run ALL split tests to verify no regressions**

Run: `go test ./internal/split/ -v -count=1`
Expected: ALL existing tests still pass

**Step 6: Commit**

```bash
git add internal/split/plan.go internal/split/plan_test.go
git commit -m "feat(split): add ValidatePhase1 and ValidateHunkAssignment"
```

---

### Task 4: AI Prompt Updates — BuildHunkPrompt, ParseHunkAssignment, MergePlan

**Files:**
- Modify: `internal/split/ai.go`
- Modify: `internal/split/ai_test.go`

**Step 1: Write failing tests in `internal/split/ai_test.go`**

Append:

```go
func TestBuildPrompt_AllowsSharedFiles(t *testing.T) {
	prompt := BuildPrompt("feature/big-change", "main")

	// Should mention that files may appear in multiple layers
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
	if !strings.Contains(prompt, "db") && !strings.Contains(prompt, "api") {
		t.Error("prompt should mention layer names")
	}
	if !strings.Contains(prompt, "hunk_assignments") {
		t.Error("prompt should show expected YAML format")
	}
}

func TestParseHunkAssignment(t *testing.T) {
	text := `Here are the assignments:
` + "```yaml" + `
hunk_assignments:
  - file: user.go
    assignments:
      - hunk: 0
        layer: db
      - hunk: 1
        layer: api
` + "```"

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
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/split/ -run "TestBuildHunkPrompt|TestParseHunkAssignment|TestMergePlan" -v`
Expected: FAIL — functions not defined

**Step 3: Update `BuildPrompt` in `internal/split/ai.go`**

Replace the two rules about file assignment (lines 35-40 in the prompt string):

Old:
```
- Every changed file must appear in exactly one layer
...
- A file can only belong to one layer. If a file has changes for multiple concerns, put it in the earliest layer that needs it
```

New:
```
- Every changed file must appear in at least one layer
- A file MAY appear in multiple layers if its changes clearly serve different concerns.
  When this happens, we'll assign individual hunks in a follow-up step.
- If all of a file's changes belong to one concern, list it in only one layer
```

**Step 4: Add `BuildHunkPrompt`, `ParseHunkAssignment`, `MergePlan` to `internal/split/ai.go`**

```go
// BuildHunkPrompt constructs the Phase 2 prompt for hunk assignment.
// sharedFiles maps file path → layer names that listed it.
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
	// Build lookup: file → layer → hunk indices
	layerHunks := make(map[string]map[string][]int) // file → layer → hunks
	for _, fa := range resp.HunkAssignments {
		if layerHunks[fa.File] == nil {
			layerHunks[fa.File] = make(map[string][]int)
		}
		for _, a := range fa.Assignments {
			layerHunks[fa.File][a.Layer] = append(layerHunks[fa.File][a.Layer], a.Hunk)
		}
	}

	// Build set of shared file paths
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

		// Split files into whole vs partial
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
```

Note: Add `"gopkg.in/yaml.v3"` to ai.go imports if not already present (it's needed for `ParseHunkAssignment`). The existing import is in plan.go, so ai.go needs its own.

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/split/ -run "TestBuildPrompt_AllowsSharedFiles|TestBuildHunkPrompt|TestParseHunkAssignment|TestMergePlan" -v`
Expected: PASS (all 4 new tests + existing TestBuildPrompt still passes)

**Step 6: Run ALL split tests**

Run: `go test ./internal/split/ -v -count=1`
Expected: ALL tests pass

**Step 7: Commit**

```bash
git add internal/split/ai.go internal/split/ai_test.go
git commit -m "feat(split): add BuildHunkPrompt, ParseHunkAssignment, MergePlan for Phase 2"
```

---

### Task 5: Two-Phase Analyze Flow

**Files:**
- Modify: `internal/split/ai.go`

This task updates the `Analyze` function to support two-phase analysis. Phase 2 is triggered when shared files are detected after Phase 1.

**Step 1: Rewrite `Analyze` in `internal/split/ai.go`**

Replace the entire `Analyze` function with:

```go
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

	fileDiffs, hunkCounts, err := parseSharedFileDiffs(base, fromBranch, shared)
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

// parseSharedFileDiffs extracts and parses diffs for shared files.
func parseSharedFileDiffs(base, source string, shared map[string][]string) ([]FileDiff, map[string]int, error) {
	var sharedPaths []string
	for f := range shared {
		sharedPaths = append(sharedPaths, f)
	}

	diff, err := gitpkg.DiffFiles(base, source, sharedPaths)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot extract diff for shared files: %w", err)
	}

	allFileDiffs := ParseDiff(diff)

	// Filter to only shared files and build hunk counts
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
```

**Step 2: Verify the build compiles**

Run: `go build ./internal/split/`
Expected: Success

**Step 3: Run ALL split tests (no new tests needed — Analyze is tested via integration)**

Run: `go test ./internal/split/ -v -count=1`
Expected: ALL tests pass

**Step 4: Commit**

```bash
git add internal/split/ai.go
git commit -m "feat(split): update Analyze for two-phase flow with hunk assignment"
```

---

### Task 6: Execute with PartialFiles

**Files:**
- Modify: `internal/split/execute.go`
- Modify: `internal/split/execute_test.go`

**Step 1: Write failing test in `internal/split/execute_test.go`**

Add a test repo helper and test that exercises hunk-level splitting:

```go
// testRepoForHunks creates a temp repo where one file has two independent
// regions of change, suitable for hunk-level splitting.
func testRepoForHunks(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(origDir) })
	os.Chdir(dir)

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), out)
		}
	}

	write := func(name, content string) {
		t.Helper()
		full := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(full), 0755)
		os.WriteFile(full, []byte(content), 0644)
	}

	git("init", "-b", "main")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "Test")
	git("config", "commit.gpgsign", "false")

	// Create a file with two functions (enough content for 2 hunks)
	write("shared.go", `package main

func Alpha() string {
	return "alpha"
}

// separator to ensure two hunks

func Beta() string {
	return "beta"
}
`)
	write("only-a.go", "package main\n")
	write("only-b.go", "package main\n")
	git("add", ".")
	git("commit", "-m", "initial")

	git("checkout", "-b", "feature")

	// Modify two separate regions of shared.go + modify only-a.go and only-b.go
	write("shared.go", `package main

func Alpha() string {
	return "alpha-modified"
}

// separator to ensure two hunks

func Beta() string {
	return "beta-modified"
}
`)
	write("only-a.go", "package main\nfunc A() {}\n")
	write("only-b.go", "package main\nfunc B() {}\n")
	git("add", ".")
	git("commit", "-m", "modify all")

	return dir
}

func TestExecute_WithPartialFiles(t *testing.T) {
	dir := testRepoForHunks(t)

	// First, parse the diff to know hunk indices
	diff, err := gitpkg.DiffFiles("main", "feature", []string{"shared.go"})
	if err != nil {
		t.Fatalf("DiffFiles: %v", err)
	}
	fileDiffs := ParseDiff(diff)
	if len(fileDiffs) != 1 {
		t.Fatalf("expected 1 file diff, got %d", len(fileDiffs))
	}
	if len(fileDiffs[0].Hunks) < 2 {
		t.Fatalf("expected at least 2 hunks, got %d", len(fileDiffs[0].Hunks))
	}

	plan := &Plan{
		Layers: []Layer{
			{
				Name:        "layer-a",
				Description: "Alpha changes",
				Files:       []string{"only-a.go"},
				PartialFiles: []PartialFile{
					{Path: "shared.go", Hunks: []int{0}},
				},
			},
			{
				Name:        "layer-b",
				Description: "Beta changes",
				Files:       []string{"only-b.go"},
				PartialFiles: []PartialFile{
					{Path: "shared.go", Hunks: []int{1}},
				},
			},
		},
	}

	branches, err := Execute(plan, "hunk-stack", "main", "feature", dir)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(branches) != 2 {
		t.Fatalf("expected 2 branches, got %d", len(branches))
	}

	// Tree identity: last branch should match feature
	lastBranch := branches[len(branches)-1]
	treeErr := ValidateTree("feature", lastBranch)
	if treeErr != nil {
		diffOut, _ := gitpkg.DiffFull("feature", lastBranch)
		t.Fatalf("tree identity failed: %v\ndiff:\n%s", treeErr, diffOut)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/split/ -run TestExecute_WithPartialFiles -v`
Expected: FAIL (Execute doesn't handle PartialFiles yet)

**Step 3: Update `Execute` in `internal/split/execute.go`**

Update the layer loop inside `Execute` to handle both whole files and partial files. Replace the section from `// Extract diff for this layer's files` through `// Stage and commit` (lines 50-72) with:

```go
		// Build the combined patch for this layer
		var patchParts []string

		// Whole files — extract their full diff
		if len(layer.Files) > 0 {
			wholePatch, err := gitpkg.DiffFiles(base, source, layer.Files)
			if err != nil {
				return createdBranches, fmt.Errorf("cannot extract diff for %s: %w", layer.Name, err)
			}
			if wholePatch != "" {
				patchParts = append(patchParts, wholePatch)
			}
		}

		// Partial files — extract and filter hunks
		for _, pf := range layer.PartialFiles {
			fileDiff, err := gitpkg.DiffFiles(base, source, []string{pf.Path})
			if err != nil {
				return createdBranches, fmt.Errorf("cannot extract diff for %s in %s: %w", pf.Path, layer.Name, err)
			}
			parsed := ParseDiff(fileDiff)
			if len(parsed) == 0 {
				return createdBranches, fmt.Errorf("no diff found for partial file %s in layer %s", pf.Path, layer.Name)
			}
			filtered := FilterHunks(parsed[0], pf.Hunks)
			if filtered != "" {
				patchParts = append(patchParts, filtered)
			}
		}

		if len(patchParts) == 0 {
			return createdBranches, fmt.Errorf("empty diff for layer %s — no changes to apply", layer.Name)
		}

		patch := strings.Join(patchParts, "")

		// Apply the patch
		if err := gitpkg.ApplyPatch(patch); err != nil {
			return createdBranches, fmt.Errorf("apply failed for %s: %w", layer.Name, err)
		}

		// Stage and commit all files (whole + partial)
		allFiles := layer.AllFilePaths()
		if err := gitpkg.Add(allFiles...); err != nil {
			return createdBranches, fmt.Errorf("cannot stage files for %s: %w", layer.Name, err)
		}

		if err := gitpkg.Commit(layer.Description); err != nil {
			return createdBranches, fmt.Errorf("cannot commit %s: %w", layer.Name, err)
		}
```

Add `"strings"` to the import list in execute.go.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/split/ -run TestExecute_WithPartialFiles -v`
Expected: PASS

**Step 5: Run ALL execute tests + full split tests**

Run: `go test ./internal/split/ -v -count=1`
Expected: ALL tests pass (existing tests still work — they only use `Files`, no `PartialFiles`)

**Step 6: Commit**

```bash
git add internal/split/execute.go internal/split/execute_test.go
git commit -m "feat(split): Execute handles PartialFiles with hunk-filtered patches"
```

---

### Task 7: Display Update + Integration Verification

**Files:**
- Modify: `cmd/split.go`

**Step 1: Update `displaySplitPlan` in `cmd/split.go`**

Replace the `displaySplitPlan` function (lines 247-273) with:

```go
func displaySplitPlan(plan *splitpkg.Plan, stackName, base, source string) {
	fmt.Printf("\nSplit plan for %s (base: %s)\n", ui.Branch(stackName), ui.Branch(base))
	fmt.Println(strings.Repeat("─", 50))

	totalFiles := 0
	totalPartial := 0
	for i, layer := range plan.Layers {
		wholeCount := len(layer.Files)
		partialCount := len(layer.PartialFiles)
		fileCount := wholeCount + partialCount
		totalFiles += fileCount
		totalPartial += partialCount

		// Try to get line stats for whole files
		lineInfo := ""
		allPaths := layer.AllFilePaths()
		if len(allPaths) > 0 {
			diff, err := gitpkg.DiffFiles(base, source, allPaths)
			if err == nil {
				adds, dels := countDiffLines(diff)
				if adds > 0 || dels > 0 {
					lineInfo = fmt.Sprintf(", +%d -%d", adds, dels)
				}
			}
		}

		fmt.Printf("\n  Layer %d: %s (%d files%s)\n",
			i+1, ui.Bold.Render(layer.Name), fileCount, lineInfo)
		fmt.Printf("    %s\n", layer.Description)

		// Show shared files
		for _, pf := range layer.PartialFiles {
			fmt.Printf("    Shared: %s (hunks %s)\n", pf.Path, formatHunkIndices(pf.Hunks))
		}
	}

	summary := fmt.Sprintf("  Total: %d files across %d layers", totalFiles, len(plan.Layers))
	if totalPartial > 0 {
		// Count unique partial file paths
		seen := make(map[string]bool)
		for _, layer := range plan.Layers {
			for _, pf := range layer.PartialFiles {
				seen[pf.Path] = true
			}
		}
		summary += fmt.Sprintf(" (%d file(s) split at hunk level)", len(seen))
	}
	fmt.Printf("\n%s\n\n", summary)
}

// formatHunkIndices formats a slice of ints as a comma-separated string.
func formatHunkIndices(hunks []int) string {
	parts := make([]string, len(hunks))
	for i, h := range hunks {
		parts[i] = fmt.Sprintf("%d", h)
	}
	return strings.Join(parts, ", ")
}
```

Note: the line stats now use `AllFilePaths()` which includes partial file paths. This gives an approximate line count (it includes all hunks of partial files, not just the assigned ones). This is acceptable for display — exact per-hunk counting can be added later.

**Step 2: Build everything**

Run: `go build ./...`
Expected: Success

**Step 3: Run vet**

Run: `go vet ./...`
Expected: Clean

**Step 4: Run ALL tests**

Run: `go test ./... -count=1`
Expected: ALL tests pass

**Step 5: Verify the command help**

Run: `go run . split --help`
Expected: Shows same help as before (no CLI changes)

**Step 6: Commit**

```bash
git add cmd/split.go
git commit -m "feat(split): update displaySplitPlan for hunk-level shared files"
```

---

### Summary of changes

| File | Change |
|------|--------|
| `internal/split/hunk.go` | **New** — Hunk, FileDiff, ParseDiff, FilterHunks, FormatNumberedHunks |
| `internal/split/hunk_test.go` | **New** — 6 tests |
| `internal/split/plan.go` | **Modified** — PartialFile, HunkAssignment types, AllFilePaths, SharedFiles, ValidatePhase1, ValidateHunkAssignment |
| `internal/split/plan_test.go` | **Modified** — 10 new tests |
| `internal/split/ai.go` | **Modified** — Two-phase Analyze, BuildHunkPrompt, ParseHunkAssignment, MergePlan |
| `internal/split/ai_test.go` | **Modified** — 5 new tests |
| `internal/split/execute.go` | **Modified** — PartialFiles support in Execute |
| `internal/split/execute_test.go` | **Modified** — 1 new hunk-level test |
| `cmd/split.go` | **Modified** — displaySplitPlan shows shared files |
