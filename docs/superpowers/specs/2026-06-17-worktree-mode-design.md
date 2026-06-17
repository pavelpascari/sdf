# sdf Worktree Mode — Design

**Date:** 2026-06-17
**Status:** Approved (pending spec review)

## Goal

Add an opt-in, per-stack mode in which every branch of a stack is also a git
worktree (its own working directory). This lets multiple agents work on
different branches of the same stack simultaneously, in isolated directories,
without stepping on each other's working trees.

sdf provides the **substrate**: it creates/destroys worktrees, scopes sync to a
single worktree, and tracks per-branch readiness. It does **not** spawn or manage
agents — who runs in each worktree (Claude, a human, an orchestrator) is the
caller's concern.

## Mental model

```
main repo (./sdf)              ← branch "main"; OWNS .sdf/ (the only copy)
  .sdf/stacks/feature.json     ← single source of truth: stack state + readiness
  .sdf/local.json              ← per-checkout, gitignored

../sdf.worktrees/feature-a/    ← branch feature-a, agent A works here
../sdf.worktrees/feature-b/    ← branch feature-b, agent B works here
../sdf.worktrees/feature-c/    ← branch feature-c, agent C works here
```

`.sdf/` is gitignored (see `.gitignore`: `.sdf/`), so a new worktree does **not**
get its own copy — there is exactly one `.sdf/`, in the main repo. This gives a
single source of truth and avoids file contention. Worktrees locate it via
`git rev-parse --git-common-dir` (the `.git` dir is shared across all worktrees,
so the main repo root is always discoverable from any worktree).

## Coordination model: worktree-driven (pull)

Sync in worktree mode is **not** a monolithic operation that rewrites branches
under busy agents. It is an incremental, coordinated protocol driven from each
worktree:

1. An agent, working in its own worktree, reaches a clean stopping point and
   commits its work.
2. It runs `sdf sync`, which rebases **only its branch** onto its already-updated
   parent, pushes, and updates the recorded base tip.
3. Rewriting that branch makes the downstream child's `BaseTip` stale — the
   child's turn. The child's agent picks it up on its own schedule.

Readiness falls out of the existing `BaseTip` machinery for free: a branch's turn
has arrived when its parent's current tip no longer equals the node's recorded
`BaseTip`. sdf never touches a downstream branch itself; propagation flows down as
each agent takes its turn.

## Data model

`internal/stack/stack.go`:

```go
type Stack struct {
    StackID  string `json:"stack_id"`
    Base     string `json:"base"`
    Nodes    []Node `json:"nodes"`
    Worktree bool   `json:"worktree,omitempty"` // worktree mode for this stack
}

type Node struct {
    Branch       string `json:"branch"`
    PR           int    `json:"pr,omitempty"`
    Status       string `json:"status"`
    BaseTip      string `json:"base_tip,omitempty"`
    NavHash      string `json:"nav_hash,omitempty"`
    WorktreePath string `json:"worktree_path,omitempty"` // absolute path, worktree mode only
}
```

- `Stack.Worktree` — the per-stack opt-in flag.
- `Node.WorktreePath` — the recorded location of each branch's checkout, used by
  status, switch, merge, prune, and doctor.

## Configuration

New section in `.sdf/config.json` (repo) and global config, merged two-tier as
today:

```json
{ "worktree": { "base_path": "../{repo}.worktrees" } }
```

- `base_path`: template. `{repo}` expands to the repo directory name. Default
  `../{repo}.worktrees`.
- Each worktree is created at `<base_path>/<branch>`, with `/` in branch names
  (from prefixes like `stack/feature-a`) sanitized to `-` so the layout stays
  flat (no deep nesting).
- The **enabled** flag is *not* config — it lives on the stack (`Stack.Worktree`).

## State discovery & concurrency

### Worktree-aware root discovery

`stack.FindRoot()` gains a fallback: if walking up from the cwd finds no `.sdf/`,
it runs `git rev-parse --git-common-dir`, resolves to the main worktree root, and
checks for `.sdf/` there. This makes every sdf command work from inside any
worktree, all pointing at the single `.sdf/`.

### Locking

Concurrent `sdf` invocations across worktrees may read/write the same stack JSON.
A simple advisory file lock guards every read-modify-write of stack state:

- Lock file `.sdf/<stack>.lock`, created with `O_CREATE|O_EXCL`.
- Acquire with timeout (~10s, polled).
- Stores holder PID + timestamp; a stale lock (dead PID or too old) is stolen.

Each sync step is short (one rebase + push + JSON update), so contention windows
are tiny.

## Command behavior

### `sdf init --worktrees`

Creates the stack with `Worktree: true`. The base branch stays in the main repo.

**Enabling on an existing stack** (in v1): `sdf worktree enable [--stack <name>]`
sets `Worktree: true` on the resolved stack and materializes worktrees for all of
its current **open** nodes — reusing the same worktree-add path as `sdf branch`.
Merged/closed nodes are skipped. This is the one new verb introduced beyond
flags on existing commands; it is idempotent (re-running skips nodes that already
have a live worktree).

