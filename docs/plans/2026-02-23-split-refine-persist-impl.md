# sdf split — Plan Refinement & Persistence Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace Y/n confirmation with Execute/Refine/Abort menu, persist plans to disk, and enable interactive plan refinement via Claude session.

**Architecture:** Add plan serialization to `internal/split/plan.go`, interactive Claude session to `internal/claude/claude.go`, refine/re-extract logic to `internal/split/ai.go`, and wire the 3-choice loop in `cmd/split.go`.

**Tech Stack:** Go 1.24, gopkg.in/yaml.v3, charmbracelet/huh (ui.Select), Claude CLI (`claude --resume`)

---

### Task 1: Plan Persistence — SavePlan, PlanPath, DeletePlan

Add functions to serialize a Plan to YAML, compute the save path, and clean up after execution.

**Files:**
- Modify: `internal/split/plan.go`
- Modify: `internal/split/plan_test.go`

**Step 1: Write the failing tests**

Add to `internal/split/plan_test.go`:

```go
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
```

Add imports to the test file: `"os"`, `"path/filepath"`.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/split/ -run 'TestSavePlan|TestPlanPath|TestDeletePlan' -v`
Expected: FAIL (functions undefined)

**Step 3: Implement**

Add to `internal/split/plan.go`:

```go
// SplitPlansDir is the subdirectory for persisted split plans.
const SplitPlansDir = "split-plans"

