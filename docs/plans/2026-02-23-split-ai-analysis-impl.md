# sdf split — Claude-Powered Analysis Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Rewrite `sdf split` to use agentic Claude for commit analysis and file-based diff+apply execution, replacing the directory-affinity heuristic and cherry-pick engine.

**Architecture:** Claude CLI is invoked as a streaming subprocess. It explores the repo via git tools and returns a YAML plan assigning files to layers. sdf validates the plan, then executes it by extracting per-layer diffs and applying them as clean commits on stacked branches. Session ID is captured for resumability.

**Tech Stack:** Go, Claude CLI (stream-json), gopkg.in/yaml.v3, existing sdf internal packages (git, gh, claude, stack, config, ui)

---

### Task 1: Add YAML dependency

**Files:**
- Modify: `go.mod`

**Step 1: Add gopkg.in/yaml.v3**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go get gopkg.in/yaml.v3`

**Step 2: Tidy**

Run: `go mod tidy`

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "Add gopkg.in/yaml.v3 dependency for split plan parsing"
```

---

### Task 2: Add git helpers — DiffFiles and ApplyPatch

**Files:**
- Modify: `internal/git/git.go` (append after line 325)
- Create: `internal/git/git_split_test.go`

**Step 1: Write the failing tests**

Create `internal/git/git_split_test.go`:

```go
package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestRepo creates a temp git repo with a main branch and a feature branch.
// Feature branch has changes in two files across two directories.
// Returns the repo dir. Caller must os.Chdir back.
func setupTestRepo(t *testing.T) string {
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

	write("README.md", "# test\n")
	git("add", ".")
	git("commit", "-m", "initial")

	git("checkout", "-b", "feature")

	write("src/foo.go", "package src\nfunc Foo() {}\n")
	write("src/bar.go", "package src\nfunc Bar() {}\n")
	write("lib/util.go", "package lib\nfunc Util() {}\n")
	git("add", ".")
	git("commit", "-m", "add src and lib files")

	return dir
}

func TestDiffNameOnly(t *testing.T) {
	setupTestRepo(t)

	files, err := DiffNameOnly("main", "feature")
	if err != nil {
		t.Fatalf("DiffNameOnly: %v", err)
	}

	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d: %v", len(files), files)
	}

	want := map[string]bool{"src/foo.go": true, "src/bar.go": true, "lib/util.go": true}
	for _, f := range files {
		if !want[f] {
			t.Errorf("unexpected file: %s", f)
		}
	}
}

func TestDiffFiles(t *testing.T) {
	setupTestRepo(t)

	// Get diff for only src/ files
	patch, err := DiffFiles("main", "feature", []string{"src/foo.go", "src/bar.go"})
	if err != nil {
		t.Fatalf("DiffFiles: %v", err)
	}

	if !strings.Contains(patch, "src/foo.go") {
		t.Error("patch should contain src/foo.go")
	}
	if !strings.Contains(patch, "src/bar.go") {
		t.Error("patch should contain src/bar.go")
	}
	if strings.Contains(patch, "lib/util.go") {
		t.Error("patch should NOT contain lib/util.go")
	}
}

func TestApplyPatch(t *testing.T) {
	setupTestRepo(t)

	// Get patch for src/ files
	patch, err := DiffFiles("main", "feature", []string{"src/foo.go"})
	if err != nil {
		t.Fatalf("DiffFiles: %v", err)
	}

	// Go back to main and apply
	if err := Checkout("main"); err != nil {
		t.Fatalf("checkout main: %v", err)
	}

	if err := ApplyPatch(patch); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}

	// Verify file exists
	content, err := os.ReadFile("src/foo.go")
	if err != nil {
		t.Fatalf("src/foo.go should exist after apply: %v", err)
	}
	if !strings.Contains(string(content), "Foo") {
		t.Error("src/foo.go should contain Foo()")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go test ./internal/git/ -run 'TestDiffNameOnly|TestDiffFiles|TestApplyPatch' -v -count=1`
Expected: compilation errors (functions not defined)

**Step 3: Implement the helpers**

Add to `internal/git/git.go` after the `DeleteBranch` function (after line 308):

```go
// DiffNameOnly returns the list of files changed between two refs.
func DiffNameOnly(from, to string) ([]string, error) {
	out, err := run("diff", "--name-only", from+"..."+to)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// DiffFiles returns the diff between two refs for a specific set of files.
// The result is a patch suitable for git apply.
func DiffFiles(from, to string, files []string) (string, error) {
	args := []string{"diff", from + "..." + to, "--"}
	args = append(args, files...)
	return run(args...)
}

// ApplyPatch applies a patch string using git apply --3way.
// The patch is written to a temp file and applied.
func ApplyPatch(patch string) error {
	f, err := os.CreateTemp("", "sdf-patch-*.patch")
	if err != nil {
		return fmt.Errorf("cannot create temp file: %w", err)
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString(patch); err != nil {
		f.Close()
		return fmt.Errorf("cannot write patch: %w", err)
	}
	f.Close()

	_, err = run("apply", "--3way", f.Name())
	return err
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go test ./internal/git/ -run 'TestDiffNameOnly|TestDiffFiles|TestApplyPatch' -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/git.go internal/git/git_split_test.go
git commit -m "Add DiffNameOnly, DiffFiles, ApplyPatch git helpers for file-based splitting"
```

