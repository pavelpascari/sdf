# Improve `sdf new` DX (formerly `sdf init`)

**Date:** 2026-02-19
**Status:** Implemented (PRs #16, #17, #18)

## Problem

Running `sdf new` (formerly `sdf init`) creates the stack file but leaves the user on `main` with no branch and unclear next steps. The gap between creating a stack and productive work is too wide.

## Design

`sdf new` becomes a single-step operation that creates the stack AND the first branch.

### New behavior

```
sdf new <stack-name> [--base <branch>] [--branch <name>] [--json]
```

1. Create `.sdf/` structure + stack file (existing)
2. Create config file (existing)
3. Determine first branch name — defaults to `<stack-name>`, overridden by `--branch`
4. Apply branch prefix (respecting config, same as `RunBranch`)
5. Create git branch and checkout
6. Register node in stack, save
7. Push tracking branch to origin
9. Print summary + next steps (or JSON if `--json`)

### Implementation (Stacked Diffs — init-dx)

| Branch | PR | Scope |
|--------|-----|-------|
| `init-dx/branch-creation` | #16 | Core: init creates first branch |
| `init-dx/json-output` | #17 | `--json` flag for agents |
| `init-dx/docs` | #18 | README + help text updates |

---

# Stack Resilience: Git as Source of Truth

**Date:** 2026-02-19
**Status:** Design — pending init-dx merge

## Problem

The stack JSON (`stacks/<name>.json`) is the sole source of truth for branch ordering and parent-child relationships. This causes several issues:

1. **`sdf branch` always appends to the end** — if you're checked out on an earlier branch and want to insert a new branch after it, the branch lands at the tail of the stack instead
2. **No drift detection** — if branches are rebased manually outside sdf, or PR bases change on GitHub, the stack JSON silently becomes stale
3. **No reconciliation** — there's no mechanism to detect or recover from JSON-vs-git mismatches

## Design Principles

- **Stack JSON is the intended state** — it records the planned ordering and PR metadata
- **Git + GitHub is the actual state** — branch tips, merge-bases, and PR base branches reflect reality
- **sdf identifies and resolves the delta** — when intended and actual state diverge, sdf reports the drift and suggests corrections

## Feature 1: Insert-at-position branching

**Current:** `sdf branch foo` always appends `foo` as the last node, based on the tail of the stack.

**New:** `sdf branch foo` uses the current checkout branch to determine position.

- If you're on `branchA` (node index 1 in stack), `sdf branch foo` inserts `foo` at index 2, based on `branchA`
- Downstream branches (`branchB`, `branchC`, etc.) shift down in the array
- Their BaseTip values and PR bases remain unchanged (they still point at their original parents)
- If current checkout is not in the stack, falls back to appending at end (current behavior)

### Implications for downstream branches

When a branch is inserted mid-stack, the branches after the insertion point now have a new sibling, NOT a new parent. Their parent relationships don't change — `branchB` still sits on top of `branchA`, not on top of the newly inserted `foo`. The stack becomes:

```
main ← branchA ← foo (new)
                ← branchB ← branchC
```

This is a **fork in the stack**, not a linear chain. We need to decide:

**Option A: Keep linear** — inserting mid-stack makes the new branch the parent of everything below it. Downstream branches get rebased onto the new branch. The stack stays linear but downstream branches change.

**Option B: Allow forks** — the stack becomes a DAG. Multiple branches can share a parent. This is more flexible but adds complexity.

**Recommendation: Option A (keep linear).** Stacked diffs are inherently linear chains. Forks are better modeled as separate stacks.

## Feature 2: Drift detection

Before mutations (sync, branch, pr), sdf validates the stack JSON against git/GitHub:

### Checks

| Check | Source | Detection |
|-------|--------|-----------|
| BaseTip staleness | git | `git merge-base <branch> <parent>` vs stored BaseTip |
| PR base mismatch | GitHub | PR's base branch (from `gh`) vs expected parent in stack |
| Branch existence | git | Branch in stack JSON but deleted locally or on remote |
| Merged PR not recorded | GitHub | PR state is merged but stack JSON says "open" |

### Reporting

```
sdf status init-dx

  init-dx  (base: main)

   ●  init-dx/branch-creation   PR #16  open     in sync
   ⚠  init-dx/json-output       PR #17  open     drift: PR base is main, expected init-dx/branch-creation
   ●  init-dx/docs              PR #18  open     in sync
```

### Reconciliation

- **Safe auto-fix:** merged PRs → update status in JSON, prune node
- **Warn + suggest:** BaseTip drift → "run `sdf sync` to reconcile"
- **Error:** branch deleted → "branch X no longer exists, remove from stack with `sdf prune`?"

## Feature 3: PR base update on branch insert

When a branch is inserted mid-stack, the branch immediately after the insertion needs its PR base updated:

```sh
# Before: branchB PR base = branchA
# Insert foo after branchA
# After: foo PR base = branchA, branchB PR base = foo
gh pr edit <branchB-PR> --base foo
```

This keeps GitHub PRs showing the correct diff.

## Implementation Plan (Stacked Diffs — stack-resilience)

### Stack: `stack-resilience` (from main, after init-dx merges)

#### Branch 1: `stack-resilience/insert-branch`
**Scope:** `sdf branch` inserts at current checkout position.

- Modify `cmd/branch.go` to detect current branch position in stack
- Insert node at correct index instead of appending
- Rebase downstream branches onto the new branch (linear chain)
- Update PR bases for shifted branches via `gh pr edit`
- Tests with multi-branch temp repos

#### Branch 2: `stack-resilience/drift-detection`
**Scope:** Detect and report stack-vs-git drift.

- Add validation functions to `internal/stack/`
- Compare BaseTip vs `git merge-base`
- Compare PR base branches vs expected parents (via `gh`)
- Integrate into `sdf status` output (drift warnings)
- Tests for each drift scenario

#### Branch 3: `stack-resilience/reconciliation`
**Scope:** Auto-fix safe drifts, suggest fixes for others.

- Auto-update merged PR status in stack JSON
- `sdf sync` reconciles BaseTip drift during rebase
- Add `sdf prune` or similar for cleaning up deleted branches
- Integration tests
