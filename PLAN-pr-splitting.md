# sdf split — Turn a Large PR into a Stack

## Problem Statement

You've been working with an AI agent (or solo) and accumulated a 5,000+ line PR. Reviewers dread it. You want to break it into a chain of focused, reviewable PRs — but doing so manually is tedious: figuring out the right split points, cherry-picking, rebasing, creating branches, fixing up commits, and wiring the PR chain together.

`sdf split` automates this. It analyzes a large branch, plans a logical decomposition into layers, and executes the git operations to produce a proper sdf-managed stack with PRs chained correctly.

---

## Two Modes of Operation

### Mode 1: `sdf split --from <branch>` (AI-planned split)

The "trust the tool" path. Claude analyzes the full diff, commit history, and file structure of the source branch and proposes a split plan. The user reviews and confirms (or edits) the plan, then sdf executes it.

```
sdf split --from feature/big-change
```

### Mode 2: `sdf split --from <branch> --plan <file>` (User-provided plan)

The user provides a YAML/JSON plan file describing exactly which files/commits go into which layer. sdf validates and executes it without AI involvement.

```
sdf split --from feature/big-change --plan split-plan.yaml
```

### Mode 3: `sdf split --from <branch> --interactive` (Hybrid)

Claude generates a plan, the user edits it in `$EDITOR`, then sdf executes the edited plan. This is Mode 1 with a mandatory edit step.

---

## The Split Plan

The split plan is the central data structure. Whether AI-generated or user-authored, it's the same format:

```yaml
source_branch: feature/big-change
base: main
stack_name: big-change

layers:
  - name: db-schema
    description: "Add users table with migration"
    files:
      - "migrations/001_create_users.sql"
      - "internal/models/user.go"
    commits: []  # empty = file-based assignment (see Strategy section)

  - name: repository
    description: "UserRepository with CRUD operations"
    files:
      - "internal/users/repository.go"
      - "internal/users/repository_test.go"
    depends_on_files:  # optional: files from earlier layers this layer references
      - "internal/models/user.go"

  - name: api-handler
    description: "REST endpoints for user management"
    files:
      - "internal/users/handler.go"
      - "internal/users/handler_test.go"
      - "internal/users/routes.go"
```

### Plan Validation Rules

Before execution, sdf validates:
1. Every changed file in the source branch appears in exactly one layer
2. No file appears in multiple layers
3. Layer ordering is compatible with import/dependency relationships (best-effort)
4. The plan covers the complete diff (no files left unassigned)
5. Layer names are valid branch names

---

## Split Strategies

There are two fundamentally different ways to decompose a branch:

### Strategy A: File-based splitting (primary)

Each layer gets a set of files. sdf constructs synthetic commits per layer by diffing the source branch against the base for just those files. This is the simplest and most reliable approach.

**How it works:**
1. Compute the full diff: `git diff main...feature/big-change`
2. For each layer, extract the diff hunks for its assigned files
3. Apply those hunks as a single commit on the layer's branch
4. If the source had meaningful commit messages, use Claude to synthesize an appropriate commit message per layer

**Tradeoff:** Commit history within the source branch is lost. Each layer gets one clean commit. For agent-generated code with messy intermediate commits, this is actually a feature.

### Strategy B: Commit-based splitting (advanced, future)

Each layer gets a set of commits by SHA. sdf cherry-picks them in order. This preserves commit granularity but is more fragile — commits may not apply cleanly out of order.

**How it works:**
1. List commits: `git log --oneline main..feature/big-change`
2. For each layer, cherry-pick its assigned commits onto the parent layer's branch
3. Handle conflicts (potentially via Claude)

**Tradeoff:** Preserves history but requires commits to be somewhat self-contained. Conflicts are likely if commits touch overlapping files.

**Recommendation:** Ship Strategy A first. It handles the 90% case (agent-generated code with messy commits). Strategy B can follow as an opt-in for repos with disciplined commit hygiene.

---

## AI Planning Phase (Mode 1)

### What Claude Receives

When `sdf split --from <branch>` is invoked, sdf constructs a prompt containing:

