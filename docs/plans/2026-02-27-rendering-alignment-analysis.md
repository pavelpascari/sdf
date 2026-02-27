# Rendering System: Research Alignment Analysis

Date: 2026-02-27
Status: Draft
Inputs: `2026-02-27-advanced-rendering.md`, `2026-02-20-terminal-ui-design.md`, current codebase

## TL;DR

The advanced rendering research doc describes a **general-purpose JSONL event bus + renderer** — a reusable engine for any CLI that runs parallel tasks. The current sdf codebase has a **single ad-hoc implementation** of the same idea (`orderedPrinter` in `sync.go`). The gap is large but well-defined: the research describes a separate module, and the current code confirms there's exactly one consumer ready to adopt it.

**Recommendation: Build it as `internal/render` — a separate internal package**, not an external module. It maps cleanly onto sdf's existing architecture without requiring a public API contract. Extract it to a standalone module later if other projects want it.

---

## 1. Data Flow: What Carries What

**JSONL is a serialization format for persistence and external output. It is never used for in-process communication.** The in-process bus uses typed Go structs over channels.

```
                          In-process (Go structs over channels)
                         ┌──────────────────────────────────────────────┐
                         │                                              │
  goroutine 1 ─→ Reporter ─→ chan Event ─→ Router ─→ Renderer ─→ terminal (styled)
  goroutine 2 ─→ Reporter ─┘               │
  goroutine N ─→ Reporter ─┘               │
                                            │  side-effect (serialization only)
                                            ├─→ json.Encoder ─→ .sdf/logs/*.jsonl
                                            │
                                            └─→ (--json mode) JSON Renderer ─→ stdout
```

Three distinct boundaries:

| Boundary | Transport | Format |
|---|---|---|
| **goroutine → Router** | `chan render.Event` (buffered Go channel) | Go structs — `render.Event`, `render.TaskRef`, etc. |
| **Router → Renderer** | Direct function call or channel | Go structs — same `render.Event` |
| **Router → Log file** | `json.Encoder` writing to `io.Writer` | JSONL (one JSON object per line) |
| **JSON Renderer → stdout** | `json.Encoder` or `json.Marshal` | JSON (see `--json` mode below) |

The Event struct is the single source of truth. JSONL is just one way to serialize it — used only when crossing process boundaries (log files, piped stdout, agent consumption).

---

## 2. What the Research Describes vs. What Exists Today

| Concern | Research Doc (advanced-rendering) | Current Implementation |
|---|---|---|
| **Event schema** | Typed JSONL envelope (`task.start`, `task.progress`, `task.log`, `task.end`) with versioning, ordering, routing | None — `fmt.Printf` and `ui.SymOK` strings printed inline |
| **Concurrency model** | errgroup fan-out → per-task Reporter → bounded channel → Router → Renderer | `sync.WaitGroup` + `orderedPrinter` (mutex + indexed slots) in `sync.go` only |
| **Backpressure** | Progress = "latest wins" (droppable), lifecycle edges = never drop, `renderer.notice` counters | None — `orderedPrinter.set()` always succeeds (unbounded slice) |
| **Terminal control** | Double-buffered diff renderer with CUP/EL/ED, alternate screen, Unicode width | `\r\033[K` on a single progress line; all other output is append-only `fmt.Print` |
| **Rendering modes** | Full-screen (alt buffer) and inline (sticky footer) | Inline only — single `\r\033[K` line at bottom |
| **JSONL persistence** | Router tees events to `.sdf/logs/` as serialized JSONL for replay/debugging (side-effect, not communication) | No event persistence; spy recording captures git/gh/claude invocations but not UI events |
| **`--json` output** | Not explicitly addressed (research doc focuses on event streaming) | `--json` flag on `pr`/`new`/`config` outputs command-specific result objects |
| **Resize handling** | `SIGWINCH` + `term.GetSize` → force full redraw | No resize handling |
| **Error recovery** | Restore cursor/alt screen on panic; emit `task.error` consistently | Errors return up call stack; no terminal state cleanup |

### Where They Already Align

1. **Parallel execution, sequential display.** The terminal-ui-design doc's core rule #2 ("fire goroutines concurrently but display results in order") matches the research doc's renderer-managed slots. `orderedPrinter` is a minimal implementation of this exact pattern.

2. **One line per action.** Both documents agree on the output density — one semantic line per task outcome. The research doc's `task.end` events map 1:1 to `orderedPrinter`'s result strings.

3. **Semantic symbols.** The existing `ui.SymOK/SymFail/SymConflict/SymWarn/SymPlan` set maps directly to the `task.end.status` field values (`succeeded/failed/conflict/warning/planned`).