// PlanPath returns the file path for a stack's split plan.
func PlanPath(root, stackName string) string {
	return filepath.Join(root, stack.SDFDir, SplitPlansDir, stackName+".yaml")
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
```

Add imports: `"os"`, `"path/filepath"`, and `"github.com/pavelpascari/sdf/internal/stack"`.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/split/ -run 'TestSavePlan|TestPlanPath|TestDeletePlan' -v`
Expected: PASS

**Step 5: Run full test suite**

Run: `go build ./... && go vet ./... && go test ./internal/split/ -count=1`
Expected: All pass

**Step 6: Commit**

```bash
git add internal/split/plan.go internal/split/plan_test.go
git commit -m "feat(split): add SavePlan, PlanPath, DeletePlan for plan persistence"
```

---

### Task 2: Interactive Claude Resume

Add a function to spawn `claude --resume <id>` as an interactive terminal process with an initial prompt.

**Files:**
- Modify: `internal/claude/claude.go`

**Step 1: Implement**

This function spawns an interactive process with stdin/stdout/stderr attached to the terminal. It cannot be unit tested — it's verified manually and via integration tests.

Add to `internal/claude/claude.go`:

```go
// RunInteractiveResume spawns an interactive Claude session that resumes
// a previous conversation. The initialPrompt is passed as the positional
// argument so Claude starts with context. Returns nil when the user exits.
func RunInteractiveResume(sessionID, initialPrompt string) error {
	args := []string{"--resume", sessionID, initialPrompt}
	cmd := exec.Command(Binary, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
```

**Step 2: Build to verify compilation**

Run: `go build ./...`
Expected: Clean build

**Step 3: Commit**

```bash
git add internal/claude/claude.go
git commit -m "feat(claude): add RunInteractiveResume for interactive session resumption"
```

---

### Task 3: Refine and Re-Extract Prompts

Add prompt builders for the interactive refinement session and the follow-up plan re-extraction.

**Files:**
- Modify: `internal/split/ai.go`
- Modify: `internal/split/ai_test.go`

**Step 1: Write the failing tests**

Add to `internal/split/ai_test.go`:

```go
func TestBuildRefinePrompt(t *testing.T) {
	plan := &Plan{
		Layers: []Layer{
			{Name: "db", Description: "Add schema", Files: []string{"a.go", "b.go"}},
			{Name: "api", Description: "REST endpoints", Files: []string{"c.go"},
				PartialFiles: []PartialFile{{Path: "shared.go", Hunks: []int{1, 3}}}},
		},
	}

	prompt := BuildRefinePrompt(plan)

	if !strings.Contains(prompt, "db") {
		t.Error("prompt should contain layer name 'db'")
	}
	if !strings.Contains(prompt, "api") {
		t.Error("prompt should contain layer name 'api'")
	}
	if !strings.Contains(prompt, "Add schema") {
		t.Error("prompt should contain layer description")
	}
	if !strings.Contains(prompt, "a.go") {
		t.Error("prompt should contain file names")
	}
	if !strings.Contains(prompt, "shared.go") {
		t.Error("prompt should contain partial file names")
	}
	if !strings.Contains(prompt, "refine") || !strings.Contains(prompt, "change") {
		t.Error("prompt should ask user what to change")
	}
}

func TestBuildReExtractPrompt(t *testing.T) {
	prompt := BuildReExtractPrompt()

	if !strings.Contains(prompt, "layers:") {
		t.Error("prompt should show expected YAML format")
	}
	if !strings.Contains(prompt, "yaml") {
		t.Error("prompt should mention YAML")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/split/ -run 'TestBuildRefinePrompt|TestBuildReExtractPrompt' -v`
Expected: FAIL (functions undefined)

**Step 3: Implement**

Add to `internal/split/ai.go`:

```go
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
	b.WriteString("exit this session (Ctrl+C or /exit) and sdf will re-read the updated plan.\n\n")
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
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/split/ -run 'TestBuildRefinePrompt|TestBuildReExtractPrompt' -v`
Expected: PASS

**Step 5: Run full test suite**

Run: `go build ./... && go vet ./... && go test ./internal/split/ -count=1`
Expected: All pass

**Step 6: Commit**

```bash
git add internal/split/ai.go internal/split/ai_test.go
git commit -m "feat(split): add BuildRefinePrompt and BuildReExtractPrompt"
```

---

### Task 4: ReExtractPlan Function

Add a function that resumes the Claude session in `-p` mode, parses the plan from the response, and validates it.

**Files:**
- Modify: `internal/split/ai.go`
- Modify: `internal/split/ai_test.go`

**Step 1: Write the failing test**

Add to `internal/split/ai_test.go`:

```go
func TestBuildReExtractPrompt_ContainsFormat(t *testing.T) {
	prompt := BuildReExtractPrompt()
	if !strings.Contains(prompt, "name:") {
		t.Error("re-extract prompt should contain name field")
	}
	if !strings.Contains(prompt, "description:") {
		t.Error("re-extract prompt should contain description field")
	}
	if !strings.Contains(prompt, "files:") {
		t.Error("re-extract prompt should contain files field")
	}
}
```

Note: `ReExtractPlan` itself calls Claude (external process) so it cannot be unit tested. We test the prompt construction and rely on integration testing for the full flow.

**Step 2: Implement**

Add to `internal/split/ai.go`:

```go
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
```

**Step 3: Run tests**

Run: `go build ./... && go vet ./... && go test ./internal/split/ -count=1`
Expected: All pass

**Step 4: Commit**

```bash
git add internal/split/ai.go internal/split/ai_test.go
git commit -m "feat(split): add ReExtractPlan for post-refinement plan extraction"
```

---

### Task 5: Wire the 3-Choice Loop in cmd/split.go

Replace the Y/n confirmation with Execute/Refine/Abort, add plan persistence, and wire the refinement loop.

**Files:**
- Modify: `cmd/split.go`

**Context:** The current confirmation code is at lines 140-146 in `cmd/split.go`:

```go
// --- Confirm ---
if !yes {
    if !ui.Confirm("Execute this split?") {
        fmt.Println("Aborted.")
        return nil
    }
}
```

Replace this entire section with the 3-choice loop. Also add plan save after analysis and plan delete after successful execution.

**Step 1: Implement the changes**

Add import for `"github.com/charmbracelet/huh"` at the top of `cmd/split.go`.

Replace the confirmation section (lines 133-146) and add plan persistence. The new code:

After the analysis result is obtained and the plan is displayed (around line 134), insert plan save:

```go
// --- Save plan to disk ---
planPath := splitpkg.PlanPath(root, stackName)
if err := splitpkg.SavePlan(planPath, result.Plan); err != nil {
    fmt.Fprintf(os.Stderr, "  %s could not save plan: %v\n", ui.SymWarn, err)
} else {
    fmt.Printf("  Plan saved to %s\n", planPath)
}
```

Replace the confirmation block with the 3-choice loop:

```go
if dryRun {
    return nil
}

// --- Choose action ---
plan := result.Plan
sessionID := result.SessionID

if !yes {
    for {
        choice := ui.Select("What would you like to do?", []huh.Option[string]{
            huh.NewOption("Execute this split", "execute"),
            huh.NewOption("Refine plan (opens Claude session)", "refine"),
            huh.NewOption("Abort", "abort"),
        })

        switch choice {
        case "execute":
            goto execute
        case "refine":
            if sessionID == "" {
                fmt.Println("No Claude session available for refinement.")
                continue
            }

            fmt.Println("\nOpening Claude session for plan refinement...")
            fmt.Println("(Exit with Ctrl+C or /exit when done)\n")

            refinePrompt := splitpkg.BuildRefinePrompt(plan)
            if err := claudepkg.RunInteractiveResume(sessionID, refinePrompt); err != nil {
                fmt.Fprintf(os.Stderr, "\n%s Claude session exited with error: %v\n", ui.SymWarn, err)
                fmt.Println("Continuing with the previous plan.\n")
                displaySplitPlan(plan, stackName, base, fromBranch)
                continue
            }

            fmt.Println("\nRe-reading plan from Claude session...")
            changedFiles, _ := gitpkg.DiffNameOnly(base, fromBranch)
            newPlan, err := splitpkg.ReExtractPlan(sessionID, "split-analysis", changedFiles, os.Stdout)
            if err != nil {
                fmt.Fprintf(os.Stderr, "\n%s Could not re-extract plan: %v\n", ui.SymWarn, err)
                fmt.Println("Continuing with the previous plan.\n")
                displaySplitPlan(plan, stackName, base, fromBranch)
                continue
            }

            plan = newPlan
            // Check for shared files in the refined plan
            shared := splitpkg.SharedFiles(plan)
            if len(shared) > 0 {
                fmt.Printf("\n%d file(s) appear in multiple layers — assigning hunks...\n", len(shared))
                fileDiffs, hunkCounts, err := splitpkg.ParseSharedFileDiffs(base, fromBranch, shared)
                if err != nil {
                    fmt.Fprintf(os.Stderr, "\n%s Could not parse shared file diffs: %v\n", ui.SymWarn, err)
                    fmt.Println("Continuing with the previous plan.\n")
                    plan = result.Plan
                    displaySplitPlan(plan, stackName, base, fromBranch)
                    continue
                }

                hunkPrompt := splitpkg.BuildHunkPrompt(shared, fileDiffs)
                sr, err := claudepkg.RunPromptStreamingResume("split-analysis", sessionID, hunkPrompt, os.Stdout)
                if err != nil {
                    fmt.Fprintf(os.Stderr, "\n%s Hunk assignment failed: %v\n", ui.SymWarn, err)
                    fmt.Println("Continuing with the previous plan.\n")
                    plan = result.Plan
                    displaySplitPlan(plan, stackName, base, fromBranch)
                    continue
                }

                resp, err := splitpkg.ParseHunkAssignment(sr.Result)
                if err != nil {
                    fmt.Fprintf(os.Stderr, "\n%s Could not parse hunk assignments: %v\n", ui.SymWarn, err)
                    fmt.Println("Continuing with the previous plan.\n")
                    plan = result.Plan
                    displaySplitPlan(plan, stackName, base, fromBranch)
                    continue
                }

                validationErrs := splitpkg.ValidateHunkAssignment(resp, shared, hunkCounts)
                if len(validationErrs) > 0 {
                    fmt.Fprintf(os.Stderr, "\n%s Hunk assignment validation failed\n", ui.SymWarn)
                    fmt.Println("Continuing with the previous plan.\n")
                    plan = result.Plan
                    displaySplitPlan(plan, stackName, base, fromBranch)
                    continue
                }

                plan = splitpkg.MergePlan(plan, resp)
            }

            result.Plan = plan

            fmt.Println()
            displaySplitPlan(plan, stackName, base, fromBranch)

            // Update saved plan
            if err := splitpkg.SavePlan(planPath, plan); err != nil {
                fmt.Fprintf(os.Stderr, "  %s could not save updated plan: %v\n", ui.SymWarn, err)
            }

            continue
        default:
            // abort or empty (user cancelled)
            fmt.Println("Aborted.")
            return nil
        }
    }
}
execute:
```

After successful tree identity verification (around current line 171), add plan cleanup:

```go
// --- Delete plan file (split succeeded) ---
splitpkg.DeletePlan(planPath)
```

**Important:** The `parseSharedFileDiffs` function in `ai.go` is currently unexported. We need to export it as `ParseSharedFileDiffs` for use from `cmd/split.go`.

**Step 2: Export parseSharedFileDiffs in ai.go**

In `internal/split/ai.go`, rename `parseSharedFileDiffs` to `ParseSharedFileDiffs` (capitalize the P). Update the call in the `Analyze` function to use `ParseSharedFileDiffs`.

**Step 3: Build and verify**

Run: `go build ./...`
Expected: Clean build

**Step 4: Manual smoke test**

The 3-choice loop involves interactive UI (`ui.Select`, `claude --resume`) which can't be unit tested easily. Verify manually:

1. `go install ./...`
2. Create a test repo with a feature branch
3. `sdf split --from feature --stack test --dry-run` — should show plan without prompt
4. `sdf split --from feature --stack test -y` — should skip prompt and execute
5. `sdf split --from feature --stack test` — should show 3-choice menu

**Step 5: Commit**

```bash
git add cmd/split.go internal/split/ai.go
git commit -m "feat(split): add Execute/Refine/Abort menu with plan persistence"
```

---

### Task 6: Verify Full Test Suite

Run the complete test suite to ensure nothing is broken.

**Step 1: Build, vet, test**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: All pass

**Step 2: Verify plan persistence paths**

The `.sdf/split-plans/` directory uses `stack.SDFDir` constant for consistency. Verify this compiles and the path looks correct by checking the test output from Task 1.

---

## Context Notes

**Key files you'll need to understand:**

- `internal/split/plan.go` — Plan/Layer/PartialFile structs, validation functions, YAML parsing
- `internal/split/ai.go` — Claude interaction: prompts, Analyze (2-phase), retry logic
- `internal/claude/claude.go` — Claude CLI wrapper: `RunPromptStreaming`, `RunPromptStreamingResume`
- `cmd/split.go` — Command orchestration: flags, preconditions, analysis, display, execute, push, PRs
- `internal/ui/prompt.go` — `ui.Confirm` (being replaced), `ui.Select` (being used)
- `internal/stack/stack.go` — `SDFDir` constant, `LocalState` with `SplitSessions`

**Existing patterns to follow:**

- Error handling: non-fatal warnings use `fmt.Fprintf(os.Stderr, "  %s ...\n", ui.SymWarn, ...)`, fatal errors return `fmt.Errorf(...)`
- YAML: uses `gopkg.in/yaml.v3` throughout
- Plan display: `displaySplitPlan(plan, stackName, base, source)` already handles partial files
- Session persistence: `SplitSessions` map in `.sdf/local.json` stores `stackName → sessionID`
