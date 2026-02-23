# `sdf split` — Design Document

## Overview

`sdf split` takes a single large branch and decomposes it into a stack of smaller, independently reviewable PRs — without touching the original branch. It uses a hybrid strategy that preserves clean commits, reshuffles messy ones, and decomposes large commits at the hunk level when needed.

The split produces a standard sdf stack, so all existing commands (`sdf sync`, `sdf status`, `sdf merge`, `sdf pr`) work on the result immediately.

```
Before:  main ← big-feature (847 insertions, 23 files, 15 commits)

After:   main ← stack/1-schema ← stack/2-api ← stack/3-ui ← stack/4-tests
         big-feature (untouched, still points at the same commit)
```

-----

## Core Guarantee

**The original branch is never modified.** The split creates new branches and new commits. If the split looks wrong, delete the stack branches and try again. The user's work is never at risk.

This is the trust anchor of the entire feature.

-----

## Command Surface

```
sdf split                        # analyze, propose plan, execute, push, create PRs
sdf split --plan-only            # produce plan YAML, don't execute
sdf split --local                # run build+tests per PR before pushing
sdf split --dry-run              # show what would happen, touch nothing
sdf split status                 # check CI status of split PRs
sdf split fix                    # auto-fix common mid-stack CI failures, re-push
```

Default mode is ship: execute the split, push all branches, create PRs, let CI validate in parallel.

-----

## The Pipeline

### Phase 1: Analysis

Read-only. Gather all information needed to produce a split plan.

**Inputs:**

- Base branch (from stack metadata or auto-detect from `origin/HEAD`)
- Full diff: `git diff base..HEAD`
- Commit list: `git log base..HEAD` (SHAs, messages, per-commit diffs)
- Per-commit file lists with change stats

#### Step 1a: Commit Quality Assessment

Classify each commit in the range `base..HEAD`:

| Classification | Meaning | Example | Action |
|---|---|---|---|
| CLEAN | Single concern, well-scoped | "add user schema migration" | Preserve as-is |
| MIXED | Multiple identifiable concerns | "add user API + fix linting" | Decompose by concern |
| MESSY | No clear structure | "WIP", "stuff" | Full decomposition |
| DEPENDENT | Fixes a prior commit's breakage | "fix tests" after a broken commit | Squash into parent |
| SCATTERED | Touches multiple earlier concerns | "address review feedback" | Distribute by concern |

This classification is performed by Claude, which reads each commit's diff and message to determine its nature.

#### Step 1b: Semantic Theme Extraction

Analyze the *entire diff* (not per-commit) to identify logical themes. Themes cut across commits — a "WIP" commit might contain schema changes, API code, and business logic. A "review feedback" commit scatters fixes across every theme.

Example themes:
```
T1: Database schema + migrations     (c1, part of c3, part of c6)
T2: API endpoints                    (part of c2, part of c3)
T3: Business logic / services        (part of c3)
T4: UI components                    (c5, part of c6)
```

#### Step 1c: Dependency Ordering

Determine which themes depend on which. The natural order is typically:

```
Schema/types → API/interfaces → Business logic → UI/presentation
                                     ↑
                              Tests travel with each
```

Constraints:
- Each PR must build and pass tests when applied on top of its parent
- Types and interfaces must be defined before they're used
- Tests travel with the code they test (not in a separate PR, unless pure integration tests that span the full surface)

-----

### Phase 2: Plan Generation

Produce a structured split plan. This is what the user reviews before anything is executed.

#### Split Strategies (per PR)

The tool picks the right strategy per-PR, not one-size-fits-all:

| Strategy | When Used | Git Operations |
|---|---|---|
| commit-preserving | Commits are already CLEAN | `git cherry-pick` directly |
| commit-reshuffling | Commits are MIXED/SCATTERED but separable by file | `git cherry-pick -n` + selective staging by file |
| commit-decomposition | Same file has changes belonging to different themes | `git cherry-pick -n` + selective staging by hunk |
| synthesized-refactor | AI restructures code for cleaner separation | Generate new code (flagged explicitly to user) |

#### The Hybrid Decision Flow

For a branch with 15 commits, the analysis might conclude:

