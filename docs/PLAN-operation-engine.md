# Operation Engine: Phased Pipeline Model

**Date:** 2026-04-05
**Status:** Design — under review (v2: generalized to all commands)

## Problem

sdf has 15+ commands, each with independently invented patterns for:
- Progress tracking and recovery (or none at all)
- Push timing (immediate, deferred, or absent)
- Error handling (abort, retry, ignore, or crash)
- Transparency (some show plans, most don't)
- Data flow between steps (ad-hoc variables, or deeply nested function calls)

The three mutation commands (sync, restack, move) are the worst offenders, but even read-only commands like `status` make 8+ external calls with implicit data dependencies between them. There is no shared vocabulary for "what sdf is doing right now" or "what it's about to do."

## Goals

1. **Every sdf command** is expressed as an ordered sequence of steps — mutation or not
2. Steps declare their outputs; downstream steps reference those outputs by name
3. The executor validates the step graph before running anything
4. Mutation commands get `--continue`, `--abort`, `--quit` for free
5. Full transparency: any command can show its step sequence before execution
6. Crash-safe: mutation commands persist step status to disk
7. One validation layer catches logic errors (missing dependencies, impossible sequences)

## Non-goals

- This is not a generic task runner or plugin system
- We are not building an undo system for pushed changes — push is the point of no return
- Read-only commands don't need progress persistence — but they do use the same step model for transparency and validation

---

## Design

### Core concept: Steps with named outputs

Every sdf command is a sequence of **steps**. Each step:
- Has a **kind** (what it does: `git-rebase`, `git-push`, `gh-pr-edit`, `git-rev-parse`, etc.)
- Has **inputs** (concrete values or references to upstream step outputs)
- Produces **outputs** (named values available to downstream steps)
- Has a **status** (`pending`, `in-progress`, `done`, `conflict`, `skipped`, `failed`)

The key design decision: **steps reference upstream outputs by name, not by value.** This solves the dependency problem without requiring steps to know about each other.

### Step definition

```go
type Step struct {
    ID      string            `json:"id"`                // unique within the operation, e.g. "rebase-auth"
    Kind    string            `json:"kind"`              // "git-rebase", "git-push", "gh-pr-edit", etc.
    Inputs  map[string]Value  `json:"inputs"`            // parameters for this step
    Outputs map[string]string `json:"outputs,omitempty"` // filled after execution: name → value
    Status  string            `json:"status"`            // pending, in-progress, done, conflict, skipped, failed
    Error   string            `json:"error,omitempty"`   // if failed or conflict
}

type Value struct {
    Literal string `json:"literal,omitempty"` // a concrete value: "abc1234"
    Ref     string `json:"ref,omitempty"`     // a reference: "rebase-auth.new_sha"
}
```

A `Value` is either a literal string or a reference to `<step-id>.<output-name>`. References are resolved at execution time, after the referenced step completes.

### Example: sync pipeline

```
sdf sync on a 3-branch stack where auth needs rebasing onto main:

Step 1: { id: "fetch",        kind: "git-fetch-all",   inputs: {},                                        outputs: {} }
Step 2: { id: "ff-main",      kind: "git-fast-forward", inputs: { branch: "main" },                       outputs: { tip: "abc1234" } }
Step 3: { id: "rebase-auth",  kind: "git-rebase",      inputs: { branch: "feat/auth", onto: ref("ff-main.tip"), old_base: "aaa111" },
                                                                                                            outputs: { new_sha: "ccc333" } }
Step 4: { id: "rebase-api",   kind: "git-rebase",      inputs: { branch: "feat/api",  onto: ref("rebase-auth.new_sha"), old_base: "bbb222" },
                                                                                                            outputs: { new_sha: "ddd444" } }
Step 5: { id: "push-auth",    kind: "git-push",        inputs: { branch: "feat/auth" },                   outputs: {} }
Step 6: { id: "push-api",     kind: "git-push",        inputs: { branch: "feat/api" },                    outputs: {} }
Step 7: { id: "pr-base-42",   kind: "gh-pr-edit-base", inputs: { pr: 42, base: "main" },                  outputs: {} }
Step 8: { id: "pr-base-43",   kind: "gh-pr-edit-base", inputs: { pr: 43, base: "feat/auth" },             outputs: {} }
Step 9: { id: "update-nav",   kind: "update-stack-nav", inputs: { stack: "my-feature" },                  outputs: {} }
```

Step 4's `onto` input references step 3's `new_sha` output. At plan time, the executor sees this reference and knows:
- Step 4 cannot run before step 3
- If step 3 fails or conflicts, step 4 cannot proceed
- In verbose mode, step 4 displays: `git rebase --onto <new-tip-of-feat/auth> bbb222 feat/api`

### Example: status (read-only)

```
sdf status on the same stack:

Step 1: { id: "fetch",       kind: "git-fetch-all",    inputs: {},                               outputs: {} }
Step 2: { id: "ff-main",     kind: "git-fast-forward",  inputs: { branch: "main" },              outputs: { tip: "abc1234" } }
Step 3: { id: "tip-auth",    kind: "git-rev-parse",     inputs: { ref: "feat/auth" },            outputs: { sha: "..." } }
Step 4: { id: "tip-api",     kind: "git-rev-parse",     inputs: { ref: "feat/api" },             outputs: { sha: "..." } }
Step 5: { id: "pr-info",     kind: "gh-pr-list",        inputs: { branches: ["feat/auth","feat/api"] }, outputs: { prs: [...] } }
Step 6: { id: "display",     kind: "render-status",     inputs: { stack: "my-feature", prs: ref("pr-info.prs") }, outputs: {} }
```

Status doesn't need progress persistence or recovery. But it uses the same step model, so `sdf status --verbose` can show:
```
  1. git fetch --all
  2. git fetch origin main:main (fast-forward)
  3. git rev-parse feat/auth
  4. git rev-parse feat/api
  5. gh pr list --head feat/auth,feat/api --json number,state,...
  6. (render)
```

### Example: move

```
sdf move abc1234 (moving a commit from feat/api to feat/auth):

Step 1:  { id: "snapshot-auth",    kind: "git-rev-parse",  inputs: { ref: "feat/auth" },    outputs: { sha: "..." } }
Step 2:  { id: "snapshot-api",     kind: "git-rev-parse",  inputs: { ref: "feat/api" },     outputs: { sha: "..." } }
Step 3:  { id: "snapshot-ui",      kind: "git-rev-parse",  inputs: { ref: "feat/ui" },      outputs: { sha: "..." } }
Step 4:  { id: "cherry-pick",      kind: "git-cherry-pick", inputs: { onto: "feat/auth", commits: ["abc1234"] },
                                                                                              outputs: { new_tip: "..." } }
Step 5:  { id: "rebase-api",       kind: "git-rebase",     inputs: { branch: "feat/api", onto: ref("cherry-pick.new_tip"), old_base: "abc1234" },
                                                                                              outputs: { new_sha: "..." } }
Step 6:  { id: "rebase-ui",        kind: "git-rebase",     inputs: { branch: "feat/ui", onto: ref("rebase-api.new_sha"), old_base: "..." },
                                                                                              outputs: { new_sha: "..." } }
Step 7:  { id: "push-auth",        kind: "git-push",       inputs: { branch: "feat/auth" }, outputs: {} }
Step 8:  { id: "push-api",         kind: "git-push",       inputs: { branch: "feat/api" },  outputs: {} }
Step 9:  { id: "push-ui",          kind: "git-push",       inputs: { branch: "feat/ui" },   outputs: {} }
```

### All commands as step sequences

| Command | Steps | Persisted | Recovery |
|---------|-------|-----------|----------|
| **status** | fetch → ff-base → rev-parse branches → gh pr list → render | No | N/A |
| **sync** | fetch → ff-base → reconcile-prs → rebase(s) → push(s) → update-pr-bases → update-nav | Yes | --continue, --abort, --quit |
| **restack** | fetch → ff-base → reorder-nodes → rebase(s) → push(s) → update-pr-bases → update-nav | Yes | --continue, --abort, --quit |
| **move** | snapshot → cherry-pick → rebase(s) → push(s) | Yes | --continue, --abort, --quit |
| **merge** | retarget-next-pr → gh-pr-merge → ff-base → sync-pipeline | Yes | --continue, --abort, --quit |
| **pr** | push → gh-pr-create → update-nav | Yes (lightweight) | N/A (atomic enough) |
| **branch** | git-create-branch → push → insert-node → update-downstream-pr-base | No | N/A |
| **new** | detect-base → create-sdf-dir → create-branch → push | No | N/A |
| **fetch** | gh-pr-list → discover-chains → select-stack → checkout-branches → register | No | N/A |
| **split** | diff-files → claude-analyze → create-branches → push(s) → create-prs → update-nav | Yes | --continue (resume from failed branch) |
| **switch** | git-checkout | No | N/A |
| **ls** | load-stacks → render | No | N/A |
| **prune** | check-branches → remove-nodes → delete-stacks → cleanup-legacy | No | N/A |
| **doctor** | check-git → check-gh → check-claude → render | No | N/A |
| **config** | load-config → set/show | No | N/A |

### Step kinds: the vocabulary

Each step kind maps to a concrete operation. The executor knows how to run each kind and what outputs it produces:

**Git operations:**
| Kind | Inputs | Outputs | Reversible |
|------|--------|---------|------------|
| `git-fetch-all` | — | — | N/A (read) |
| `git-fast-forward` | branch | tip (new SHA) | N/A (read) |
| `git-rev-parse` | ref | sha | N/A (read) |
| `git-is-ancestor` | ancestor, descendant | result (bool) | N/A (read) |
| `git-create-branch` | name, from | — | reversible (delete branch) |
| `git-checkout` | branch | — | reversible (checkout back) |
| `git-rebase` | branch, onto, old_base | new_sha | reversible (reset to snapshot) |
| `git-cherry-pick` | onto, commits[] | new_tip | reversible (reset to snapshot) |
| `git-push` | branch | — | **irreversible** (commit point) |
| `git-push-new` | branch | — | **irreversible** |
| `git-reset-hard` | branch, sha | — | destructive (used in abort) |

**GitHub operations:**
| Kind | Inputs | Outputs | Reversible |
|------|--------|---------|------------|
| `gh-pr-list` | branches[] | prs[] (JSON) | N/A (read) |
| `gh-pr-create` | branch, base, title, body | pr_number, url | technically no (but deletable) |
| `gh-pr-edit-base` | pr, base | — | reversible (edit back) |
| `gh-pr-merge` | pr, method | — | **irreversible** |
| `gh-pr-view` | pr | body, state, ... | N/A (read) |

**sdf-internal operations:**
| Kind | Inputs | Outputs | Reversible |
|------|--------|---------|------------|
| `reconcile-prs` | stack | changes[] | yes (undo status changes) |
| `reorder-nodes` | stack, source, after | new_order | yes (restore original) |
| `update-stack-nav` | stack | — | yes (idempotent) |
| `render-status` | stack, data | — | N/A (display) |
| `claude-analyze` | branch, base, files | plan | N/A (read) |

### Dependency resolution and validation

The executor resolves references before running each step:

```go
func (e *Executor) resolveInputs(step *Step) (map[string]string, error) {
    resolved := make(map[string]string)
    for name, val := range step.Inputs {
        if val.Literal != "" {
            resolved[name] = val.Literal
        } else if val.Ref != "" {
            // Parse "step-id.output-name"
            parts := strings.SplitN(val.Ref, ".", 2)
            upstream := e.findStep(parts[0])
            if upstream == nil {
                return nil, fmt.Errorf("step %s references unknown step %s", step.ID, parts[0])
            }
            if upstream.Status != "done" {
                return nil, fmt.Errorf("step %s depends on %s which has status %s", step.ID, parts[0], upstream.Status)
            }
            output, ok := upstream.Outputs[parts[1]]
            if !ok {
                return nil, fmt.Errorf("step %s references output %s.%s which was not produced", step.ID, parts[0], parts[1])
            }
            resolved[name] = output
        }
    }
    return resolved, nil
}
```

**Pre-execution validation** catches logic errors before any mutation:

1. **Reference validity**: Every `ref(X.Y)` must point to a step X that exists and declares output Y
2. **Ordering**: A step with a ref dependency must come after the referenced step
3. **No cycles**: the dependency graph is a DAG (enforced by ordering — steps are a flat list, refs can only point backward)
4. **Phase coherence**: irreversible steps (push) must come after all reversible steps (rebase). No interleaving.
5. **Snapshot completeness**: every branch touched by a reversible step must have a snapshot entry

This validation runs once, before the first step executes. It is the "logic mistake catcher" — if a command builds a plan where step 5 references step 7's output, validation fails with a clear error message before anything runs.

### The executor

```go
type Executor struct {
    Steps    []*Step
    Snapshot map[string]string  // branch → pre-mutation SHA
    Phase    string             // "planning", "executing", "pushing", "post-push", "done"
    Root     string             // repo root for persistence
    Bus      *render.Bus        // output
    Persist  bool               // whether to save progress to disk
}
```

The executor loop:

```
func (e *Executor) Run() error {
    // 1. Validate the full step graph
    if err := e.validate(); err != nil {
        return fmt.Errorf("invalid operation plan: %w", err)
    }

    // 2. Execute steps in order
    for _, step := range e.Steps {
        if step.Status == "done" || step.Status == "skipped" {
            continue  // already completed (--continue case)
        }

        // Resolve refs to concrete values
        inputs, err := e.resolveInputs(step)
        if err != nil {
            return err  // upstream dependency not met
        }

        // Mark in-progress (persist for crash safety)
        step.Status = "in-progress"
        e.save()

        // Dispatch to handler
        outputs, err := e.dispatch(step.Kind, inputs)
        if err != nil {
            return e.handleError(step, err)
        }

        // Record outputs, mark done
        step.Outputs = outputs
        step.Status = "done"
        e.save()
    }

    return nil
}
```

The `dispatch` function maps step kinds to concrete implementations. It is a switch statement, not an interface:

```go
func (e *Executor) dispatch(kind string, inputs map[string]string) (map[string]string, error) {
    switch kind {
    case "git-rebase":
        return e.execGitRebase(inputs)
    case "git-push":
        return e.execGitPush(inputs)
    case "git-rev-parse":
        return e.execGitRevParse(inputs)
    // ... etc
    }
}
```

### Phase boundaries

Not all steps are equal. The executor recognizes **phase boundaries** — points in the step sequence where behavior changes:

```go
type Step struct {
    // ... existing fields
    Phase string `json:"phase"` // "pre-mutation", "mutation", "commit", "post-commit"
}
```

**Phase rules:**
- `pre-mutation` steps: read-only. No snapshot needed. Failures → abort the whole operation.
- `mutation` steps: reversible writes (rebase, cherry-pick, reorder). Snapshot required. Failures → pause for user. `--abort` restores snapshots.
- `commit` steps: irreversible writes (push). No abort after first commit step starts. Failures → warn, continue, report.
- `post-commit` steps: best-effort (PR updates, nav). Failures → warn only.

Phase transitions are validated at plan time. The executor enforces: no `mutation` step after a `commit` step. No `commit` step without a preceding `mutation` step (nothing to push). Pre-mutation steps must precede all mutations.

### Recovery: --continue

```
1. Load persisted operation from local.json
2. Find first non-"done" step
3. If step.Status == "in-progress" or "conflict":
   a. Inspect git state to determine reality
   b. If rebase completed: mark "done", record outputs, continue
   c. If rebase still in progress: run git rebase --continue, then proceed
   d. If rebase was aborted: mark "pending", re-execute
4. Resume the executor loop from this step
```

### Recovery: --abort

```
1. Load persisted operation
2. If any "commit"-phase step has Status == "done":
   → Error: "Pushes already completed — cannot abort"
3. Abort any in-progress git rebase
4. For each "mutation"-phase step with Status == "done" or "in-progress":
   → git checkout <branch>; git reset --hard <snapshot[branch]>
5. Restore command_data (original node order, etc.)
6. Restore original branch, clear progress
```

### Recovery: --quit

```
1. Abort any in-progress git rebase
2. Clear progress from local.json
3. Leave everything else as-is
```

### Transparency

Every command gets `--verbose` and `--dry-run` for free from the executor.

**Default output** (what runs today, roughly):
```
Syncing stack my-feature...
  ✦ rebase feat/auth onto main
  ✦ rebase feat/api onto feat/auth
  ✦ push 2 branches
  ✦ update 2 PR bases
```

**`--verbose` output** (full commands):
```
Syncing stack my-feature...

  Pre-mutation:
    1. git fetch --all
    2. git fetch origin main:main
    3. git rev-parse main → <sha>

  Mutation:
    4. git rebase --onto <main-tip> aaa111 feat/auth
    5. git rebase --onto <new-tip-of-auth> bbb222 feat/api

  Push:
    6. git push --force-with-lease origin feat/auth
    7. git push --force-with-lease origin feat/api

  Post-push:
    8. gh pr edit 42 --base main
    9. gh pr edit 43 --base feat/auth
   10. (update stack navigation in PR descriptions)

Proceed? [Y/n]
```

**`--dry-run`** shows the verbose plan and exits without executing.

**`--json`** includes the full step array with kinds, inputs (refs shown as `<step-id.output>`), and phases.

---

## The dependency problem: detailed analysis

The central challenge is that downstream steps need outputs from upstream steps, but those outputs don't exist at plan time.

### Three categories of dependencies

**1. SHA dependencies (most common)**

Step N rebases a branch → produces a new SHA. Step N+1 rebases a downstream branch *onto* that new SHA.

```
Step 3: rebase feat/auth onto main      → outputs: { new_sha: "ccc333" }
Step 4: rebase feat/api onto feat/auth   → inputs:  { onto: ref("rebase-auth.new_sha") }
```

At plan time, we know the *shape* (step 4 depends on step 3's output) but not the *value*. The verbose plan shows `<new-tip-of-feat/auth>` as a placeholder.

**Resolution:** The executor resolves `ref("rebase-auth.new_sha")` at execution time by reading step 3's outputs after it completes. If step 3 hasn't run, the executor blocks step 4.

**2. Branch existence dependencies**

Step N creates a branch. Step N+1 pushes it.

```
Step 1: create branch feat/new    → outputs: {}
Step 2: push feat/new             → inputs: { branch: "feat/new" }
```

No output reference needed — the branch name is a literal known at plan time. The dependency is *implicit*: step 2 will fail if step 1 hasn't run. The validator catches this by knowing that `git-push` requires the branch to exist, and if the branch is being created in the same plan, the creation step must come first.

**3. Conditional/dynamic dependencies**

Step N queries GitHub. Based on the result, different steps should run.

```
Step 1: gh pr list → outputs: { prs: [...] }
Step 2: IF pr 42 is merged, skip rebase for feat/auth
```

This is where the pipeline model gets strained. The plan can't be fully determined until step 1 completes.

**Resolution:** Commands resolve conditional logic *before* building the step list. Sync's `reconcilePRStates()` and `computeSyncPlan()` run during plan building, not as steps. By the time the executor sees the step list, all conditions have been evaluated. The step list is flat and unconditional.

The exception is status, where PR data fetching is a step. For read-only commands, this is fine — there's no recovery concern, and the executor just runs steps sequentially.

### What the executor resolves vs. what commands resolve

| Concern | Resolved by | When |
|---------|------------|------|
| Which branches need rebasing | Command (sync/restack/move) | Plan building (before executor) |
| PR states (merged/open/closed) | Command (sync) | Plan building |
| SHA of branch tips | Executor (via git-rev-parse steps) | Execution |
| New SHA after rebase | Executor (step output) | Execution |
| Whether a step should run at all | Command | Plan building |
| What value to pass to a step | Executor (ref resolution) | Execution |

This split is important: **commands own the "what", the executor owns the "how and when."** Commands never need to think about crash recovery, status persistence, or dependency resolution. They build a flat step list with refs, hand it to the executor, and get back a result.

---

## Implementation plan

### Phase 1: Step and executor model (`internal/ops/`)

**New files:**
- `internal/ops/step.go` — `Step`, `Value`, `Ref()` helper, step kind constants
- `internal/ops/operation.go` — `Operation` struct (steps, snapshot, phase, command, command_data), `Load()`, `Save()`, `Clear()`
- `internal/ops/executor.go` — `Executor` struct, `Run()` loop, `validate()`, `resolveInputs()`, `dispatch()`, `handleError()`
- `internal/ops/handlers.go` — implementations for each step kind (`execGitRebase`, `execGitPush`, `execGitRevParse`, etc.)
- `internal/ops/inspect.go` — `InspectRebaseState()` for crash recovery
- `internal/ops/recover.go` — `Continue()`, `Abort()`, `Quit()` implementations
- `internal/ops/display.go` — `FormatPlan()` for default/verbose/dry-run/json rendering

**Tests:**
- `internal/ops/executor_test.go` — validate dependency resolution, phase ordering, error handling with mock steps
- `internal/ops/recover_test.go` — crash recovery scenarios with manipulated state files

### Phase 2: Migrate restack (closest to target)

1. `cmd/restack.go` builds steps: `[rev-parse(s) → reorder-nodes → rebase(s) → push(s) → pr-edit-base(s) → update-nav]`
2. Snapshot built from rev-parse step outputs
3. `runRestackLogic` becomes: build steps → `executor.Run()`
4. `runRestackContinue` becomes: `ops.Continue()`
5. `runRestackAbort` becomes: `ops.Abort()`
6. Add `--quit`, `--verbose`, `--dry-run`
7. Remove `RestackProgress` usage, store original nodes in `command_data`

### Phase 3: Migrate sync

1. Pre-executor logic stays: fetch, ff-base, reconcile PR states, compute which branches need rebasing
2. Build steps from computed plan: `[rebase(s) → push(s) → pr-edit-base(s) → update-nav → create-missing-prs]`
3. **Key change:** remove per-branch push from rebase loop, add push steps after all rebase steps
4. `runSyncContinue` uses `ops.Continue()`
5. Add `--abort`, `--quit`, `--verbose`, `--dry-run`
6. Remove `SyncProgress` type

### Phase 4: Migrate move

1. Build steps: `[snapshot(s) → cherry-pick → rebase(s) → push(s)]`
2. Cherry-pick step uses `git-cherry-pick` kind; outputs `new_tip`
3. Rebase steps reference `cherry-pick.new_tip` for the first rebase
4. Add `--continue`, `--abort`, `--quit`

### Phase 5: Migrate remaining mutation commands

**merge:**
1. Steps: `[retarget-next-pr → gh-pr-merge → ff-base → sync-steps]`
2. The sync steps are built by the same logic as sync, embedded in the merge plan
3. `gh-pr-merge` is a commit-phase step (irreversible)

**split:**
1. Steps: `[analyze → create-branches → push(s) → create-prs → update-nav]`
2. Claude analysis is a pre-mutation step
3. Branch creation + push are the mutation/commit phases

**pr:**
1. Steps: `[push → gh-pr-create → update-nav]`
2. Simple enough that persistence may be overkill, but the model still applies for `--dry-run` and `--verbose`

**branch:**
1. Steps: `[create-branch → push-new → insert-node → update-downstream-pr]`
2. No persistence needed

### Phase 6: Read-only commands

Express as steps for transparency, but no persistence:

**status:** `[fetch → ff-base → rev-parse(s) → gh-pr-list → render]`
**ls:** `[load-stacks → render]`
**doctor:** `[check-git → check-gh → check-claude → render]`
**fetch:** `[gh-pr-list → discover → select → checkout-branches → register]`

These benefit from `--verbose` (see exactly what calls are made) and `--dry-run` (see the plan without executing).

### Phase 7: Plan display and transparency

1. `--verbose` / `-v` on all commands → shows full step sequence with git/gh commands
2. `--dry-run` on all commands → shows plan, exits
3. Global config option `verbose = true` for users who always want transparency
4. `--json` includes step array in output

### Phase 8: Cleanup and migration

1. Remove `SyncProgress`, `RestackProgress` from `stack.go`
2. Migration logic: detect old-format progress, clear with warning
3. Update all tests
4. Document the new `--verbose`, `--dry-run`, `--continue`, `--abort`, `--quit` flags

---

## Benefits

1. **One mental model** — Every command is steps. Users learn it once. Developers add commands by defining steps, not by reinventing recovery.

2. **Logic error catching** — The validator runs before any mutation. A command that accidentally puts a push before a rebase, or references a nonexistent step output, fails at validation with a clear message. This is the "one layer in between" that catches mistakes.

3. **Transparency for free** — `--verbose` and `--dry-run` work on every command, even read-only ones. Users can always see what sdf will do.

4. **Crash safety for free** — Any command that sets `Persist: true` gets status tracking, `--continue`, `--abort`, `--quit` from the executor. No per-command recovery logic.

5. **Deferred push everywhere** — The phase model enforces that all mutation steps complete before any commit step. No more partially-pushed stacks.

6. **Testable in isolation** — Step handlers are unit-testable. The executor is testable with mock handlers. Commands are testable by inspecting the step list they produce (without executing).

7. **Future commands are trivial** — Define steps, hand to executor. `sdf merge` is just: build the right step list, call `executor.Run()`.

## Risks

1. **Over-engineering for simple commands** — `sdf switch` is literally one git checkout. Wrapping it in a Step with an Executor is ceremony that adds no value. Same for `sdf ls`, `sdf config show`.

   *Mitigation:* Simple commands can use the step model for `--verbose`/`--dry-run` without persistence. The overhead is: build a `[]Step`, call `executor.Run()`. If that's still too much, the simplest commands can skip the executor entirely — the model is opt-in, not mandatory. The value is in the mutation commands.

2. **The abstraction may not fit sync's complexity** — Sync has conditional logic mid-execution: if a rebase discovers the branch is already an ancestor, it skips the rebase and just updates BaseTip. Sync also has `--full` mode that changes which branches need rebasing. This logic currently lives *inside* the rebase loop.

   *Mitigation:* Same as before — sync resolves all this during plan building. The `computeSyncPlan()` function already does this; it just needs to produce Steps instead of the current `syncAction` structs. The "already an ancestor" case becomes a step with kind `update-tip` instead of `git-rebase`.

3. **Step output types are strings** — Everything is `map[string]string`. PR list results, boolean checks, JSON payloads — all flattened to strings. This works for SHAs and branch names but gets awkward for complex data like PR lists.

   *Mitigation:* Complex data (PR lists, status results) passes through `command_data` or is resolved during plan building, not as step outputs. Step outputs are for simple values that downstream steps need: SHAs, branch names, PR numbers. If a step needs complex data, it queries directly (the step handler can call `ghpkg.PRList()` internally).

4. **Dispatch is a big switch** — Every step kind needs a case in the dispatch function. As the vocabulary grows, this becomes a maintenance burden.

   *Mitigation:* The vocabulary is bounded by what git and gh can do. There are roughly 15-20 step kinds total (see tables above). A 20-case switch statement is readable and greppable. If it grows beyond that, the step kinds can be grouped into files (`handlers_git.go`, `handlers_gh.go`, `handlers_sdf.go`).

5. **Ref resolution adds indirection** — Debugging a failed step requires understanding where its input came from. "Step 5 failed with onto=abc1234" is less helpful than "rebase of feat/api onto feat/auth (abc1234) failed."

   *Mitigation:* Error messages include the resolved values AND the ref path: "step rebase-api failed: git rebase --onto abc1234 (from rebase-auth.new_sha) ..." The verbose plan shows the chain explicitly.

6. **Two-pass execution model** — Commands first build steps (which may require git/gh calls to compute the plan), then the executor runs steps (which also make git/gh calls). Some calls happen twice: e.g., `git rev-parse` during plan building to check if rebase is needed, then again as a step.

   *Mitigation:* Accept the duplication for now. Rev-parse is < 1ms. The alternative (making plan building itself a step) adds complexity without meaningful benefit. If this becomes a performance issue, plan-building results can be injected as literal values in step inputs.

7. **Migration path** — All three mutation commands need to be migrated. During migration, the codebase has both old and new patterns. If migration stalls after restack but before sync, users have inconsistent behavior across commands.

   *Mitigation:* Migrate one command at a time, ship each migration as its own PR. Each migration is independently valuable (restack gets --quit and --dry-run, sync gets --abort and deferred push, etc.). Incomplete migration is still better than the status quo.

---

## Open questions

1. **Should `--continue` / `--abort` / `--quit` be subcommands of `sdf` rather than flags on each command?** Since only one operation can be in progress at a time, `sdf --continue` could dispatch automatically based on the progress file's `command` field. This is cleaner UX but changes the CLI surface.

2. **Should move start pushing by default?** Today it's local-only. Adding pushes changes behavior. Option: `sdf move` pushes, `sdf move --local` doesn't.

3. **How granular should step kinds be?** Is `git-rebase` one kind, or should `git-rebase --onto` be distinct from a plain `git rebase`? Current answer: one kind (`git-rebase`) with inputs that determine behavior. The kind is the verb, inputs are the arguments.

4. **Should read-only commands use the executor at all?** They get `--verbose` for free, but they don't need persistence, recovery, or validation. A simpler wrapper might suffice: just format the planned commands and run them directly.

5. **What about interactive steps?** `sdf sync` prompts for conflict resolution (Claude/manual/skip/abort). `sdf split` has an interactive Claude refinement session. These don't fit neatly into the step model. Current answer: the executor pauses execution and returns control to the command for interactive steps. The command handles the interaction and tells the executor to continue, skip, or abort.

6. **How does `sdf merge` compose with sync?** Merge currently calls `runSyncFull()` directly. In the new model, merge would build a combined step list that includes sync's steps. This means sync's plan-building logic needs to be callable from merge without running the executor. Current answer: extract sync's plan building into a function that returns `[]Step`, callable by both sync and merge.
