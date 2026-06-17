# sdf ↔ flow Integration — Design

**Date:** 2026-06-17
**Status:** Approved (pending spec review)
**Target:** sdf v0.5.0

## Goal

Make sdf satisfy flow's integration contract: a set of headless, `--json`,
idempotent jobs that flow (the orchestrator) invokes to drive a worktree-mode
stack. flow supplies the agent in each worktree and polls a coordinator; sdf is
the substrate that stacks, syncs, and surfaces PR/branch state. This release
closes the gaps between flow's contract and sdf v0.4.0.

sdf does **not**: trigger/report CI, spawn agents, run reviews, or autonomously
merge to the default branch. Those are flow's.

## Current state (v0.4.0) vs contract

Already satisfied (no work): `status --json` already emits `sync_state`,
`worktree_path`, `pr`, `status`, `ci_status`, `review_status`, `mergeable`,
`is_draft` (J5); `switch --path-only` (J10); branch-as-worktree (J2 creation);
`prune` (J9); worktree pull-model `sync` (J6 mechanics).

Gaps this spec closes: idempotency of `new`/`branch`/`pr` (J1–J3); `worktree_path`
in `new --json` (J1); `pr --draft` + `pr --ready` + PR result fields (J3/J4);
`conflicted` in the `sync_state` enum (J5); structured `{status, conflicts}` for
worktree `sync`/`--continue` with conflict-≠-error semantics (J6/J7); a
distinguishable lock-timeout error (concurrency); a test confirming external-merge
propagation (J8).

## Design

### A. Idempotency by default (J1/J2/J3)

`new`, `branch`, and `pr` currently hard-error when the target already exists.
flow re-issues commands on crash-resume, so re-running must succeed and return the
current state.

- `new <stack>`: if the stack already exists, load it and return the existing
  first branch's `NewResult` (do not recreate the stack, branch, or worktree).
- `branch <task>`: if the branch already exists in the stack, return its
  `NewBranchResult` (`branch`, `parent`, `worktree_path`) without recreating the
  branch or re-adding the worktree.
- `pr`: if the branch already has a PR, return its `PRResult` (replaces the
  `"branch %q already has PR #%d"` error).

Each result gains an additive `"created": bool` field: `true` when this invocation
created the resource, `false` when it already existed. In non-`--json` mode, an
"already exists — returning current state" note is printed to stderr; exit code is
0 in both modes. No prompts.

The idempotency check runs **inside the existing stack lock** (`stack.WithLock`)
where the command already mutates stack state, so a concurrent create cannot race
the existence check.

### B. `sdf pr` — draft, ready, fields (J3/J4)

- New flag `--draft`: opens the PR as a draft via `gh pr create --draft`. The gh
  wrapper signature becomes `ghpkg.PRCreate(title, body, base, head string, draft bool)`.
- New flag `--ready [--branch <b>]`: flips a draft PR to ready via `gh pr ready <n>`
  (new `ghpkg.PRReady(number int) error`). Idempotent — marking an already-ready PR
  is a no-op success. With `--branch`, targets that branch's PR; without, the
  current worktree's branch. `--ready` does not create a PR; if none exists it errors
  clearly ("no PR for branch %q").
- `PRResult` gains `pr` (alias of `number`), `draft` (bool), and `created` (bool):
  ```go
  type PRResult struct {
      Number  int    `json:"number"`
      Pr      int    `json:"pr"`      // == Number, for flow's contract
      URL     string `json:"url"`
      Title   string `json:"title"`
      Draft   bool   `json:"draft"`
      Created bool   `json:"created"`
  }
  ```
  `number` and `title` are retained (no breaking change).

### C. `worktree_path` in `NewResult` (J1)

```go
type NewResult struct {
    Stack        string `json:"stack"`
    Base         string `json:"base"`
    Branch       string `json:"branch"`
    WorktreePath string `json:"worktree_path,omitempty"` // first branch's worktree
    Pushed       bool   `json:"pushed"`
    Created      bool   `json:"created"`
}
```
`worktree_path` is populated from the first branch's node when the stack is in
worktree mode (`--worktrees`); empty otherwise.

### D. `status` — `conflicted` sync_state (J5)

`status` currently sets `sync_state` only to `in_sync` or `needs_sync`. Add
`conflicted`: when a node has a worktree (`worktree_path != ""`) with a paused
rebase (`gitpkg.IsRebaseInProgressAt(worktree_path)` is true), set
`sync_state:"conflicted"`. Precedence: `conflicted` overrides `needs_sync`.

Documented `sync_state` enum (in code doc + README):
- `in_sync` — branch is current with its (effective) parent.
- `needs_sync` — parent advanced (or a parent merged); branch must be synced.
- `conflicted` — a rebase is paused in this branch's worktree, awaiting resolution.
- `""` (omitted) — base branch / node without a meaningful sync state.

`status` (`StatusNodeResult.Status`) remains `open | merged | closed`.

### E. Worktree `sync` / `sync --continue` structured status (J6/J7)

This is the load-bearing semantic: **a rebase conflict is a normal, actionable
outcome, not a process failure.** Today the worktree sync step returns a Go error
on conflict, which `runSyncCmd` places in `SyncResult.error` (with exit 0 in
`--json` mode) and never emits a structured status — flow would have to parse the
error string.

