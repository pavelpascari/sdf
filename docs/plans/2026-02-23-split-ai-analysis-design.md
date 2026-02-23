# sdf split — Claude-Powered Analysis (Iteration 3)

## Summary

Rewrite `sdf split` to use an AI agent (Claude) for commit analysis and a file-based execution engine (diff+apply). Replaces the directory-affinity heuristic and cherry-pick engine from iterations 1+2.

The command requires Claude CLI. No fallback heuristic — if Claude is unavailable, the command errors with install guidance.

---

## Command Interface

```
sdf split --from <branch> --stack <name>              # required
sdf split --from <branch> --stack <name> --dry-run    # show plan only
sdf split --from <branch> --stack <name> --no-push    # local only
sdf split --from <branch> --stack <name> --base <branch>
sdf split --from <branch> --stack <name> -y           # skip confirmation
```

Both `--from` and `--stack` are required. `--base` defaults to auto-detected default branch.

### Removed flags

- `--parts N` — Claude decides grouping

### New precondition

```
✗ sdf split requires an AI agent (claude CLI)
  Install: https://claude.ai/download
```

---

## Claude Invocation

### Agentic, not prompt-stuffed

The prompt gives Claude instructions and success criteria. Claude explores the repo itself using git commands (diff, log, show, etc.). This keeps the prompt small and leverages Claude's tool-use capabilities.

### Streaming with session capture

Invocation: `claude -p <prompt> --output-format stream-json --include-partial-messages`

The stream-json format emits:
1. **Init event** — capture `session_id`
2. **Assistant events** — display progress to user in real-time
3. **Result event** — capture final response + `session_id`

Session ID is stored in `.sdf/local.json` under `split_sessions.<stack_name>` for later resumption via `claude --resume <id>`.

### Prompt template

```
You are analyzing a git branch to split it into a stack of smaller, reviewable PRs.

SOURCE BRANCH: {{.FromBranch}}
BASE BRANCH: {{.Base}}

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

Return ONLY this YAML (wrapped in ```yaml fences):

layers:
  - name: <kebab-case-name>
    description: "<one-line summary>"
    files:
      - <path/to/file>
      - ...
```

### Retry logic

If validation fails, retry up to 3 times using `--resume <session_id>` so Claude has full context of its previous attempt. The retry prompt describes exactly what went wrong (missing files, duplicates, etc.).

After 3 failures, surface the error with validation details.

---

## Plan Format

```yaml
layers:
  - name: db-schema
    description: "Add users table with migration"
    files:
      - migrations/001_create_users.sql
      - internal/models/user.go

  - name: repository
    description: "UserRepository with CRUD operations"
    files:
      - internal/users/repository.go
      - internal/users/repository_test.go
```

### Data structures

```go
type Plan struct {
    Layers []Layer `yaml:"layers"`
}