4. **Single writer rule.** `orderedPrinter` already ensures only one goroutine flushes to the terminal (via mutex + `finish()`). The research doc formalizes this as the renderer being "the sole writer."

---

## 3. Gap Analysis: What's Missing

### Gap 1: No Event Bus (the core abstraction)

Today, commands interleave `fmt.Printf` calls between git/gh operations. There's no intermediate representation — output is generated inline with execution.

**What it takes:**
- Define the `Event` struct (the research doc provides complete Go code)
- Create a `Reporter` that wraps a `chan<- Event` with convenience methods (`Start`, `Progress`, `LogLine`, `End`)
- Commands create tasks and call reporter methods instead of `fmt.Printf`

**Effort:** Small. The struct definitions and reporter are ~100 lines. The research doc provides working code.

### Gap 2: No Router/Renderer Separation

There's no component that:
- Assigns global sequence numbers
- Applies coalescing (latest-wins for progress)
- Manages terminal layout (slot assignment)
- Drives a tick-based render loop

**What it takes:**
- A `Router` goroutine that reads from the event channel, assigns seq, optionally persists to JSONL, and forwards to the renderer
- A `Renderer` that maintains a frame buffer and diffs on tick (10-20 Hz)

**Effort:** Medium. The router is ~150 lines. The renderer depends on scope — the research doc's minimal line-buffer renderer is ~100 lines; adding lipgloss styling and multi-region layout adds more.

### Gap 3: No Command Orchestration Layer

Each `Run*` function directly calls git/gh functions and prints. There's no "task plan → execute tasks → collect results" separation.

**What it takes:**
- Each command defines its work as a list of `TaskSpec` (id, name, function)
- An orchestrator runs them via errgroup with a Reporter per task
- Results flow back through the event bus to the renderer

**Effort:** This is the big one. It requires refactoring each command's execution path. But not all commands need it — only commands with parallelizable work benefit (see Section 4).

### Gap 4: No JSONL Persistence

The spy recording system already captures git/gh/claude invocations as JSONL — but only under the `spyrecord` build tag for testing. UI events are never captured.

**What it takes:**
- The router tees events to a file writer (the research doc's `json.Encoder` approach)
- Write to `.sdf/logs/<run-id>.jsonl`

**Effort:** Trivial once the router exists — it's a single `tee` from the router's output.

---

## 4. Separate Module vs. Internal Package

### Option A: `internal/render` (Recommended)

```
internal/
  render/
    event.go      — Event, TaskRef, RunRef structs (Go types, not JSON schemas)
    reporter.go   — Per-task Reporter with Start/Progress/Log/End (writes to chan Event)
    bus.go        — Orchestrator: wires reporters → router → renderer via channels
    router.go     — Global seq assignment, fans out to renderer + log writer
    renderer.go   — Renderer interface + TTY renderer (append-only, lipgloss styled)
    json.go       — JSON Renderer: collects task.end events, emits result object (--json)
    log.go        — JSONL log writer: serializes events to .sdf/logs/ (persistence only)
```

**Pros:**
- No public API contract to maintain — free to iterate
- Natural fit: `cmd/*` already imports `internal/*`
- Can use `internal/ui` styles directly (lipgloss, symbols)
- The spy recording pattern (`record_noop.go` / `record_spy.go`) is already per-package in `internal/`

**Cons:**
- Can't reuse in other projects without extracting later
- Tightly coupled to sdf's conventions (but that's fine for now)

### Option B: Standalone Go Module (`github.com/pavelpascari/termrender`)

**Pros:**
- Reusable across projects
- Forced clean API boundary
- Could attract community contributions

**Cons:**
- Premature — the API isn't validated yet
- Semver pressure: breaking changes require major version bumps
- Extra repo, CI, releases to manage
- The research doc's schema is comprehensive but untested — iterating on it is easier inside sdf

### Option C: `internal/render` now, extract later

Start with Option A. Once the API stabilizes after 2-3 commands use it, extract to a standalone module. This is the Go community's standard advice: "a little copying is better than a little dependency."

**Recommendation: Option C** (start with A, plan for extraction).

---

## 5. Which Commands Benefit and When

Not all commands need the full event bus. Categorize by parallelism potential:

### High Value (parallel work today or imminent)

| Command | Current Parallelism | Rendering Gain |
|---|---|---|
| `sync` | Partial (PR content updates via `orderedPrinter`) | Full pipeline: rebase tasks could report progress, conflict prompts interleave with status |
| `status` | None, but fetches + fast-forwards base | Could fetch PR state in parallel with base fast-forward |
| `merge` | Planned — merge head, sync, repeat | Natural task pipeline: merge → sync → repeat, each step reported |