```
Commits 1-3:   CLEAN, single concern each       → commit-preserving
Commits 4-10:  MIXED/MESSY, interleaved concerns → commit-reshuffling or decomposition
Commits 11-13: CLEAN tests                       → commit-preserving
Commit 14:     DEPENDENT (fixes c5)              → squash into c5's theme
Commit 15:     SCATTERED (review feedback)       → distribute across themes
```

The first three commits become PR 1 as-is. Commits 4-10 are decomposed by theme and redistributed across PRs 2-3. Commits 11-13 get distributed to the PRs whose code they test. Commit 14 is absorbed. Commit 15 is scattered back to its origins.

#### Same-File, Same-Method Splitting

The hardest case: a single function contains changes belonging to different themes.

```python
# Original (on main):
def get_user(user_id: int) -> User:
    user = db.query(User).get(user_id)
    if not user:
        raise NotFoundError()
    return user

# Modified (on big-feature):
def get_user(user_id: int, include_deleted: bool = False) -> UserResponse:
    cache_key = f"user:{user_id}"
    if cached := cache.get(cache_key):          # Theme: caching
        return UserResponse.from_orm(cached)

    query = db.query(User)
    if not include_deleted:                      # Theme: soft-delete
        query = query.filter(User.deleted_at.is_(None))

    user = query.get(user_id)
    if not user:
        raise APIError(404, "User not found",    # Theme: API error reform
                       error_code="USER_NOT_FOUND")

    cache.set(cache_key, user, ttl=300)          # Theme: caching
    response = UserResponse.from_orm(user)       # Theme: response models
    return response
```

Three concerns are woven into the same function. The decision framework:

1. **Are the changes separable without creating broken intermediate states?**
   YES → Line-level split (commit-decomposition).
   NO → Continue to 2.

2. **Is one concern clearly dominant (>70% of changes to this region)?**
   YES → Keep together, assign to dominant theme.
   NO → Continue to 3.

3. **Can a clean refactor create separation points?**
   YES → Generate a refactor-first PR (synthesized-refactor). This is flagged explicitly to the user.
   NO → Keep together in the most relevant theme.

The tool defaults to option 2 (keep together) when in doubt. Broken intermediate states are worse than slightly larger PRs.

#### Self-Containment Heuristic

To avoid trivial one-line PRs:

- If a theme produces fewer than ~10 meaningful lines, merge it into the most related theme
- If splitting a function creates an intermediate state that doesn't compile or pass type-checking, keep it together
- "Fixup" and "review feedback" commits are distributed back to their parent themes, not given their own PRs
- Each PR should tell a coherent story — a reviewer should understand *why* these changes go together

#### Plan Format

```yaml
split_plan:
  original_branch: big-feature
  base: main
  strategy: hybrid

  pr_stack:
    - title: "Add user schema and migration"
      strategy: commit-preserving
      source_commits: [c1, c6(partial)]
      files: [migrations/003.sql, models/user.go]
      tests: [models/user_test.go]
      estimated_diff: "+127 -31"

    - title: "Add user API endpoints"
      strategy: commit-decomposition
      source_commits: [c2, c3(partial), c4, c6(partial)]
      files: [api/users.go, handlers/users.go, errors.go]
      tests: [api/users_test.go]
      estimated_diff: "+389 -98"
      notes:
        - "get_user() rewrite kept together — caching + response model
           touch same lines, intermediate state would be broken"
        - "c4 ('fix tests') squashed into this PR — it fixed c2's breakage"

    - title: "Add user management UI"
      strategy: commit-preserving
      source_commits: [c5, c6(partial)]
      files: [components/UserList.tsx, components/UserForm.tsx]
      tests: [components/UserList.test.tsx]
      estimated_diff: "+241 -52"

    - title: "Add user integration tests"
      strategy: commit-preserving
      source_commits: [c7]
      files: [e2e/users_test.go]
      estimated_diff: "+90 -22"

  decisions:
    - "c4 squashed into PR 2 — it only fixed a test c2 broke"
    - "c6 ('review feedback') distributed across PRs 1, 2, 3 by concern"
    - "Linting fix in c2 (3 lines) kept with PR 2 — not worth a separate PR"

  file_overlap:
    - files: [models/user.go]
      prs: [1, 2]
      regions: "non-overlapping (PR1: schema, PR2: methods)"
```

