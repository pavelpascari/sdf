# Stack Reconciliation Design

Date: 2026-02-20
Status: Approved

## Problem

Local `.sdf/stacks/*.json` files can drift from the actual PR chain on GitHub.
This happens when branches are added from another machine, PRs are merged on
GitHub, PR bases are retargeted, or `register` is run multiple times creating
duplicates. The current `register` command hard-errors on name collisions and
has no concept of updating an existing stack.

## Core Principles

### GitHub PRs are the source of truth

The PR dependency graph on GitHub (each PR's base branch → head branch)
defines the stack topology. Local stack files are a cached view of that truth.
Reconciliation means updating the cache to match.

### A stack is a chain, not a set of branches

A stack requires at least 2 PRs chaining: A→main, B→A, C→B. Ten independent
PRs all targeting main are zero stacks — they're unrelated PRs.

### Branch uniqueness invariant

A branch can belong to at most one stack. This is enforced at every mutation
point (`init`, `branch`, `fetch`). Violation is a bug, not a user decision.

### Always show what happened

Every auto-applied change prints a line. Nothing is silent. Notable changes
(insertions, reordering, removals) get `⚠` emphasis so the user notices
topology shifts.

## Reconciliation Model

All changes are deterministic from GitHub data — there is only one correct
local state that matches the PR chain. No user confirmation needed. Changes
are classified as routine or notable for output purposes only.

### Routine changes (normal `✓` output)

- **Status updated**: PR merged/closed on GitHub, local said "open"
- **PR number filled**: branch existed locally without a PR, now has one
- **Branch appended**: new branch added to end of chain

### Notable changes (emphasized `⚠` output)

- **Branch inserted**: new branch appeared in the middle of the chain
- **Branch reordered**: PR bases changed on GitHub, topology differs
- **Branch removed**: PR retargeted away from this chain
- **Base branch changed**: chain now roots on a different branch

## Architecture

### Shared reconcile function

```
internal/stack/reconcile.go

type ReconcileChange struct {
    Kind     string  // "add", "remove", "reorder", "status", "pr-number",
                     // "insert", "rebase-root"
    Branch   string
    Detail   string  // human-readable: "PR #21: open → merged"
    Notable  bool    // true = ⚠ emphasis, false = ✓ routine
}

func Reconcile(local *Stack, discovered DiscoveredStack) []ReconcileChange
```

Pure logic, no I/O, fully testable. Compares local stack topology against
discovered PR chain and returns a list of changes needed.

### Branch uniqueness validation

```
internal/stack/validate.go

func ValidateBranchUniqueness(root, branch string) error
```

Called from `init`, `branch`, `fetch`, and the reconcile application path.
Returns error if the branch already exists in any stack.

## Command Changes

### Rename `register` → `fetch`

`sdf fetch` replaces `sdf register`. Discovery from GitHub PRs, plus:

- **No local stacks**: create new stack(s) from discovered chains
  (current register behavior)
- **Overlapping stack found**: run `Reconcile()`, apply all changes, show
  output
- **Branch uniqueness**: checked before any mutation
- **Backward compat**: `sdf register` still works, prints hint to use `fetch`

### `sdf sync` gains lightweight reconciliation

During the existing PR poll phase (sync already calls `gh pr list`), sync also:

1. Builds the discovered chain from PR data it already has
2. Runs `Reconcile(local, discovered)`
3. Applies routine changes inline (status, PR numbers, appended branches),
   printing each one
4. For notable changes (insert, reorder, remove), prints `⚠` warning and
   suggests `sdf fetch`

No extra GitHub API calls — sync reuses the PR data it already fetches.

## Output Examples

### `sdf fetch` — first time (was `register`)

```
Scanning your open PRs (base: main)...

Found 1 stack:

  ├─ feat/schema   PR #10
  ├─ feat/api      PR #11
  └─ feat/ui       PR #12

Registered stack "feat" with 3 branches (base: main)
```

### `sdf fetch` — reconciling existing stack

```
Scanning your open PRs (base: main)...

Reconciling stack "feat":
  ✓ PR #10 (feat/schema) status updated: open → merged
  ✓ added feat/endpoint to stack (PR #14)
  ⚠ feat/api moved: position 2 → 3 (PR bases changed on GitHub)

Stack "feat" updated. Run `sdf sync` to rebase.
```

### `sdf sync` — inline reconciliation

```
Syncing stack "feat"...
Fetching from origin...
  ✓ PR #10 (feat/schema) merged
  ✓ added feat/endpoint to stack (PR #14)
  rebasing feat/api onto main...
  ✓ feat/api rebased and pushed
```

### `sdf sync` — notable drift detected

```
Syncing stack "feat"...
Fetching from origin...
  ✓ PR #10 (feat/schema) merged
  ⚠ stack topology changed on GitHub — run `sdf fetch` to reconcile

Sync aborted. Reconcile first with `sdf fetch`.
```

## New Files

- `internal/stack/reconcile.go` — `Reconcile()`, `ReconcileChange` type
- `internal/stack/reconcile_test.go` — tests for every change kind
- `internal/stack/validate.go` — `ValidateBranchUniqueness()`
- `cmd/fetch.go` — renamed from `register.go`, adds reconciliation path

## Test Scenarios

1. First-time registration (reconcile against empty stack)
2. Append: local A→B, GitHub A→B→C
3. Insert: local A→C, GitHub A→B→C
4. Reorder: local A→B→C, GitHub A→C→B
5. Remove: local A→B→C, GitHub A→C
6. Status update: local open, GitHub merged
7. PR number fill: branch without PR gains one
8. Base change: local base=main, GitHub roots on develop
9. No change: local matches GitHub exactly
10. Branch uniqueness violation: branch already in another stack
11. Sync inline reconciliation: appended branch during sync
12. Sync drift warning: reorder detected during sync

## Relationship to Roadmap

This design replaces and extends the `stack-resilience` roadmap:

- **Branch 1**: reconcile function + `sdf fetch` + branch uniqueness invariant
- **Branch 2**: `sdf sync` integration (lightweight reconciliation)
- **Branch 3**: drift detection in `sdf status` (uses same reconcile output)

The insert-at-position branching from the original roadmap becomes a natural
extension — `sdf branch` inserts at checkout position, and reconciliation
ensures other machines pick up the change via `fetch`/`sync`.