1. **Diff stat** — `git diff --stat main...feature/big-change` (file-level overview)
2. **Full diff** — `git diff main...feature/big-change` (complete changes) — or for very large diffs, a summarized version with full content only for key files
3. **Commit log** — `git log --oneline main..feature/big-change` (intent signal from commit messages)
4. **File tree** — `find` output of changed files with directory structure
5. **Import/dependency graph** — (optional, language-aware) which changed files import which other changed files

### What Claude Produces

Claude returns a split plan in YAML format (as defined above). The prompt instructs Claude to:

- Group files by logical concern (data layer, business logic, API, tests)
- Order layers from foundational to dependent (schema before repository before handler)
- Keep each layer under ~500 lines of diff where practical
- Write a one-line description for each layer explaining the "why"
- Ensure every changed file appears in exactly one layer
- Consider test files: pair them with the code they test when practical

### Prompt Template

```
You are analyzing a large branch to split it into a stack of smaller, reviewable PRs.

SOURCE BRANCH: {{.SourceBranch}}
BASE BRANCH: {{.Base}}
TOTAL FILES CHANGED: {{.FileCount}}
TOTAL LINES: +{{.Insertions}} -{{.Deletions}}

=== DIFF STAT ===
{{.DiffStat}}

=== COMMIT LOG ===
{{.CommitLog}}

=== FULL DIFF ===
{{.FullDiff}}

Produce a split plan as YAML. Rules:
1. Each layer should be a coherent, reviewable unit (aim for <500 lines each)
2. Order layers from foundational to dependent
3. Every changed file must appear in exactly one layer
4. Group test files with the production code they test
5. Name layers with short, descriptive kebab-case names
6. Write a brief description for each layer

Output ONLY the YAML, no explanation:

source_branch: ...
base: ...
stack_name: ...
layers:
  - name: ...
    description: "..."
    files:
      - ...
```

### Handling Large Diffs

For branches with >10,000 lines of diff, sending the full diff to Claude may exceed context limits. The strategy:

1. **First pass**: Send only the diff stat + commit log + file tree. Ask Claude to produce the plan based on file names and structure alone.
2. **Validation pass**: For each proposed layer, send the actual diff of those files and ask Claude to confirm the grouping makes sense or suggest adjustments.

This two-pass approach keeps each prompt manageable while still leveraging the full diff content.

---

## Execution Phase

Once the plan is confirmed (user approved or provided), sdf executes it:

### Step-by-step

```
1. Validate the plan (all files covered, no overlaps, names valid)

2. Create the sdf stack:
   sdf init <stack_name> --base <base> --branch <layers[0].name>

3. For each layer (starting from the first):
   a. If not the first layer: sdf branch <layer.name>
   b. Extract the diff for this layer's files from the source branch
   c. Apply the diff: git apply <layer-patch>
   d. Commit: git commit -m "<layer.description>"
   e. Push: git push -u origin <branch>

4. For each layer that should have a PR:
   sdf pr  (creates PR with layer description as body)

5. Clean up:
   - Save the split plan to .sdf/split-plans/<stack_name>.yaml for reference
   - Optionally close/supersede the original large PR
```

### Extracting Per-Layer Diffs

The key git operation is extracting a subset of the full diff:

```bash
# Get the full diff for specific files
git diff main...feature/big-change -- file1.go file2.go > layer.patch

# Apply it on the layer branch
git apply layer.patch
git add .
git commit -m "Layer description"
```