The plan shows the *reasoning* behind grouping decisions, so the user can make informed adjustments.

-----

### Phase 3: User Review

The plan is presented to the user via an interactive prompt. They can:

- **Accept** as-is
- **Adjust boundaries** — move files or commits between PRs
- **Change ordering** — reorder PRs in the stack
- **Rename** — change PR titles
- **Merge** — combine two proposed PRs into one
- **Reject** — re-analyze with guidance ("keep caching and API together", "split tests into their own PR")

The plan can also be written to a YAML file (`sdf split --plan-only`) for offline editing, then executed later (`sdf split --from-plan <file>`).

-----

### Phase 4: Execution

Create branches and apply changes. The original branch is never modified.

```
For each PR in the plan (ordered base-to-tip):
  1. Determine parent:
     - PR 1: parent is base branch (e.g., main)
     - PR N: parent is stack branch N-1
  2. Create branch from parent tip
  3. Apply changes using the strategy:
     - commit-preserving:
         git cherry-pick <commits>
     - commit-reshuffling:
         git cherry-pick -n <commits>
         git reset HEAD
         git add <files-for-this-PR>
         git commit
         git checkout -- .    # discard remaining changes
     - commit-decomposition:
         git cherry-pick -n <commits>
         git reset HEAD
         git add -p           # stage specific hunks
         git commit
         git checkout -- .
     - synthesized-refactor:
         Write generated files
         git add <files>
         git commit
  4. Record branch in new stack
```

#### Branch Naming

Follows existing sdf conventions. Uses the stack ID as prefix with the configured separator:

```
Stack ID: users-feature
Separator: /

Branches:
  users-feature/1-schema
  users-feature/2-api
  users-feature/3-ui
  users-feature/4-tests
```

Respects `branch_prefix` configuration. The numeric prefix ensures merge order is unambiguous.

#### Stack Registration

Creates a new `.sdf/stacks/<name>.json` with the split stack topology. This means all existing sdf commands work immediately:

- `sdf status` shows the new stack
- `sdf sync` cascade-rebases if any PR is amended
- `sdf merge` merges the head PR and shifts the stack
- Context docs can be added to each branch

-----

### Phase 5: Validation

Runs automatically after execution. Four layers, from cheapest to most thorough.

#### Layer 1: Tree Identity (instant, hard gate)

The strongest guarantee. The final tree of the stack must be byte-identical to the original branch's tree.

```bash
git diff original-branch stack/last
# Must produce: empty output
```

If this diff is empty, then mathematically no change was lost or invented. The stack is a lossless decomposition of the original.

This is cheap, deterministic, and a hard gate — if it fails, the split is rejected and nothing is pushed.

**Exception:** If the user opted into a synthesized refactor, the trees will intentionally differ. In that case, the tool flags the specific files that differ and why:

```
⚠ Tree differs from original branch (synthesized refactor in PR 3)

  Files that differ:
    api/users.go      — caching extracted to decorator
    cache/decorators.go — new file (not in original)

  Run tests to verify semantic equivalence.
```

#### Layer 2: Patch Accounting (instant, informational)

Verify that the sum of all PR diffs equals the original diff:

```
Original diff: 23 files changed, +847 -203

Stack accounting:
  PR 1 (schema):     4 files,  +127  -31
  PR 2 (API):        8 files,  +389  -98
  PR 3 (UI):         6 files,  +241  -52
  PR 4 (int tests):  5 files,   +90  -22
  ─────────────────────────────────────────
  Total:            23 files,  +847 -203  ✓ matches original

Files in original but not in any PR: none  ✓
Files in stack but not in original: none   ✓
```

#### Layer 3: File Overlap Analysis (instant, informational)

When the same file appears in multiple adjacent PRs, verify the changes target non-overlapping regions:

```
File overlap analysis:
  api/users.go appears in PR 2 and PR 3
    PR 2 changes: lines 14-45 (endpoint handlers)
    PR 3 changes: lines 89-102 (response serialization)
    Overlap: none ✓

  models/user.go appears in PR 1 and PR 2
    PR 1 changes: lines 5-12 (add deleted_at column)
    PR 2 changes: lines 5-8 (add deleted_at column) ⚠ OVERLAP
    → Risk: PR 2 may conflict when rebased after PR 1 merges
```

#### Layer 4: Build + Test Verification (mode-dependent)

For each PR in the stack, verify it independently:

- **`--ship` mode (default):** Skip local verification. Push all PRs and let CI run in parallel across all of them simultaneously. This is faster (5 minutes CI vs 20 minutes sequential local) and CI is the canonical environment.

- **`--local` mode:** Run build + typecheck + lint + tests per PR sequentially before pushing. For repos without CI, or when you want confidence before pushing to a shared repo.

#### Validation Report

After validation completes:

```
Split validation report
═══════════════════════

Tree identity:
  git diff big-feature stack/4-tests  →  ✓ identical

Patch accounting:
  Files:       23/23 accounted for    ✓
  Insertions:  847/847                ✓
  Deletions:   203/203                ✓

File overlap:
  2 files in multiple PRs
    models/user.go (PR 1, PR 2)  →  ✓ non-overlapping regions
    api/users.go (PR 2, PR 3)    →  ✓ non-overlapping regions

Result: Split is valid ✓
```

If any check fails, the split is rejected and the user sees the specific failure with a suggested fix.

-----

### Phase 6: Ship

Default mode. Runs after validation passes.

```
For each PR in the stack:
  1. git push -u origin <branch>
  2. gh pr create --title <title> --body <body> --base <parent-branch>
```

All branches are pushed, then all PRs are created. PR bodies include:

- Stack navigation links (reusing existing `prnav.go` logic)
- The split plan's notes for this PR (explaining what's in it and why)
- A reference to the original branch: "Split from `big-feature`"
- Per-PR estimated diff stats

The stack is then fully functional — mergeable head-to-tail, each PR independently reviewable, CI running on all PRs in parallel.

-----

### Phase 7: Post-Ship Monitoring

#### `sdf split status`

Check CI results across the entire split stack:

```
sdf split status

  PR #201 (schema)     ✓ CI passed
  PR #202 (API)        ✗ CI failed: ImportError 'UserResponse' (defined in PR 3)
  PR #203 (UI)         ✓ CI passed
  PR #204 (tests)      ✓ CI passed

  Suggested fix: Move UserResponse definition from PR 3 to PR 2
  Run: sdf split fix
```

#### `sdf split fix`

Diagnoses common mid-stack CI failures and applies targeted fixes:

- Missing imports (type defined in later PR, used in earlier one)
- Missing migration dependencies
- Undefined types or functions at intermediate stack levels
- Test fixtures not available at the PR's stack level

After fixing, force-pushes only the affected PRs. Other PRs are untouched.

-----

## Integration With Existing sdf

The split creates a standard sdf stack. After splitting:

| Command | Behavior |
|---|---|
| `sdf status` | Shows the split stack with all PRs and their CI state |
| `sdf sync` | Cascade-rebases if any PR in the split is amended |
| `sdf merge` | Merges the head PR and shifts the stack forward |
| `sdf pr` | Works on new branches added to the split stack |
| `sdf context` | Context docs can be created for each split branch |
| `sdf switch` | Navigate between split branches |

The original branch remains as a reference. The user can delete it when satisfied with the split, or keep it indefinitely.

-----

## Architecture

Following existing codebase patterns. All packages shell out to `git`, `gh`, and `claude` — no reimplementation.

| Component | Location | Purpose |
|---|---|---|
| `cmd/split.go` | Cobra command | CLI entry point, flags, orchestration |
| `internal/split/analyze.go` | Analysis engine | Commit classification, theme extraction via Claude |
| `internal/split/plan.go` | Plan model | Plan structure, YAML serialization, user-facing display |
| `internal/split/execute.go` | Execution engine | Branch creation, cherry-pick, selective staging |
| `internal/split/validate.go` | Validation suite | Tree identity, patch accounting, overlap analysis |
| `internal/split/fix.go` | Post-ship repair | Diagnose + fix mid-stack CI failures |