### Medium Value (sequential but would benefit from structured output)

| Command | Why |
|---|---|
| `pr` | Push + create + nav update are serial but distinct steps; event stream enables `--json` event mode for agents |
| `fetch` / `register` | PR discovery + reconciliation are distinct phases |

### Low Value (keep as-is)

| Command | Why |
|---|---|
| `branch`, `new`, `switch` | Single fast operation, no parallelism needed |
| `config`, `init`, `doctor` | Diagnostic/setup — simple append-only output is fine |

### Suggested Adoption Order

1. **Start with `sync`** — it already has `orderedPrinter` and the most complex output. Migrate `orderedPrinter` into `internal/render/ordered.go`, then gradually wire the event bus into the sync pipeline.
2. **Then `status`** — it benefits from parallel PR fetching and is a read-only command (safe to experiment with).
3. **Then `merge`** — the planned command will need the orchestration layer from day one.

---

## 6. `--json` Mode: How It Works

The `--json` flag doesn't change the bus or the router. It swaps the **renderer**.

There are three renderer implementations, selected at bus creation:

### TTY Renderer (default)

Styled append-only output. Consumes events from the router, prints one line per task outcome using lipgloss/symbols. Drives the `\r\033[K` progress counter during parallel work. This is what `orderedPrinter` does today.

### JSON Renderer (`--json` flag)

A **collecting renderer**. It silently accumulates `task.end` events and, when the bus finishes, outputs a single JSON result object to stdout. No progress lines, no styled output — just the final result.

The JSON renderer does **not** emit JSONL event streams. It produces a command-specific result object, consistent with existing `--json` behavior (`sdf pr --json` → `PRResult`, `sdf new --json` → `NewResult`).

Example for `sdf sync --json`:

```json
{
  "stack": "my-feature",
  "base": "main",
  "tasks": [
    {
      "branch": "feature-1",
      "pr": 142,
      "action": "skip",
      "status": "merged"
    },
    {
      "branch": "feature-2",
      "pr": 143,
      "action": "rebase",
      "status": "succeeded"
    },
    {
      "branch": "feature-3",
      "pr": 144,
      "action": "rebase",
      "status": "failed",
      "error": "conflict in auth.go"
    }
  ],
  "prs_updated": 2
}
```

**How it works internally:**

```go
type JSONRenderer struct {
    results []TaskResult  // accumulated from task.end events
}

func (r *JSONRenderer) HandleEvent(ev render.Event) {
    if ev.Type == "task.end" {
        r.results = append(r.results, taskResultFrom(ev))
    }
    // All other events (progress, log) are silently consumed
}

func (r *JSONRenderer) Finish() error {
    // Command wraps r.results in its own result struct
    // e.g. SyncResult{Stack: "...", Tasks: r.results}
    return json.NewEncoder(os.Stdout).Encode(result)
}
```

The command owns the result shape — the JSON renderer collects raw task outcomes, and the command's `Run*` function wraps them into its specific result struct before encoding.

### JSONL Log (not a renderer — a router side-effect)

The JSONL log file (`.sdf/logs/*.jsonl`) is **always written** regardless of output mode. It captures every event (start, progress, log, end) as a JSON line. This is a side-effect of the router, not a renderer.

```
TTY mode:     Router → TTY Renderer → styled terminal
                 └──→ .sdf/logs/run.jsonl

--json mode:  Router → JSON Renderer → result JSON to stdout
                 └──→ .sdf/logs/run.jsonl

Piped/CI:     Router → TTY Renderer (plain, no color) → stdout
                 └──→ .sdf/logs/run.jsonl
```

The JSONL log is for debugging and replay. The `--json` output is for agents and scripts. They serve different audiences and have different schemas — events vs. results.

---

## 7. Architectural Recommendation

### Phase 1: Extract and Formalize (internal/render)

```
internal/render/
  event.go       — Event struct + types (from research doc, simplified)
  reporter.go    — Reporter: Start/Progress/Log/End
  bus.go         — Buffered channel + Run() that wires reporter→router→renderer
  renderer.go    — Renderer interface + TTY renderer (append-only, lipgloss styled)
  json.go        — JSON Renderer (collecting mode for --json flag)
  log.go         — JSONL log writer (router side-effect, persistence only)
```

**Do not build** the full double-buffered alt-screen renderer yet. Start with the append-only model from `terminal-ui-design.md` — it's what sdf uses today and it works. The event bus is the valuable part; the renderer can evolve independently.

