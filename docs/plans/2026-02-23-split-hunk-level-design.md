# sdf split — Hunk-Level Decomposition (Iteration 4)

## Summary

Extend `sdf split` to split individual files across multiple layers at the diff hunk level. A file whose changes span multiple concerns can have its hunks assigned to different layers instead of being forced into the earliest one.

Uses a two-phase Claude analysis: Phase 1 groups files into layers (same as iteration 3, but files *may* appear in multiple layers). Phase 2 runs only for "shared files" — sdf parses their diffs into numbered hunks and asks Claude to assign each hunk to a layer.

Backward compatible — branches where every file belongs to a single layer behave identically to iteration 3.

---

## Command Interface

No changes to the CLI. Same flags and behavior as iteration 3:

```
sdf split --from <branch> --stack <name>
sdf split --from <branch> --stack <name> --dry-run
sdf split --from <branch> --stack <name> --no-push
sdf split --from <branch> --stack <name> --base <branch>
sdf split --from <branch> --stack <name> -y
```

---

## Two-Phase Analysis

### Phase 1: File-Level Grouping

Nearly identical to iteration 3. The key change: relax the "every file in exactly one layer" constraint.

**Prompt change** (single line):
```
- A file MAY appear in multiple layers if its changes clearly serve different concerns.
  When a file appears in multiple layers, we'll assign individual hunks in a follow-up step.
- If all of a file's changes belong to one concern, list it in only one layer.
```

**Output**: same YAML format as iteration 3. Files may repeat across layers.

### Phase 2: Hunk Assignment (conditional)

Triggered only when Phase 1 produces "shared files" (files listed in >1 layer).

For each shared file:

1. sdf extracts the full diff: `git diff base...source -- <file>`
2. sdf parses the diff into numbered hunks (0-indexed)
3. sdf sends a targeted follow-up prompt via `--resume <session_id>`

**Phase 2 prompt** (per batch of shared files):

```
Some files in your plan appear in multiple layers. I've extracted their diff hunks below.
For each file, assign every hunk to exactly one of the layers you listed it in.

## <filepath>
Layers: <layer-a>, <layer-b>

Hunk 0:
@@ -10,7 +10,8 @@ func Foo() {
 context
-removed
+added
 context

Hunk 1:
@@ -25,3 +26,5 @@ func Bar() {
 context
+new line
 context

---

Return ONLY this YAML (wrapped in ```yaml fences):

```yaml
hunk_assignments:
  - file: <filepath>
    assignments:
      - hunk: 0
        layer: <layer-name>
      - hunk: 1
        layer: <layer-name>
```
```

**Retry**: same session-resume retry logic as Phase 1 (up to 3 retries), validating that every hunk is assigned to exactly one layer that originally listed the file.

### Merging phases into final plan

After Phase 2, sdf merges the results:

1. Files in only one layer → `Layer.Files` (whole files, same as iteration 3)
2. Shared files → removed from `Layer.Files`, added to `Layer.PartialFiles` with their hunk indices

---

## Plan Format

### Extended data structures

```go
type Plan struct {
    Layers []Layer `yaml:"layers"`
}

type Layer struct {
    Name         string        `yaml:"name"`
    Description  string        `yaml:"description"`
    Files        []string      `yaml:"files"`
    PartialFiles []PartialFile `yaml:"partial_files,omitempty"`
}

type PartialFile struct {
    Path  string `yaml:"path"`
    Hunks []int  `yaml:"hunks"`
}
```

### Phase 2 response structures

```go
type HunkAssignmentResponse struct {
    HunkAssignments []FileHunkAssignment `yaml:"hunk_assignments"`
}

type FileHunkAssignment struct {
    File        string           `yaml:"file"`
    Assignments []HunkAssignment `yaml:"assignments"`
}

type HunkAssignment struct {
    Hunk  int    `yaml:"hunk"`
    Layer string `yaml:"layer"`
}
```

### Example merged plan

```yaml
layers:
  - name: db-schema
    description: "Add users table with migration"
    files:
      - migrations/001_create_users.sql
    partial_files:
      - path: internal/models/user.go
        hunks: [0, 2]

  - name: api-handlers
    description: "REST endpoints for user management"
    files:
      - internal/handlers/users.go
      - internal/handlers/users_test.go
    partial_files:
      - path: internal/models/user.go
        hunks: [1, 3]
```

---

## Hunk Parsing

New package-level utilities in `internal/split/hunk.go`.

### ParseHunks

```go
// Hunk represents a single diff hunk for one file.
type Hunk struct {
    Header string // the @@ line
    Body   string // all lines after the header, up to the next hunk or file
}

// FileDiff represents all hunks for a single file in a unified diff.
type FileDiff struct {
    Header string // diff --git, index, ---, +++ lines
    Path   string // file path extracted from the header
    Hunks  []Hunk
}