### `sdf branch <name>` (worktree stack)

Instead of checking out the parent in the main repo:

1. Resolve the parent using the existing insert-at-position logic.
2. `git worktree add <wt-path> -b <name> <parent>`.
3. Record `WorktreePath` on the node; push the tracking branch; update the
   downstream PR base if inserting mid-stack (unchanged behavior).
4. The main repo checkout is **never touched**.

Output prints the worktree path and a `cd` hint. `--json` includes
`worktree_path`.

### `sdf sync`

Behavior branches on where it is run:

**From a worktree** (cwd's branch is a stack node) — the worktree-driven step:

1. Acquire the stack lock; resolve the effective parent (skipping merged nodes).
2. If parent tip == node `BaseTip` → "up to date", done.
3. If the worktree is dirty → error: *commit your work first*. The pull model
   assumes the agent reaches a clean stopping point before integrating.
4. `git -C <wt> rebase --onto <parent> <BaseTip> <branch>`. On conflict: pause,
   save a scoped `SyncProgress`, instruct the agent to resolve and run
   `sdf sync --continue` (reuses the existing conflict engine, run in-worktree).
5. On success: `git -C <wt> push --force-with-lease`; set `BaseTip = parent tip`;
   save; update the PR base via `gh pr edit --base` only if the parent *name*
   changed (existing rule).
6. Print "downstream `<child>` now needs to sync." sdf never touches downstream.

**From the main repo** (on the base branch, not a stack node) — a **readiness
dashboard**: prints each branch's state (up-to-date / stale-needs-sync / dirty /
conflicted), and refreshes the stack nav in PR descriptions. It does **not**
rebase. (Orchestrator-driven *push* stepping is out of scope for v1.)

**Non-worktree stacks:** existing monolithic sync, unchanged.

### `sdf sync --continue` (worktree stack)

Resumes a paused in-worktree rebase: detects whether the rebase is in progress,
completed manually, or aborted (existing logic), then finishes the step (push,
update `BaseTip`, save).

### `sdf switch <branch>` (worktree stack)

A CLI cannot change the parent shell's directory, so:

- Default: prints the worktree path and the `cd` command.
- `--path-only`: emits just the absolute path, for `cd "$(sdf switch x --path-only)"`.
- `--json`: includes the worktree path.

### `sdf status` (worktree stack)

Per node: worktree path, clean/dirty, readiness (stale base = needs sync), and a
current-worktree marker.

### `sdf ls`

Tags worktree-mode stacks so they're distinguishable in the list.

### `sdf merge` (worktree stack)

Merges the head PR and marks the node merged, then `git worktree remove` for that
branch's worktree (warns if dirty; `--force` to override). It **skips** the
auto-cascade-sync (pull model) and instead prints which downstream worktree now
needs to sync.

### `sdf prune` (worktree stack)

When removing merged stacks/branches, also `git worktree remove` for each
(clean check; `--force` to remove dirty worktrees).

### `sdf doctor`

New checks:

- Recorded `WorktreePath` missing or moved (orphaned worktree).
- A worktree present on disk but not in any stack.
- A branch/worktree/node mismatch.

Each with a repair hint.

## git wrapper changes

New functions in `internal/git/git.go`:

- `WorktreeAdd(path, branch, from string) error`
- `WorktreeRemove(path string, force bool) error`
- `WorktreeList() ([]WorktreeInfo, error)`
- `GitCommonDir() (string, error)`
- `IsCleanAt(dir string) (bool, error)`
- `-C <dir>` variants of the operations per-worktree sync needs: rebase-onto,
  push, rev-parse, rebase --continue/--abort, conflicted files.

Implemented via a small internal `runAt(dir string, args ...string)` helper. The
production wrapper stays spy-free as today; matching `spyrecord`-tagged recording
entries are added for the new commands.

## Testing

- **Unit:** worktree path sanitization/templating; `FindRoot` worktree fallback
  via git-common-dir; lock acquire/timeout/steal.
- **Integration** (temp git repo, worktrees placed in a separate `t.TempDir()` to
  avoid nesting): create a worktree-mode stack → branches land at expected paths
  and are checked out; parent advances → in-worktree sync rebases correctly;
  dirty worktree is rejected; merge and prune remove worktrees; enabling mode on
  an existing stack materializes worktrees for open nodes.
- **E2E/spy:** new `git worktree` spy recordings under the `spyrecord` tag.

## Out of scope for v1 (YAGNI)

- sdf spawning or managing agents (process lifecycle, prompts, monitoring).
- Orchestrator-driven *push* sync — in v1 the main-repo `sdf sync` only reports
  readiness; it does not drive steps into worktrees.
- Auto-stashing dirty worktrees during sync.