Extend the existing `SyncResult`/`BranchResult` shape (no new top-level object):

```go
type BranchResult struct {
    Branch      string   `json:"branch"`
    PR          int      `json:"pr,omitempty"`
    Action      string   `json:"action"`            // existing (monolithic sync)
    Status      string   `json:"status,omitempty"`  // NEW: clean | noop | conflicted
    Conflicts   []string `json:"conflicts,omitempty"` // NEW: paths, when conflicted
    Pushed      bool     `json:"pushed,omitempty"`
    BaseUpdated bool     `json:"base_updated,omitempty"`
    Reason      string   `json:"reason,omitempty"`
}
```

Worktree `sync` (run inside a branch's worktree) populates exactly one entry in
`branches`:
- **`clean`** — branch rebased onto its parent and pushed (`pushed:true`).
- **`noop`** — already up to date with parent (nothing rebased/pushed).
- **`conflicted`** — rebase paused; `conflicts` lists the conflicted paths; the
  branch's `SyncProgress`/`WorktreeProgress` is recorded for `--continue`.

On conflict the step returns **no Go error**; `SyncResult.error` stays empty and
the process exits 0. `SyncResult.error` (and `error_code`, §F) are reserved for
*real* failures (lock timeout, push rejection that isn't a conflict, IO errors).

`sync --continue` (also inside the worktree) returns the same shape: `clean` once
the resolved rebase completes and pushes, or `conflicted` again (with the
remaining `conflicts`) if still unresolved.

flow reads `branches[0].status`. The monolithic (non-worktree) sync path is
unchanged — it continues to populate `action` and leaves `status` empty.

### F. Distinguishable lock-timeout error (concurrency requirement)

When `stack.AcquireLock` times out (another sdf process holds the lock), the
failure must be retryable-distinguishable, not an opaque error flow escalates on.

- `SyncResult` (and the other `--json` results: `NewResult`, `NewBranchResult`,
  `PRResult`, status) gain an additive `error_code string json:"error_code,omitempty"`.
- On lock-timeout, `--json` output sets `error` (human message) **and**
  `error_code:"lock_timeout"`.
- Non-`--json` invocations exit with a distinct, documented non-zero code
  (`75`, EX_TEMPFAIL) for lock-timeout, vs `1` for ordinary errors.
- Implementation: `AcquireLock` returns a sentinel (`stack.ErrLockTimeout`);
  command wrappers map it to `error_code`/exit 75.

### G. External-merge propagation (J8) — confirm with a test

`status`/`fetch` already fetch from origin and poll `gh` PR state, and a merged
parent yields a child `needs_sync` via `ParentBranch` skipping merged nodes. Add
an integration test asserting: after a PR is marked merged on the remote, a
subsequent `status --json` shows that node `status:"merged"` and its child
`sync_state:"needs_sync"`. Add code only if the test exposes a gap (none expected).

### H. Cross-cutting & non-goals

- **Headless:** every flow-invoked command (`new`, `branch`, `pr`, `pr --ready`,
  `sync`, `sync --continue`, `status`, `fetch`, `prune`, `switch`) runs without
  prompts. Verify none prompt in the flow paths (they don't today).
- **Stable field names:** all JSON changes are additive except the idempotency
  *behavior* change. No field is renamed or removed.
- **Non-goals (unchanged):** sdf does not trigger/report CI, spawn agents, run
  reviews, or autonomously merge to the default branch.

## Testing

- Idempotency: re-run `new`/`branch`/`pr` and assert exit 0, `created:false`, and
  identical resource (no duplicate node/PR/worktree).
- `pr --draft`: asserts `draft:true` and a draft PR (via the gh spy).
- `pr --ready`: asserts a draft flips to ready (`draft:false`), idempotent on
  already-ready, errors when no PR exists.
- `new --json`: asserts `worktree_path` present for `--worktrees`, empty otherwise.
- `status`: a node with a paused rebase in its worktree reports
  `sync_state:"conflicted"`.
- Worktree `sync` contract: real-conflict test asserts `branches[0].status ==
  "conflicted"`, `conflicts` non-empty, top-level `error` empty, exit 0; a clean
  rebase asserts `status:"clean"`, `pushed:true`; up-to-date asserts `status:"noop"`.
- `sync --continue`: after staging resolved files, asserts `status:"clean"`.
- Lock-timeout: a held lock makes a second invocation emit
  `error_code:"lock_timeout"` (json) / exit 75 (non-json).
- J8: external-merge propagation test (§G).
- Existing non-worktree `sync`/`new`/`branch`/`pr` tests must stay green
  (idempotency and additive fields must not alter their behavior beyond the
  documented re-run change).

## Field-name contract (for flow's parser)

Stable JSON keys flow depends on:
- `new`: `stack, base, branch, worktree_path, pushed, created` (+ `error_code`).
- `branch`: `branch, parent, worktree_path, created` (+ `error_code`).
- `pr`: `number, pr, url, title, draft, created` (+ `error_code`).
- `status` node: `branch, status, sync_state, worktree_path, pr` (+ existing extras).
- `sync`: `branches[].{branch, status, conflicts, pushed}` (+ top-level `error`,
  `error_code`).
- `switch --path-only`: bare absolute path on stdout.