// ParseDiff splits a unified diff string into per-file sections with numbered hunks.
func ParseDiff(diff string) []FileDiff
```

### FilterHunks

```go
// FilterHunks reconstructs a valid patch containing only the specified hunks
// from a FileDiff. The returned string is suitable for git apply.
func FilterHunks(fd FileDiff, indices []int) string
```

The reconstructed patch includes the file header (`diff --git`, `index`, `---`, `+++`) followed by only the selected hunk headers and bodies. Line numbers in `@@` headers are left as-is — `git apply --3way` handles the merge correctly since it falls back to three-way merge using the common ancestor.

---

## Execution Engine Changes

### Current flow (iteration 3)

For each layer: extract diff for whole files → apply → commit.

### New flow (iteration 4)

For each layer:
1. Checkout parent branch
2. Create layer branch
3. **Whole files**: same as before — `git diff base...source -- file1 file2` → apply
4. **Partial files**: for each partial file:
   a. Get full diff: `git diff base...source -- <file>`
   b. Parse into `FileDiff`
   c. Filter to only this layer's hunk indices
   d. Apply filtered patch via `git apply --3way`
5. Stage and commit

The key insight: `git apply --3way` resolves against the merge base, so applying subset hunks to a branch that may already have other hunks from a previous layer works correctly — the hunks operate on independent regions of the file.

### Tree identity check

Unchanged — `git diff source_branch last_layer_branch` must still be empty. This validates that the hunk-level split is lossless.

---

## Validation Changes

### Phase 1 validation (relaxed)

Replaces `ValidatePlan` with `ValidatePhase1`:

1. **Coverage**: every changed file appears in at least one layer
2. **No extras**: no file that isn't in the diff
3. **Non-empty layers**: every layer has at least one file
4. **Valid names**: kebab-case

Note: duplicates are no longer errors — they signal shared files that need Phase 2.

### Phase 2 validation (new)

`ValidateHunkAssignment(response, sharedFiles, hunkCounts)`:

1. **Completeness**: every hunk of every shared file is assigned
2. **No duplicates**: no hunk assigned to multiple layers
3. **Valid layers**: each hunk assigned to a layer that listed the file
4. **Valid indices**: all hunk indices are within range

### Final plan validation (post-merge) *(deferred to iteration 5)*

`ValidateFinalPlan` is not yet implemented. The tree identity check at execution time (`git diff source last_layer` must be empty) catches any issues with malformed merged plans. Explicit pre-execution validation would provide clearer error messages and avoid unnecessary branch creation.

---

## User-Facing Output

### Plan display (updated)

```
Split plan for my-feature (base: main)
──────────────────────────────────────────────────

  Layer 1: db-schema (2 files, +320 lines)
    Add users table with migration
    Shared: internal/models/user.go (hunks 0, 2)

  Layer 2: api-handlers (3 files, +480 lines)
    REST endpoints for user management
    Shared: internal/models/user.go (hunks 1, 3)

  Total: 4 files across 2 layers (1 file split at hunk level)
──────────────────────────────────────────────────
```

Shared files are shown with their hunk indices so the user can verify the assignment.

---

## File Structure

### New files

- `internal/split/hunk.go` — `Hunk`, `FileDiff`, `ParseDiff`, `FilterHunks`
- `internal/split/hunk_test.go` — unit tests for hunk parsing/filtering

### Modified files

- `internal/split/plan.go` — add `PartialFile` struct, `HunkAssignmentResponse` structs, `ValidatePhase1`, `ValidateHunkAssignment`, `ValidateFinalPlan`; update `ParsePlan` for backward compat
- `internal/split/plan_test.go` — new tests for Phase 1 validation, hunk assignment validation, final validation
- `internal/split/ai.go` — add `BuildHunkPrompt`, update `Analyze` for two-phase flow
- `internal/split/ai_test.go` — tests for hunk prompt construction
- `internal/split/execute.go` — update `Execute` to handle `PartialFiles`
- `internal/split/execute_test.go` — test with shared files and hunk-level split
- `cmd/split.go` — update `displaySplitPlan` to show shared file info

---

## Error Handling

### Phase 2 specific errors

| Condition | Error |
|-----------|-------|
| Hunk index out of range | `"hunk %d for %s is out of range (0-%d)"` |
| Hunk assigned to wrong layer | `"hunk %d for %s assigned to %q which didn't list the file"` |
| Missing hunk assignment | `"hunk %d for %s is not assigned to any layer"` |
| Duplicate hunk assignment | `"hunk %d for %s assigned to both %q and %q"` |
| Hunk patch apply failure | `"cannot apply hunks for %s in layer %s: %w"` |

### Retry behavior

Phase 2 retries independently from Phase 1, using the same session (so Claude has full context). Up to 3 retries on validation failure, same as Phase 1.

---

## Testing Strategy

### internal/split/hunk_test.go — Unit tests (no git)

- Parse single-file diff into hunks
- Parse multi-file diff
- Parse diff with no hunks (empty)
- Filter specific hunks
- Filter all hunks (whole file)
- Reconstruct valid patch from filtered hunks

### internal/split/plan_test.go — Extended validation tests

- Phase 1 validation: shared files are OK (not errors)
- Phase 1 validation: files still must be in the diff
- Hunk assignment validation: all hunks assigned → pass
- Hunk assignment validation: missing hunk → error
- Hunk assignment validation: duplicate hunk → error
- Hunk assignment validation: wrong layer → error
- Final plan validation with partial files

### internal/split/ai_test.go — Hunk prompt tests

- Hunk prompt includes numbered hunks
- Hunk prompt includes layer names
- Hunk assignment response parsing

### internal/split/execute_test.go — Hunk execution tests

- Split with shared file: 2 layers, 1 file split across them
- Tree identity holds after hunk-level split
- Mixed plan: some whole files, some partial files

---

## Known Limitations

- **Overlapping context lines**: If two hunks are very close together, git may merge their context lines, making them one hunk. This is inherent to unified diff format and means fine-grained splitting has limits.
- **Binary files**: Always assigned as whole files. Hunk-level splitting only applies to text diffs.
- **Rename detection**: `git diff` may report a file as renamed. Renames go as whole files to one layer.

---

## Future Work (iteration 5+)

- **Plan editing UX**: open plan in `$EDITOR` before execution
- **YAML plan import**: `sdf split --plan <file>` to skip AI and use a user-provided plan
- **`--refine` flag**: resume Claude session to adjust the plan interactively
- **Save plans to disk**: persist YAML plan to `.sdf/split-plans/<stack-name>.yaml`