This is reliable because:
- `git diff` with file paths extracts exactly the changes to those files
- `git apply` applies them cleanly (they're against the same base)
- The first layer applies against `main`, the second against the first layer's tip, etc.

**Important subtlety:** Later layers may depend on files introduced in earlier layers. When a later layer's files import/reference symbols from an earlier layer's files, those symbols are already present (from the earlier layer's commit). So the code compiles at each layer boundary — which is exactly what we want for reviewable PRs.

### Handling Apply Failures

If `git apply` fails for a layer (e.g., because files have cross-dependencies that make the diff not apply cleanly in isolation):

1. **Try 3-way merge**: `git apply --3way layer.patch` — this uses the merge machinery and often succeeds
2. **Fall back to Claude**: Send the failed patch + the current branch state to Claude for resolution
3. **Fall back to manual**: Pause and let the user resolve, then `sdf split --continue`

---

## User Experience Flow

### Happy Path (Mode 1)

```
$ sdf split --from feature/big-change

Analyzing feature/big-change...
  42 files changed, +4,832 -201 (vs main)
  17 commits

Planning split...
  [streaming Claude output as it thinks]

Proposed stack "big-change" (4 layers):

  Layer 1: db-schema (12 files, +892 lines)
    Add users and sessions tables with migrations

  Layer 2: repository (8 files, +1,204 lines)
    UserRepository and SessionRepository with tests

  Layer 3: api-handlers (14 files, +1,891 lines)
    REST endpoints for user and session management

  Layer 4: integration-tests (8 files, +845 lines)
    End-to-end integration tests with testcontainers

  Total: 42 files, +4,832 lines across 4 layers

? Proceed with this split? (Y/n/edit)
  > Y    — execute the split as proposed
  > n    — abort
  > edit — open the plan in $EDITOR before executing

Executing split...
  ✓ Created stack "big-change" (base: main)
  ✓ Layer 1: big-change/db-schema — 12 files committed
  ✓ Layer 2: big-change/repository — 8 files committed
  ✓ Layer 3: big-change/api-handlers — 14 files committed
  ✓ Layer 4: big-change/integration-tests — 8 files committed
  ✓ Created PR #201: db-schema
  ✓ Created PR #202: repository (base: big-change/db-schema)
  ✓ Created PR #203: api-handlers (base: big-change/repository)
  ✓ Created PR #204: integration-tests (base: big-change/api-handlers)

Stack created:
  main ← db-schema (#201) ← repository (#202) ← api-handlers (#203) ← integration-tests (#204)

The original branch feature/big-change is untouched.
To close the original PR: gh pr close <number>
```

### Edit Flow

When the user chooses "edit", sdf:
1. Writes the proposed plan to a temp file
2. Opens it in `$EDITOR`
3. On save, validates the edited plan
4. If valid, proceeds with execution
5. If invalid, shows errors and re-opens the editor

### Session Continuity (Advanced)

For the case where the user wants to start a Claude Code session that has full context of the split plan:

```
$ sdf split --from feature/big-change --session

[Claude plans the split as above]
[User confirms]
[sdf executes the split]

Starting Claude Code session with stack context...
  Session has full awareness of all 4 layers and their intent.
  You can now ask Claude to make changes within the stack context.
```

This works by:
1. Completing the split
2. Launching `claude` with the split plan piped in as initial context
3. The session name is deterministic: `split-<stack_name>` so it's resumable

---

## Implementation Plan

### Phase 1: Core Plumbing (the git operations)

**New files:**
- `cmd/split.go` — Cobra command definition
- `internal/split/plan.go` — Plan data structures, validation, YAML parsing
- `internal/split/execute.go` — Plan execution (diff extraction, apply, commit)
- `internal/split/plan_test.go` — Plan validation tests
- `internal/split/execute_test.go` — Execution tests

**New git helpers needed** (in `internal/git/git.go`):
- `DiffFiles(from, to string, files []string) (string, error)` — diff for specific files
- `Apply(patch string) error` — apply a patch
- `ApplyThreeWay(patch string) error` — apply with 3-way merge fallback
- `DiffNameOnly(from, to string) ([]string, error)` — list changed file names
- `DiffStatFull(from, to string) (string, error)` — full diff stat

**Cobra command:**
```go
var splitCmd = &cobra.Command{
    Use:   "split",
    Short: "Split a large branch into a stack of smaller PRs",
    Long:  `Analyzes a branch and decomposes it into a chain of focused,
reviewable PRs managed as an sdf stack.`,
    RunE: runSplitCmd,
}

func init() {
    rootCmd.AddCommand(splitCmd)
    splitCmd.Flags().String("from", "", "source branch to split (required)")
    splitCmd.Flags().String("plan", "", "path to a split plan file (skip AI planning)")
    splitCmd.Flags().String("stack", "", "name for the resulting stack (default: derived from branch)")
    splitCmd.Flags().String("base", "", "base branch (default: auto-detect)")
    splitCmd.Flags().Bool("interactive", false, "open plan in $EDITOR before executing")
    splitCmd.Flags().Bool("session", false, "start a Claude session after splitting")
    splitCmd.Flags().Bool("yes", false, "skip confirmation prompt")
    splitCmd.Flags().Bool("dry-run", false, "show the plan without executing")
    splitCmd.Flags().Bool("json", false, "output machine-readable JSON")
    splitCmd.MarkFlagRequired("from")
}
```

**Plan structures:**
```go
// internal/split/plan.go

type Plan struct {
    SourceBranch string  `yaml:"source_branch" json:"source_branch"`
    Base         string  `yaml:"base" json:"base"`
    StackName    string  `yaml:"stack_name" json:"stack_name"`
    Layers       []Layer `yaml:"layers" json:"layers"`
}

type Layer struct {
    Name        string   `yaml:"name" json:"name"`
    Description string   `yaml:"description" json:"description"`
    Files       []string `yaml:"files" json:"files"`
}

func ParsePlan(data []byte) (*Plan, error) { ... }
func (p *Plan) Validate(changedFiles []string) []error { ... }
```

**Execution:**
```go
// internal/split/execute.go

type Executor struct {
    Root    string
    Git     gitInterface  // for testability
    GH      ghInterface
    Plan    *Plan
}

type Result struct {
    StackID  string        `json:"stack_id"`
    Layers   []LayerResult `json:"layers"`
}

type LayerResult struct {
    Branch  string `json:"branch"`
    Commits int    `json:"commits"`
    PR      int    `json:"pr,omitempty"`
    PRURL   string `json:"pr_url,omitempty"`
}

func (e *Executor) Execute() (*Result, error) { ... }
```

### Phase 2: AI Planning

**New file:**
- `internal/split/ai.go` — Prompt construction and response parsing

**The prompt builder:**
```go
func BuildAnalysisPrompt(sourceBranch, base string, diffStat, commitLog, fullDiff string) string {
    // Constructs the prompt template above
    // If fullDiff > threshold, uses two-pass strategy
}

func ParsePlanFromResponse(response string) (*Plan, error) {
    // Extracts YAML from Claude's response (handles fenced blocks)
}
```

**Integration with Claude CLI:**
- Uses `claudepkg.RunPromptStreaming()` for real-time output during planning
- Falls back to `claudepkg.RunPrompt()` if streaming isn't available
- Session name: `split-plan-<source-branch>`

### Phase 3: User Interaction (confirm/edit flow)

**Uses charmbracelet/huh for interactive prompts:**
```go
func PromptConfirmation(plan *Plan) (action string, err error) {
    // Shows plan summary
    // Returns "yes", "edit", or "abort"
}

func EditPlan(plan *Plan) (*Plan, error) {
    // Writes plan to temp file
    // Opens $EDITOR
    // Reads back, parses, validates
    // Returns edited plan or error
}
```

### Phase 4: PR Creation

After execution, for each layer:
1. Run `sdf pr` to create the GitHub PR with the layer description as body
2. Update stack navigation links across all PRs

### Phase 5: Session Continuity (--session flag)

After split + PR creation:
1. Launch `claude --session split-<stack_name>` with the split plan piped in as context
2. The session inherits awareness of all layers

---

## Testing Strategy

### Unit Tests

**Plan validation** (`internal/split/plan_test.go`):
- Missing files → error
- Duplicate files across layers → error
- File not in source diff → error
- Empty layer → error
- Invalid layer name → error
- Valid plan → no errors
- YAML parsing roundtrip

**Diff extraction** (`internal/split/execute_test.go`):
- Extract diff for subset of files
- Apply patch to clean branch
- Apply patch with 3-way merge
- Handle binary files in diff

**AI prompt construction** (`internal/split/ai_test.go`):
- Prompt includes all required sections
- Large diff triggers two-pass mode
- YAML extraction from fenced blocks
- Handles Claude returning invalid YAML gracefully

### Integration Tests (with real git repos)

**Test fixture: a repo with a known large branch:**
```go
func TestSplit_FileBasedThreeLayers(t *testing.T) {
    // 1. Create temp git repo with main branch
    // 2. Create feature branch with 15 files across 3 logical groups
    // 3. Run split with a hardcoded plan
    // 4. Verify: 3 branches created, each with correct files
    // 5. Verify: each branch compiles (files present from earlier layers)
    // 6. Verify: stack.json is correct
}

func TestSplit_PlanValidation(t *testing.T) {
    // Verify all validation rules with edge cases
}

func TestSplit_DryRun(t *testing.T) {
    // Verify dry-run shows plan without modifying repo
}

func TestSplit_UserProvidedPlan(t *testing.T) {
    // Verify --plan flag reads and executes external plan file
}
```

### Golden Tests

- Snapshot the plan output format
- Snapshot the execution summary output
- Snapshot the prompt template for AI planning

### E2E Tests

```go
func TestE2E_SplitAndCreatePRs(t *testing.T) {
    // Requires SDF_E2E_REPO + GH_TOKEN
    // 1. Create a large branch on the test repo
    // 2. Run sdf split with a plan
    // 3. Verify stack created with correct PR chain on GitHub
    // 4. Verify PR bases are correct
    // 5. Clean up
}
```

---

## Evaluation Criteria

### Correctness

1. **File coverage**: Every file in the source diff appears in exactly one layer branch
2. **No data loss**: The union of all layer diffs equals the full source diff
3. **Compilability**: Each layer branch should be a valid state (no dangling imports to files that arrive in later layers) — this is validated by the AI planner and can be checked with a build step
4. **Stack integrity**: The resulting `.sdf/stacks/<name>.json` is valid and `sdf status` shows the correct topology
5. **PR chain**: GitHub PRs have correct base branches forming a proper chain

### AI Quality (for Mode 1)

1. **Coherence**: Each layer is a logical unit (not random file groupings)
2. **Layer sizing**: Layers are roughly balanced in size, ideally <500 LOC each
3. **Dependency ordering**: Foundational changes come first (DB before business logic before API before tests)
4. **Description quality**: Layer descriptions accurately capture intent

### Robustness

1. **Dirty working tree**: Refuses to operate (like `sdf move`)
2. **Missing source branch**: Clear error
3. **Existing stack name collision**: Clear error
4. **Patch apply failure**: Graceful fallback (3-way merge → Claude → manual)
5. **Network failure during PR creation**: Partial progress saved, resumable

### Performance

1. **Small branch (10 files)**: Split should complete in seconds (excluding Claude API time)
2. **Large branch (100+ files)**: Git operations should still be fast; AI planning is the bottleneck
3. **Very large diff (10k+ lines)**: Two-pass AI planning keeps prompts manageable

---

## Open Questions and Future Work

### Open Questions

1. **Should `sdf split` modify the source branch?** Current design leaves it untouched. Alternative: delete it or mark it as superseded.

2. **How to handle non-file-splittable changes?** If a single file has changes that logically belong to two layers (e.g., a `go.mod` that adds deps for both the DB layer and the API layer), which layer gets it? Options:
   - Always put shared files in the earliest layer that needs them
   - Allow files to appear in multiple layers (with diff-level splitting — much more complex)
   - Let the AI decide and document the rationale

3. **Should we support hunk-level splitting?** Instead of assigning whole files to layers, assign individual diff hunks. This handles the `go.mod` case but adds significant complexity.

### Future Work

- **Strategy B (commit-based splitting)**: For repos with clean commit history
- **Language-aware splitting**: Use LSP or tree-sitter to understand import graphs and validate layer boundaries at the symbol level
- **Build verification**: After each layer, optionally run `make build` or equivalent to verify the layer compiles
- **Review assignment**: Auto-assign reviewers per layer based on CODEOWNERS
- **Split from PR number**: `sdf split --from-pr 42` — fetch the branch from the PR and split it
- **Undo**: `sdf split --undo <stack-name>` — delete the stack and restore the original branch state
- **Incremental updates**: If the source branch gets new commits, re-run split to update the stack

---

## Dependency Summary

**Existing infrastructure leveraged:**
- `internal/git/*` — all git operations (diff, apply, cherry-pick, branch, push)
- `internal/gh/*` — PR creation and editing
- `internal/claude/*` — AI prompt execution (both sync and streaming)
- `internal/stack/*` — stack topology management
- `internal/config/*` — prefix and naming configuration
- `internal/ui/*` — terminal styling
- `cmd/init.go`, `cmd/branch.go`, `cmd/pr.go` — reused for stack/branch/PR creation

**New dependencies:**
- `gopkg.in/yaml.v3` — YAML parsing for split plans (go.mod addition)

**No new external tools required.** `sdf split` uses the same `git`, `gh`, and `claude` CLIs that sdf already depends on.