### Dependencies on existing packages

- `internal/git` — branch creation, cherry-pick, diff, rebase operations
- `internal/gh` — PR creation, PR editing, PR state queries
- `internal/claude` — theme extraction, split reasoning, fix suggestions
- `internal/stack` — stack creation, persistence, topology
- `internal/config` — branch prefix, PR title generation
- `internal/ui` — interactive plan review, progress display
- `cmd/prnav.go` — stack navigation links in PR bodies

### New git operations needed

The existing `internal/git` package will need additions:

```go
// Cherry-pick without committing (for selective staging)
func CherryPickNoCommit(sha string) (string, error)

// Stage specific files (for commit-reshuffling)
func AddFiles(paths []string) (string, error)

// Stage specific hunks via patch (for commit-decomposition)
func ApplyPatch(patch string) (string, error)

// Compare two trees for identity
func DiffTree(ref1, ref2 string) (string, error)

// Get per-file diff stats between two refs
func DiffStatFiles(base, head string) ([]FileStat, error)

// Get hunks for a specific file between two refs
func DiffFileHunks(base, head, file string) ([]Hunk, error)
```

-----

## Testing Strategy

Following existing patterns:

| Test Type | What It Covers |
|---|---|
| Unit tests (`*_test.go`) | Plan generation, validation logic, commit classification |
| Golden file tests | Plan YAML output, validation reports, PR body formatting |
| Property-based tests | Invariant: tree identity holds for random commit sequences |
| E2E tests | Full split pipeline against real GitHub sandbox repo |

### Key test scenarios

1. **Clean branch** — all commits are well-structured, commit-preserving strategy throughout
2. **Messy branch** — WIP commits, fixups, review feedback scattered across concerns
3. **Same-file split** — two themes modify the same file in non-overlapping regions
4. **Same-function split** — two themes modify the same function (should keep together)
5. **Single commit branch** — one giant commit that needs full decomposition
6. **Already a stack** — branch is already well-structured, split is a no-op or trivial
7. **Synthesized refactor** — tree identity check correctly flags intentional differences
8. **Empty split** — no changes to split (error case)

-----

## Design Principles

1. **Original branch is never touched.** The split is a non-destructive projection. This is non-negotiable.

2. **Each PR builds and passes tests independently.** Mergeable head-to-tail. If merging PR 1 leaves failing tests, the split boundary is wrong.

3. **Tests travel with the code they test.** Unit tests go in the PR that introduces the code. Integration tests go in the PR that completes the surface they exercise.

4. **Tree identity is the mechanical trust anchor.** If `git diff original stack/tip` is empty, nothing was lost. Everything else validates that intermediate states are sane.

5. **Ship fast, validate in CI.** Push all PRs and let CI run in parallel by default. Local validation is opt-in for repos without CI.

6. **Hybrid strategy, not one-size-fits-all.** Clean commits are preserved. Messy commits are decomposed. The tool picks the right approach per-region automatically.

7. **Transparent reasoning.** The plan shows *why* changes were grouped the way they were. No black-box splitting — the user sees the logic and can adjust.

8. **Optimize for review experience.** Each PR should tell a coherent story. A reviewer should understand the intent without cross-referencing other PRs.

-----

## Open Questions

1. **Plan editing UX** — Interactive TUI editor vs. YAML file editing vs. simple accept/reject/adjust prompts? The existing `charmbracelet/huh` prompts may be sufficient for simple adjustments; a YAML file is better for complex rearrangements.

2. **Claude context window** — Very large diffs may exceed Claude's context. Strategy: hierarchical analysis (file-level first, then hunk-level for files that need decomposition) rather than sending the entire diff at once.

3. **Partial re-split** — Should `sdf split fix` be able to re-split a single PR in the stack without touching the others? This would be useful when CI reveals that one PR is still too large or has the wrong boundaries.

4. **Relationship to original branch** — After a successful split, should the tool suggest deleting the original branch? Or keep it as a permanent reference? Current design: keep it, let the user decide.

5. **Config options** — What should be configurable? Candidates: minimum PR size threshold, default validation mode (ship vs local), branch naming pattern, whether to auto-create context docs for split branches.