---

### Task 3: Extend Claude streaming to capture session_id

**Files:**
- Modify: `internal/claude/claude.go:54-121`

**Step 1: Write the test**

This is hard to unit-test without a real Claude CLI. Instead, test the event parsing logic by extracting it. Add to `internal/claude/claude.go`:

We'll refactor `RunPromptStreaming` to return a `StreamResult` struct that includes both the result text and session ID. We also add `RunPromptStreamingResume` for retry with `--resume`.

**Step 2: Implement the changes**

Replace `RunPromptStreaming` in `internal/claude/claude.go` (lines 54-121) with:

```go
// StreamResult holds the output of a streaming Claude invocation.
type StreamResult struct {
	Result    string // final response text
	SessionID string // session ID for resumption
}

// RunPromptStreaming sends a prompt to Claude using stream-json output format
// with partial messages enabled, displaying text in real-time via display writer
// while capturing the full response and session ID.
func RunPromptStreaming(name, prompt string, display io.Writer) (StreamResult, error) {
	args := []string{"-p", "--verbose",
		"--output-format", "stream-json",
		"--include-partial-messages",
		prompt}
	return runStreaming(name, args, display)
}

// RunPromptStreamingResume resumes a previous session with a new prompt,
// streaming output and capturing the response.
func RunPromptStreamingResume(name, sessionID, prompt string, display io.Writer) (StreamResult, error) {
	args := []string{"--resume", sessionID,
		"-p", "--verbose",
		"--output-format", "stream-json",
		"--include-partial-messages",
		prompt}
	return runStreaming(name, args, display)
}

// runStreaming is the shared implementation for streaming Claude invocations.
func runStreaming(name string, args []string, display io.Writer) (StreamResult, error) {
	cmd := exec.Command(Binary, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return StreamResult{}, fmt.Errorf("claude %s: %w", name, err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return StreamResult{}, fmt.Errorf("claude %s: %w", name, err)
	}

	var sr StreamResult
	var displayedLen int
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event struct {
			Type      string `json:"type"`
			SessionID string `json:"session_id"`
			Message   struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
			Result string `json:"result"`
		}

		if json.Unmarshal(line, &event) != nil {
			continue
		}

		// Capture session_id from any event that has it
		if event.SessionID != "" && sr.SessionID == "" {
			sr.SessionID = event.SessionID
		}

		// Display incremental text from partial assistant messages
		if event.Type == "assistant" && len(event.Message.Content) > 0 {
			text := event.Message.Content[0].Text
			if len(text) > displayedLen {
				display.Write([]byte(text[displayedLen:]))
				displayedLen = len(text)
			}
		}

		// Capture the final result text
		if event.Type == "result" {
			if event.Result != "" {
				sr.Result = event.Result
			}
			if event.SessionID != "" {
				sr.SessionID = event.SessionID
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		return sr, fmt.Errorf("claude %s: failed", name)
	}

	sr.Result = strings.TrimSpace(sr.Result)
	return sr, nil
}
```

**Step 3: Update all callers of RunPromptStreaming**

Search for callers. Currently `RunPromptStreaming` is not called anywhere in the codebase (it exists but is unused — only `RunPrompt` is used in sync.go and move.go). So this is a safe change with no callers to update.

**Step 4: Verify compilation**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go build ./...`
Expected: compiles cleanly

**Step 5: Commit**

```bash
git add internal/claude/claude.go
git commit -m "Extend Claude streaming to return session_id and add resume support"
```

---

### Task 4: Add split_sessions to LocalState

**Files:**
- Modify: `internal/stack/stack.go:43-45`

**Step 1: Add the field**

In `internal/stack/stack.go`, extend the `LocalState` struct (line 43):

```go
type LocalState struct {
	SyncProgress  *SyncProgress     `json:"sync_progress,omitempty"`
	SplitSessions map[string]string `json:"split_sessions,omitempty"` // stack_name → session_id
}
```

**Step 2: Verify compilation**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go build ./...`
Expected: compiles cleanly

**Step 3: Run existing tests**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go test ./internal/stack/ -v -count=1`
Expected: PASS (no behavior change, just a new optional JSON field)

**Step 4: Commit**

```bash
git add internal/stack/stack.go
git commit -m "Add split_sessions field to LocalState for Claude session resumability"
```

---

### Task 5: Create internal/split/plan.go — Plan structs and validation

**Files:**
- Create: `internal/split/plan.go`
- Create: `internal/split/plan_test.go`

**Step 1: Write the failing tests**

Create `internal/split/plan_test.go`:

```go
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
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go test ./internal/split/ -v -count=1`
Expected: compilation error (package doesn't exist)

**Step 3: Implement plan.go**

Create `internal/split/plan.go`:

```go
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
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go test ./internal/split/ -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/split/plan.go internal/split/plan_test.go
git commit -m "Add split plan parsing and validation with comprehensive tests"
```

---

### Task 6: Create internal/split/ai.go — Claude analysis with streaming and retry

**Files:**
- Create: `internal/split/ai.go`
- Create: `internal/split/ai_test.go`

**Step 1: Write the tests**

Create `internal/split/ai_test.go`:

```go
package split