### Phase 2: Wire Into sync

Replace `orderedPrinter` in `sync.go` with the new event bus:

```go
// Before (today)
var wg sync.WaitGroup
printer := newOrderedPrinter(os.Stdout, len(jobs), "Updating PR content")
for _, j := range jobs {
    wg.Add(1)
    go func(j contentJob) {
        defer wg.Done()
        // ... do work ...
        printer.set(j.index, result)
    }(j)
}
wg.Wait()
printer.finish()

// After (with render package)
bus := render.NewBus(os.Stdout, render.Options{
    Mode:     render.ModeOrdered, // append-only, sequential display
    Parallel: len(jobs),
})
for _, j := range jobs {
    bus.AddTask(render.TaskSpec{
        ID:   fmt.Sprintf("pr-%d", j.pr),
        Name: fmt.Sprintf("PR #%d (%s)", j.pr, j.branch),
        Fn: func(ctx context.Context, r *render.Reporter) error {
            // ... do work, call r.LogLine(), r.Progress() ...
            return nil
        },
    })
}
return bus.Run(ctx) // handles errgroup, rendering, cleanup
```

### Phase 3: JSONL Persistence + JSON Renderer

- Router tees events to `.sdf/logs/<timestamp>.jsonl` as a side-effect (always, regardless of output mode)
- Add `JSONRenderer` — a collecting renderer that accumulates `task.end` events and emits a single result object when `--json` is passed
- Each command defines its own result struct; the JSON renderer provides raw task outcomes for the command to wrap
- JSONL logs are for debugging/replay; `--json` output is for agents/scripts (different schemas, different audiences)

### Phase 4: Advanced Renderer (if needed)

Only build the double-buffered, multi-region, alt-screen renderer if a command genuinely needs it (e.g., a future `sdf dashboard` or long-running `sdf watch`). The append-only model covers sync/status/merge.

---

## 8. What to Simplify from the Research Doc

The research doc is thorough but over-specified for sdf's current needs. Defer these:

| Feature | Why Defer |
|---|---|
| Schema B (compact progress frames) | sdf won't have thousands of progress events per second |
| Layout regions (header/tasks/log/footer) | Append-only mode doesn't need layout management |
| Alt screen buffer | No full-screen commands planned |
| Backpressure / coalescing | Channel buffers of 100-1000 are fine for 2-10 parallel tasks |
| `renderer.notice` counters | Useful for debugging but not MVP |
| `layout.assign` events | Only needed for multi-region renderers |
| Run-level events (`run.start`) | Single-run CLI doesn't need session management |
| Unicode width handling | Already handled by lipgloss; defer custom truncation |

### Keep from the Research Doc

| Feature | Why Keep |
|---|---|
| Event struct with type + task + data | Core abstraction — enables everything else |
| Reporter pattern | Clean API for commands to emit events |
| errgroup-based orchestrator | Better than raw WaitGroup — cancellation + error propagation |
| JSONL persistence (router side-effect) | Debugging/replay value is immediate; trivial to implement |
| JSON Renderer (`--json` collecting mode) | Agent compatibility — same pattern as existing `PRResult`/`NewResult` |
| Per-task seq for ordering | Needed for ordered display |

---

## 9. Dependency Impact

### New Dependencies: None

Everything needed is already in the dependency tree:
- `encoding/json` — stdlib
- `sync`, `context` — stdlib
- `golang.org/x/sync/errgroup` — already used via charm ecosystem (check `go.sum`)
- `charmbracelet/lipgloss` — already a dependency
- `golang.org/x/term` — already a dependency (via huh/lipgloss)

### Files Touched

Phase 1 (new package): 4-5 new files in `internal/render/`
Phase 2 (wire sync): `cmd/sync.go` — replace `orderedPrinter` block (~40 lines changed)
Phase 3 (persistence): `internal/render/format.go` + minor router addition

---

## 10. Summary

The research document is a **solid reference architecture** for the problem space. It over-specifies for sdf's immediate needs but that's correct for a research document — it maps the full territory.

The practical path is:

1. Build `internal/render` with the event struct + reporter + TTY renderer (append-only)
2. Wire it into `sync` (replacing `orderedPrinter`)
3. Add JSONL log persistence (router side-effect) + JSON collecting renderer (`--json`)
4. Let the API stabilize across 2-3 commands before considering extraction

The core insight from both documents is the same: **separate event production from rendering**. The event bus (Go structs over channels) is the valuable abstraction. The renderer is pluggable — start simple (append-only), evolve as needed. JSONL is a serialization detail for persistence and debugging, never the in-process communication mechanism.