type Layer struct {
    Name        string   `yaml:"name"`
    Description string   `yaml:"description"`
    Files       []string `yaml:"files"`
}
```

### Validation (hard gates)

1. **Completeness**: every file in `git diff --name-only base...source` appears in exactly one layer
2. **No extras**: no file in a layer that isn't actually changed in the source branch
3. **No duplicates**: no file appears in more than one layer
4. **Non-empty layers**: every layer has at least one file
5. **Valid names**: layer names are valid branch name components

### Known limitation

Whole files go to one layer. A file with changes belonging to multiple concerns goes to the earliest layer that needs it. Hunk-level decomposition is deferred to iteration 4.

---

## Execution Engine (file-based diff+apply)

Replaces the cherry-pick engine from iterations 1+2.

### Step by step

1. Initialize sdf stack (`stack.Init`)
2. For each layer in order:
   a. Checkout parent (base for layer 1, previous layer's branch otherwise)
   b. Create branch: `<prefix>/<stack>/<N>-<layer-name>`
   c. Extract diff: `git diff base...source -- file1 file2 ...`
   d. Apply: `git apply --3way <patch>`
   e. Stage and commit with layer description as message
   f. Record node in stack
3. Tree identity check: `git diff source_branch last_layer_branch` must be empty
4. Push branches (unless `--no-push`)
5. Create PRs with stack navigation (if `gh` available and push succeeded)
6. Save YAML plan to `.sdf/split-plans/<stack-name>.yaml` *(deferred to iteration 4)*
7. Store session ID in `.sdf/local.json`
8. Restore original branch

### Why file-based over cherry-pick

- Handles mixed commits (one commit touching files in different layers)
- Exact file-level control per layer
- One clean commit per layer (better for review)
- Sets up hunk-level splitting for iteration 4
- Commit history within the source is irrelevant (agent-generated code has messy commits)

---

## User-Facing Output

### Plan display (before confirmation)

```
Analyzing feature/big-change...
  [Claude's streaming progress]

Split plan:
──────────────────────────────────────────────────
  Layer 1: db-schema (4 files, +320 lines)
    Add users table with migration

  Layer 2: repository (6 files, +480 lines)
    UserRepository with CRUD operations and tests

  Layer 3: api-handlers (8 files, +612 lines)
    REST endpoints for user management

  Total: 18 files, +1,412 lines across 3 layers
──────────────────────────────────────────────────

? Execute this split? (Y/n)
```

### Execution report

```
Executing split...
  ✓ Layer 1: my-feature/1-db-schema — 4 files applied
  ✓ Layer 2: my-feature/2-repository — 6 files applied
  ✓ Layer 3: my-feature/3-api-handlers — 8 files applied
  ✓ Tree identity verified — split is lossless

Pushing branches...
  ✓ my-feature/1-db-schema
  ✓ my-feature/2-repository
  ✓ my-feature/3-api-handlers

Creating pull requests...
  ✓ #201 db-schema (base: main)
  ✓ #202 repository (base: my-feature/1-db-schema)
  ✓ #203 api-handlers (base: my-feature/2-repository)
Updating stack navigation...

✓ Split complete — 3 PRs created in stack "my-feature"

  main ← #201 db-schema ← #202 repository ← #203 api-handlers

To refine this split: claude --resume abc123def
```

---

## File Structure

### New files

- `internal/split/plan.go` — Plan/Layer structs, ParsePlan, ValidatePlan, PlanStats
- `internal/split/ai.go` — BuildPrompt, Analyze (streaming + retry), YAML extraction
- `internal/split/execute.go` — file-based Execute, ValidateTree
- `internal/split/plan_test.go` — parsing and validation unit tests
- `internal/split/ai_test.go` — prompt construction, YAML extraction, retry tests
- `internal/split/execute_test.go` — file-based execution integration tests

### Modified files

- `cmd/split.go` — rewritten: flag parsing, preconditions, orchestration only
- `cmd/split_test.go` — rewritten: integration tests for new interface
- `internal/claude/claude.go` — extend RunPromptStreaming to return session_id; add RunPromptStreamingResume
- `internal/git/git.go` — add DiffFiles, ApplyPatch helpers

### Removed code from cmd/split.go

- `commitInfo`, `splitGroup` structs
- `analyzeBranch`, `autoGroup`, `equalSplit`
- `dirsOverlap`, `fileDirs`, `deriveTitle`
- Cherry-pick logic in `executeSplit`
- `--parts` flag

---

## Error Handling

### Precondition errors (fail fast, before Claude)

| Condition | Error |
|-----------|-------|
| Dirty working tree | `"working tree has uncommitted changes"` |
| `--from` branch missing | `"branch %q does not exist"` |
| Source = base | `"cannot split the base branch"` |
| Branch in a stack | `"branch %q is already in a stack"` |
| Stack name taken | `"stack %q already exists"` |
| Claude not installed | `"sdf split requires an AI agent"` |
| No commits | `"no commits to split"` |
| Only 1 file changed | `"only 1 file changed — nothing to split"` |

### Claude failures

- Non-zero exit → retry up to 3 times
- No YAML in response → retry with guidance
- Invalid YAML → retry with parse error
- Validation fails → retry with specific errors
- 3 retries exhausted → surface last error

### Execution failures

- `git apply --3way` fails → abort, cleanup, report conflicting files
- Tree identity fails → abort, cleanup, report as bug
- Push failure (partial) → skip PRs, report, suggest manual push
- `gh` unavailable → skip PRs, print guidance

### Cleanup guarantee

Any failure after branch creation triggers cleanup: delete created branches, restore original branch. Source branch is never modified.

---

## Testing Strategy

### internal/split/plan_test.go — Unit tests (no git)

- Parse YAML from fenced code block
- Parse bare YAML
- Parse with extra text around YAML
- Reject invalid YAML
- Validation: missing file, duplicate, extra, empty layer, invalid name
- Validation: valid plan passes

### internal/split/execute_test.go — Integration tests (temp git repos)

- 3 layers, clean split, tree identity
- Layer with new subdirectory
- Apply failure handling
- Stack topology correctness
- Cleanup on failure

### internal/split/ai_test.go — Unit tests (mock Claude)

- Prompt includes branch names and instructions
- YAML extraction from streaming result
- Session ID captured from init event
- Retry with validation errors via session resume
- 3 retries exhausted → error

### cmd/split_test.go — Integration tests

- Missing `--from` or `--stack` → error
- Branch doesn't exist → error
- Stack name taken → error
- Claude not available → error with guidance
- `--dry-run` shows plan, no branches
- `--no-push` creates branches, no PRs

---

## Future Work (iteration 4+)

- **Hunk-level decomposition**: split individual files across layers at the diff hunk level
- **Plan editing UX**: open plan in `$EDITOR` before execution
- **YAML plan import**: `sdf split --plan <file>` to skip AI and use a user-provided plan
- **`--refine` flag**: resume Claude session to adjust the plan interactively