import (
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
	if !strings.Contains(prompt, "```yaml") {
		t.Error("prompt should show expected YAML format")
	}
	if !strings.Contains(prompt, "layers:") {
		t.Error("prompt should show layers structure")
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
```

Note: add `"fmt"` to the imports in the test file.

**Step 2: Run tests to verify they fail**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go test ./internal/split/ -run 'TestBuildPrompt|TestBuildRetryPrompt' -v -count=1`
Expected: compilation error

**Step 3: Implement ai.go**

Create `internal/split/ai.go`:

```go
package split

import (
	"fmt"
	"io"
	"strings"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
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
- Every changed file must appear in exactly one layer
- Order layers so earlier layers don't depend on later ones
- Aim for <500 lines of diff per layer when practical
- Group test files with the production code they test
- Use short, descriptive kebab-case layer names
- A file can only belong to one layer. If a file has changes for multiple concerns, put it in the earliest layer that needs it

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

// Analyze invokes Claude to analyze a branch and produce a split plan.
// It streams Claude's progress to the display writer, retries up to MaxRetries
// times on validation failure, and returns the validated plan with session ID.
func Analyze(fromBranch, base string, display io.Writer) (*AnalysisResult, error) {
	// Get the list of changed files for validation
	changedFiles, err := gitpkg.DiffNameOnly(base, fromBranch)
	if err != nil {
		return nil, fmt.Errorf("cannot list changed files: %w", err)
	}

	prompt := BuildPrompt(fromBranch, base)
	name := "split-analysis"

	// First attempt
	sr, err := claudepkg.RunPromptStreaming(name, prompt, display)
	if err != nil {
		return nil, fmt.Errorf("claude analysis failed: %w", err)
	}

	sessionID := sr.SessionID

	for attempt := 0; attempt <= MaxRetries; attempt++ {
		plan, parseErr := ParsePlan(sr.Result)
		if parseErr != nil {
			if attempt == MaxRetries {
				return nil, fmt.Errorf("claude returned invalid plan after %d attempts: %w", MaxRetries+1, parseErr)
			}
			retryPrompt := fmt.Sprintf("Your response could not be parsed: %s\n\nPlease return a valid YAML plan.", parseErr.Error())
			fmt.Fprintf(display, "\n⚠ Parse error, retrying (%d/%d)...\n", attempt+1, MaxRetries)
			sr, err = claudepkg.RunPromptStreamingResume(name, sessionID, retryPrompt, display)
			if err != nil {
				return nil, fmt.Errorf("claude retry failed: %w", err)
			}
			continue
		}

		validationErrs := ValidatePlan(plan, changedFiles)
		if len(validationErrs) == 0 {
			return &AnalysisResult{Plan: plan, SessionID: sessionID}, nil
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
		sr, err = claudepkg.RunPromptStreamingResume(name, sessionID, retryPrompt, display)
		if err != nil {
			return nil, fmt.Errorf("claude retry failed: %w", err)
		}
	}

	// Unreachable, but satisfy the compiler
	return nil, fmt.Errorf("analysis failed")
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go test ./internal/split/ -run 'TestBuildPrompt|TestBuildRetryPrompt' -v -count=1`
Expected: PASS

**Step 5: Verify full compilation**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go build ./...`
Expected: compiles cleanly

**Step 6: Commit**

```bash
git add internal/split/ai.go internal/split/ai_test.go
git commit -m "Add Claude analysis with streaming, retry, and session capture"
```

---

### Task 7: Create internal/split/execute.go — File-based execution engine

**Files:**
- Create: `internal/split/execute.go`
- Create: `internal/split/execute_test.go`

**Step 1: Write the failing tests**

Create `internal/split/execute_test.go`:

```go
package split

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

// testRepoForExec creates a temp repo with main branch and a feature branch
// that has changes in 3 directories: src/ (2 files), lib/ (1 file), cmd/ (1 file).
func testRepoForExec(t *testing.T) string {
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

	write("README.md", "# test\n")
	git("add", ".")
	git("commit", "-m", "initial")

	git("checkout", "-b", "feature")

	write("src/foo.go", "package src\nfunc Foo() {}\n")
	write("src/bar.go", "package src\nfunc Bar() {}\n")
	write("lib/util.go", "package lib\nfunc Util() {}\n")
	write("cmd/main.go", "package main\nfunc main() {}\n")
	git("add", ".")
	git("commit", "-m", "add all files")

	return dir
}

func TestExecute_ThreeLayers(t *testing.T) {
	dir := testRepoForExec(t)

	plan := &Plan{
		Layers: []Layer{
			{Name: "core-lib", Description: "Core library utilities", Files: []string{"lib/util.go"}},
			{Name: "src-logic", Description: "Source logic", Files: []string{"src/foo.go", "src/bar.go"}},
			{Name: "cli", Description: "CLI entry point", Files: []string{"cmd/main.go"}},
		},
	}

	branches, err := Execute(plan, "test-stack", "main", "feature", dir)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(branches) != 3 {
		t.Fatalf("expected 3 branches, got %d", len(branches))
	}

	// Tree identity: last branch should match feature
	lastBranch := branches[len(branches)-1]
	diff, err := gitpkg.DiffFull("feature", lastBranch)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if diff != "" {
		t.Errorf("tree identity failed — diff:\n%s", diff)
	}

	// Stack topology
	s, err := stack.LoadStack(dir, "test-stack")
	if err != nil {
		t.Fatalf("load stack: %v", err)
	}

	if len(s.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(s.Nodes))
	}
	if s.Base != "main" {
		t.Errorf("stack base: got %q, want %q", s.Base, "main")
	}
	if s.ParentBranch(s.Nodes[0].Branch) != "main" {
		t.Errorf("node 0 parent: got %q, want %q", s.ParentBranch(s.Nodes[0].Branch), "main")
	}
	if s.ParentBranch(s.Nodes[1].Branch) != s.Nodes[0].Branch {
		t.Errorf("node 1 parent: got %q, want %q", s.ParentBranch(s.Nodes[1].Branch), s.Nodes[0].Branch)
	}
	if s.ParentBranch(s.Nodes[2].Branch) != s.Nodes[1].Branch {
		t.Errorf("node 2 parent: got %q, want %q", s.ParentBranch(s.Nodes[2].Branch), s.Nodes[1].Branch)
	}
}

func TestExecute_TreeIdentity(t *testing.T) {
	testRepoForExec(t)

	// All files in one layer — trivial case
	plan := &Plan{
		Layers: []Layer{
			{Name: "everything", Description: "All changes", Files: []string{
				"src/foo.go", "src/bar.go", "lib/util.go", "cmd/main.go",
			}},
		},
	}

	branches, err := Execute(plan, "single-stack", "main", "feature", ".")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	diff, err := gitpkg.DiffFull("feature", branches[0])
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if diff != "" {
		t.Errorf("single-layer tree identity failed:\n%s", diff)
	}
}

func TestValidateTree(t *testing.T) {
	testRepoForExec(t)

	// Same ref should pass
	if err := ValidateTree("feature", "feature"); err != nil {
		t.Errorf("same ref should pass: %v", err)
	}

	// Different refs should fail
	if err := ValidateTree("main", "feature"); err == nil {
		t.Error("different refs should fail")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go test ./internal/split/ -run 'TestExecute|TestValidateTree' -v -count=1`
Expected: compilation error

**Step 3: Implement execute.go**

Create `internal/split/execute.go`:

```go
package split

import (
	"fmt"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

// Execute creates branches and applies per-layer diffs for a validated plan.
// Returns the list of created branch names. On failure, the caller is
// responsible for cleanup (use Cleanup).
func Execute(plan *Plan, stackID, base, source, root string) ([]string, error) {
	stack.MigrateIfNeeded(root)

	if err := stack.Init(root, stackID, base); err != nil {
		return nil, fmt.Errorf("cannot initialize stack: %w", err)
	}

	cfg, err := cfgpkg.Load(root)
	if err != nil {
		cfg = cfgpkg.Defaults()
	}

	s, err := stack.LoadStack(root, stackID)
	if err != nil {
		return nil, fmt.Errorf("cannot load stack: %w", err)
	}

	var createdBranches []string
	parent := base

	for i, layer := range plan.Layers {
		shortName := fmt.Sprintf("%d-%s", i+1, layer.Name)
		branchName := cfgpkg.ApplyPrefix(cfg, stackID, shortName)

		if err := gitpkg.Checkout(parent); err != nil {
			return createdBranches, fmt.Errorf("cannot checkout %s: %w", parent, err)
		}

		if err := gitpkg.CreateBranch(branchName); err != nil {
			return createdBranches, fmt.Errorf("cannot create branch %s: %w", branchName, err)
		}
		createdBranches = append(createdBranches, branchName)

		// Extract diff for this layer's files
		patch, err := gitpkg.DiffFiles(base, source, layer.Files)
		if err != nil {
			return createdBranches, fmt.Errorf("cannot extract diff for %s: %w", layer.Name, err)
		}

		if patch == "" {
			return createdBranches, fmt.Errorf("empty diff for layer %s — no changes to apply", layer.Name)
		}

		// Apply the patch
		if err := gitpkg.ApplyPatch(patch); err != nil {
			return createdBranches, fmt.Errorf("apply failed for %s: %w", layer.Name, err)
		}

		// Stage and commit
		if err := gitpkg.Add(layer.Files...); err != nil {
			return createdBranches, fmt.Errorf("cannot stage files for %s: %w", layer.Name, err)
		}

		if err := gitpkg.Commit(layer.Description); err != nil {
			return createdBranches, fmt.Errorf("cannot commit %s: %w", layer.Name, err)
		}

		// Record node in stack
		parentTip, _ := gitpkg.RevParse(parent)
		s.Nodes = append(s.Nodes, stack.Node{
			Branch:  branchName,
			Status:  "open",
			BaseTip: parentTip,
		})

		parent = branchName
	}

	if err := stack.Save(root, s); err != nil {
		return createdBranches, fmt.Errorf("cannot save stack: %w", err)
	}

	return createdBranches, nil
}

// ValidateTree checks that two refs have identical trees.
// Returns nil if they match, an error if they differ.
func ValidateTree(source, lastBranch string) error {
	diff, err := gitpkg.DiffFull(source, lastBranch)
	if err != nil {
		return fmt.Errorf("cannot verify split: %w", err)
	}
	if diff != "" {
		return fmt.Errorf("tree differs from original branch (this is a bug)")
	}
	return nil
}

// Cleanup deletes created branches and restores the original branch.
func Cleanup(branches []string, restoreTo string) {
	gitpkg.Checkout(restoreTo)
	for _, b := range branches {
		gitpkg.DeleteBranch(b)
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go test ./internal/split/ -run 'TestExecute|TestValidateTree' -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/split/execute.go internal/split/execute_test.go
git commit -m "Add file-based split execution engine with tree identity validation"
```

---

### Task 8: Rewrite cmd/split.go — New orchestration

**Files:**
- Rewrite: `cmd/split.go`

**Step 1: Rewrite split.go**

Replace the entire contents of `cmd/split.go` with the new orchestration that uses the `internal/split` package. The new file:
- Adds `--from` (required) flag
- Makes `--stack` required
- Removes `--parts`
- Checks Claude availability as a precondition
- Calls `split.Analyze` for Claude analysis
- Displays the plan with per-layer stats
- Calls `split.Execute` for file-based execution
- Validates tree identity
- Pushes, creates PRs, saves session ID

```go
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	splitpkg "github.com/pavelpascari/sdf/internal/split"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
)

var splitCmd = &cobra.Command{
	Use:   "split",
	Short: "Split a branch into a stack of smaller PRs",
	Long: `Uses an AI agent to analyze a branch and decompose it into a chain of
focused, reviewable PRs managed as an sdf stack.

Requires the Claude CLI to be installed. The original branch is never modified.`,
	Example: `  sdf split --from feature/big-change --stack my-feature
  sdf split --from feature/big-change --stack my-feature --dry-run
  sdf split --from feature/big-change --stack my-feature --base main -y`,
	Annotations: map[string]string{"category": "stack"},
	RunE:        runSplitCmd,
}

func init() {
	rootCmd.AddCommand(splitCmd)
	splitCmd.Flags().String("from", "", "source branch to split (required)")
	splitCmd.Flags().String("stack", "", "name for the new stack (required)")
	splitCmd.Flags().String("base", "", "base branch (default: auto-detected)")
	splitCmd.Flags().Bool("dry-run", false, "show the split plan without executing")
	splitCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")
	splitCmd.Flags().Bool("no-push", false, "create branches locally without pushing or creating PRs")
	splitCmd.MarkFlagRequired("from")
	splitCmd.MarkFlagRequired("stack")
}

// RunSplit is a compatibility wrapper for tests.
func RunSplit(args []string) error {
	rootCmd.SetArgs(append([]string{"split"}, args...))
	return rootCmd.Execute()
}

func runSplitCmd(cmd *cobra.Command, args []string) error {
	fromBranch, _ := cmd.Flags().GetString("from")
	stackName, _ := cmd.Flags().GetString("stack")
	baseFlag, _ := cmd.Flags().GetString("base")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	noPush, _ := cmd.Flags().GetBool("no-push")

	// --- Preconditions ---

	// Claude CLI required
	if !claudepkg.Available() {
		return fmt.Errorf("sdf split requires an AI agent (claude CLI)\n  Install: https://claude.ai/download")
	}

	// Clean working tree
	clean, err := gitpkg.IsClean()
	if err != nil {
		return err
	}
	if !clean {
		return fmt.Errorf("working tree has uncommitted changes — commit or stash them first")
	}

	// Source branch must exist
	if !gitpkg.BranchExists(fromBranch) {
		return fmt.Errorf("branch %q does not exist", fromBranch)
	}

	// Determine base branch
	base := baseFlag
	if base == "" {
		detected, err := gitpkg.DefaultBranch()
		if err != nil {
			return fmt.Errorf("cannot detect base branch: %w\nSpecify one with --base <branch>", err)
		}
		base = detected
	}

	if fromBranch == base {
		return fmt.Errorf("cannot split the base branch %q", base)
	}

	root, err := gitpkg.RepoRoot()
	if err != nil {
		return fmt.Errorf("not inside a git repository: %w", err)
	}

	// Check if source branch is already in a stack
	if sdfRoot, findErr := stack.FindRoot(); findErr == nil {
		if _, loadErr := stack.LoadByBranch(sdfRoot, fromBranch); loadErr == nil {
			return fmt.Errorf("branch %q is already in a stack — cannot split", fromBranch)
		}
	}

	// Stack name must be available
	if sdfRoot, findErr := stack.FindRoot(); findErr == nil {
		if _, loadErr := stack.LoadStack(sdfRoot, stackName); loadErr == nil {
			return fmt.Errorf("stack %q already exists — choose a different name with --stack", stackName)
		}
	}

	// Must have changed files
	changedFiles, err := gitpkg.DiffNameOnly(base, fromBranch)
	if err != nil {
		return fmt.Errorf("cannot list changes: %w", err)
	}
	if len(changedFiles) == 0 {
		return fmt.Errorf("no changes to split — %s is up to date with %s", fromBranch, base)
	}
	if len(changedFiles) == 1 {
		return fmt.Errorf("only 1 file changed — nothing to split")
	}

	// --- Analysis ---
	fmt.Printf("Analyzing %s...\n", ui.Branch(fromBranch))

	result, err := splitpkg.Analyze(fromBranch, base, os.Stdout)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	// --- Display plan ---
	displaySplitPlan(result.Plan, stackName, base, fromBranch)

	if dryRun {
		return nil
	}

	// --- Confirm ---
	if !yes {
		if !ui.Confirm("Execute this split?") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// --- Execute ---
	// Remember current branch to restore later
	originalBranch, _ := gitpkg.CurrentBranch()

	fmt.Println("\nExecuting split...")
	branches, err := splitpkg.Execute(result.Plan, stackName, base, fromBranch, root)
	if err != nil {
		splitpkg.Cleanup(branches, originalBranch)
		return err
	}

	// --- Validate tree identity ---
	lastBranch := branches[len(branches)-1]
	if err := splitpkg.ValidateTree(fromBranch, lastBranch); err != nil {
		splitpkg.Cleanup(branches, originalBranch)
		return err
	}
	fmt.Printf("  %s Tree identity verified — split is lossless\n", ui.SymOK)

	// --- Save session ID ---
	if result.SessionID != "" {
		local, _ := stack.LoadLocal(root)
		if local.SplitSessions == nil {
			local.SplitSessions = make(map[string]string)
		}
		local.SplitSessions[stackName] = result.SessionID
		stack.SaveLocal(root, local)
	}

	if noPush {
		gitpkg.Checkout(originalBranch)
		fmt.Printf("\n%s Split complete — %d branches created in stack %q (local only)\n",
			ui.SymOK, len(branches), stackName)
		fmt.Println("\nNext steps:")
		fmt.Println("  sdf status           View the new stack")
		fmt.Println("  sdf pr               Create PRs (run from each branch)")
		if result.SessionID != "" {
			fmt.Printf("\nTo refine this split: claude --resume %s\n", result.SessionID)
		}
		return nil
	}

	// --- Push ---
	fmt.Println("\nPushing branches to origin...")
	pushFailed := false
	for _, b := range branches {
		if err := gitpkg.PushNew(b); err != nil {
			fmt.Fprintf(os.Stderr, "  %s could not push %s: %v\n", ui.SymWarn, ui.Branch(b), err)
			pushFailed = true
		} else {
			fmt.Printf("  %s %s\n", ui.SymOK, ui.Branch(b))
		}
	}

	// --- Create PRs ---
	if !pushFailed && ghpkg.Available() {
		s, err := stack.LoadStack(root, stackName)
		if err == nil {
			cfg, _ := cfgpkg.Load(root)
			fmt.Println("\nCreating pull requests...")
			if err := createSplitPRs(root, s, cfg, fromBranch, result.Plan); err != nil {
				fmt.Fprintf(os.Stderr, "  %s could not create PRs: %v\n", ui.SymWarn, err)
				fmt.Println("  You can create them manually with: sdf pr (from each branch)")
			}
		}
	} else if pushFailed {
		fmt.Println("\nSkipped PR creation (push failed for some branches).")
		fmt.Println("Push manually, then create PRs with: sdf pr")
	} else {
		fmt.Println("\nSkipped PR creation (gh CLI not available).")
		fmt.Println("Install gh from https://cli.github.com, then run: sdf pr")
	}

	// --- Restore + Report ---
	gitpkg.Checkout(originalBranch)

	fmt.Printf("\n%s Split complete — %d branches created in stack %q\n",
		ui.SymOK, len(branches), stackName)

	// Print stack chain
	s, _ := stack.LoadStack(root, stackName)
	if s != nil {
		printStackChain(s)
	}

	if result.SessionID != "" {
		fmt.Printf("\nTo refine this split: claude --resume %s\n", result.SessionID)
	}

	return nil
}

// displaySplitPlan shows the plan with per-layer file counts and line stats.
func displaySplitPlan(plan *splitpkg.Plan, stackName, base, source string) {
	fmt.Printf("\nSplit plan for %s (base: %s)\n", ui.Branch(stackName), ui.Branch(base))
	fmt.Println(strings.Repeat("─", 50))

	totalFiles := 0
	for i, layer := range plan.Layers {
		fileCount := len(layer.Files)
		totalFiles += fileCount

		// Try to get line stats for this layer
		lineInfo := ""
		stat, err := gitpkg.DiffFiles(base, source, layer.Files)
		if err == nil {
			adds, dels := countDiffLines(stat)
			if adds > 0 || dels > 0 {
				lineInfo = fmt.Sprintf(", +%d -%d", adds, dels)
			}
		}

		fmt.Printf("\n  Layer %d: %s (%d files%s)\n",
			i+1, ui.Bold.Render(layer.Name), fileCount, lineInfo)
		fmt.Printf("    %s\n", layer.Description)
	}

	fmt.Printf("\n  Total: %d files across %d layers\n\n",
		totalFiles, len(plan.Layers))
}

// countDiffLines counts added and removed lines in a diff string.
func countDiffLines(diff string) (adds, dels int) {
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			adds++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			dels++
		}
	}
	return
}

// createSplitPRs creates GitHub PRs for all branches in the split stack.
func createSplitPRs(root string, s *stack.Stack, cfg cfgpkg.Config, originalBranch string, plan *splitpkg.Plan) error {
	for i := range s.Nodes {
		node := &s.Nodes[i]
		base := s.ParentBranch(node.Branch)

		// Use layer description as title basis
		title := node.Branch
		if i < len(plan.Layers) {
			title = cfgpkg.GeneratePRTitle(cfg, s.StackID, node.Branch, []string{plan.Layers[i].Description})
		}

		body := buildSplitPRBody(s, i, originalBranch)

		fmt.Printf("  %s %s (base: %s)...\n", ui.SymPlan, title, ui.Branch(base))

		url, err := ghpkg.PRCreate(title, body, base, node.Branch)
		if err != nil {
			return fmt.Errorf("PR for %s: %w", node.Branch, err)
		}

		pr, err := ghpkg.PRView(node.Branch)
		if err == nil {
			node.PR = pr.Number
			node.Status = "open"
		}

		_ = url
	}

	if err := stack.Save(root, s); err != nil {
		return fmt.Errorf("cannot save stack: %w", err)
	}

	fmt.Println("Updating stack navigation...")
	if err := updateStackNavForAllPRs(root, s); err != nil {
		fmt.Fprintf(os.Stderr, "  %s could not update PR navigation: %v\n", ui.SymWarn, err)
	}

	return nil
}

// buildSplitPRBody generates the PR body for a split branch.
func buildSplitPRBody(s *stack.Stack, nodeIndex int, originalBranch string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Part of stack: **%s**\n\n", s.StackID)
	fmt.Fprintf(&b, "Split from `%s` — PR %d of %d\n", originalBranch, nodeIndex+1, len(s.Nodes))

	base := s.ParentBranch(s.Nodes[nodeIndex].Branch)
	fmt.Fprintf(&b, "\nBase: `%s`", base)

	return b.String()
}

// printStackChain prints a visual representation of the stack.
func printStackChain(s *stack.Stack) {
	parts := []string{s.Base}
	for _, node := range s.Nodes {
		label := node.Branch
		if node.PR > 0 {
			label = fmt.Sprintf("#%d %s", node.PR, node.Branch)
		}
		parts = append(parts, label)
	}
	fmt.Printf("\n  %s\n", strings.Join(parts, " ← "))
}

// sanitizeBranchComponent produces a branch-name-safe string.
func sanitizeBranchComponent(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, s)
	s = strings.Trim(s, "-")
	if s == "" {
		s = "changes"
	}
	return s
}
```

**Step 2: Verify compilation**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go build ./...`
Expected: compiles cleanly

**Step 3: Commit**

```bash
git add cmd/split.go
git commit -m "Rewrite sdf split: Claude-powered analysis + file-based execution"
```

---

### Task 9: Rewrite cmd/split_test.go

**Files:**
- Rewrite: `cmd/split_test.go`

**Step 1: Write the new tests**

Replace `cmd/split_test.go` with tests for the new interface. Since the command now requires Claude CLI, most integration tests focus on precondition checking. The heavy execution tests live in `internal/split/execute_test.go`.

```go
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/testutil"
)

// resetSplitFlags resets cobra flag state between test runs.
func resetSplitFlags() {
	splitCmd.Flags().Set("from", "")
	splitCmd.Flags().Set("stack", "")
	splitCmd.Flags().Set("base", "")
	splitCmd.Flags().Set("dry-run", "false")
	splitCmd.Flags().Set("yes", "false")
	splitCmd.Flags().Set("no-push", "false")
}

// splitTestRepo sets up a temp git repo with a feature branch.
func splitTestRepo(t *testing.T) string {
	t.Helper()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		os.Chdir(origDir)
	})

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	writeFile := func(name, content string) {
		t.Helper()
		full := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(full), 0755)
		os.WriteFile(full, []byte(content), 0644)
	}

	git("init", "-b", "main")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "Test")
	git("config", "commit.gpgsign", "false")

	writeFile("README.md", "# test\n")
	git("add", ".")
	git("commit", "-m", "initial")

	git("checkout", "-b", "big-feature")

	writeFile("internal/git/helpers.go", "package git\nfunc Help() {}\n")
	writeFile("internal/stack/topology.go", "package stack\nfunc Topo() {}\n")
	git("add", ".")
	git("commit", "-m", "add helpers and topology")

	return dir
}

func TestSplitRequiresClaude(t *testing.T) {
	resetSplitFlags()
	splitTestRepo(t)

	// Point Claude binary at something that doesn't exist
	testutil.SetBinary(t, &claudepkg.Binary, "/nonexistent/claude")

	err := RunSplit([]string{"--from", "big-feature", "--stack", "test", "--base", "main"})
	if err == nil {
		t.Fatal("expected error when Claude is not available")
	}
	if !strings.Contains(err.Error(), "AI agent") {
		t.Errorf("error should mention AI agent, got: %v", err)
	}
}

func TestSplitMissingFlags(t *testing.T) {
	resetSplitFlags()
	splitTestRepo(t)

	// Missing --from
	err := RunSplit([]string{"--stack", "test"})
	if err == nil {
		t.Fatal("expected error when --from is missing")
	}

	// Missing --stack
	resetSplitFlags()
	err = RunSplit([]string{"--from", "big-feature"})
	if err == nil {
		t.Fatal("expected error when --stack is missing")
	}
}

func TestSplitBranchNotExists(t *testing.T) {
	resetSplitFlags()
	splitTestRepo(t)

	// Use a fake Claude so the "requires AI" check passes
	testutil.SetBinary(t, &claudepkg.Binary, "true")

	err := RunSplit([]string{"--from", "nonexistent", "--stack", "test", "--base", "main"})
	if err == nil {
		t.Fatal("expected error for nonexistent branch")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error should mention branch not existing, got: %v", err)
	}
}

func TestSplitStackExists(t *testing.T) {
	resetSplitFlags()
	dir := splitTestRepo(t)

	// Use a fake Claude
	testutil.SetBinary(t, &claudepkg.Binary, "true")

	// Create a stack with the same name
	stack.Init(dir, "test", "main")

	err := RunSplit([]string{"--from", "big-feature", "--stack", "test", "--base", "main"})
	if err == nil {
		t.Fatal("expected error for existing stack")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention stack exists, got: %v", err)
	}
}

func TestBuildSplitPRBody(t *testing.T) {
	s := &stack.Stack{
		StackID: "my-feature",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "my-feature/1-schema", Status: "open"},
			{Branch: "my-feature/2-api", Status: "open"},
			{Branch: "my-feature/3-ui", Status: "open"},
		},
	}

	body := buildSplitPRBody(s, 1, "big-feature")

	if !strings.Contains(body, "my-feature") {
		t.Error("body should contain stack name")
	}
	if !strings.Contains(body, "big-feature") {
		t.Error("body should reference original branch")
	}
	if !strings.Contains(body, "PR 2 of 3") {
		t.Errorf("body should say PR 2 of 3, got:\n%s", body)
	}
}

func TestSanitizeBranchComponent(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"git", "git"},
		{"My Feature", "my-feature"},
		{"internal/git", "internal-git"},
		{"", "changes"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeBranchComponent(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeBranchComponent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
```

**Step 2: Check for testutil.SetBinary**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && grep -r "func SetBinary" internal/testutil/ 2>/dev/null || grep -r "func SetBinary" internal/ 2>/dev/null`

If `testutil.SetBinary` doesn't exist, check how existing tests (like `cmd/doctor_test.go`) handle binary overrides and use the same pattern.

**Step 3: Run tests**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go test ./cmd/ -run 'TestSplit' -v -count=1`
Expected: PASS for precondition tests

**Step 4: Commit**

```bash
git add cmd/split_test.go
git commit -m "Rewrite split tests for new Claude-powered interface"
```

---

### Task 10: Full integration test — verify everything compiles and passes

**Step 1: Build**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go build ./...`
Expected: compiles cleanly

**Step 2: Run all tests**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go test ./... -count=1`
Expected: all tests pass

**Step 3: Install**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go install ./...`
Expected: installs cleanly

**Step 4: Smoke test the CLI**

Run: `sdf split --help`
Expected: shows new flags (--from, --stack) and updated description

**Step 5: Commit (if any fixes were needed)**

```bash
git add -A
git commit -m "Fix integration issues from split rewrite"
```

---

### Task 11: Final commit — tidy up

**Step 1: Run go mod tidy**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go mod tidy`

**Step 2: Verify no dead code**

Ensure no unused imports or functions remain from the removed code (`commitInfo`, `splitGroup`, `analyzeBranch`, `autoGroup`, `equalSplit`, `dirsOverlap`, `fileDirs`, `deriveTitle`).

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go vet ./...`
Expected: no warnings

**Step 3: Final test run**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go test ./... -count=1`
Expected: all pass

**Step 4: Install**

Run: `cd /Users/pavelpascari/work/projects/pavelpascari/sdf && go install ./...`

**Step 5: Commit**

```bash
git add -A
git commit -m "Clean up: tidy deps and remove dead code from split rewrite"
```
