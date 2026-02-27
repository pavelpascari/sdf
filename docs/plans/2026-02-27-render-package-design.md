# Design: `internal/render` Package

Date: 2026-02-27
Status: Approved
Inputs: `2026-02-27-advanced-rendering.md`, `2026-02-27-rendering-alignment-analysis.md`, `2026-02-20-terminal-ui-design.md`

## Overview

A rendering and orchestration package for parallel CLI task execution. Commands define tasks, the bus runs them, and a pluggable renderer presents progress to the user.

**Key principle:** JSONL is a serialization format for persistence and external output. In-process communication uses typed Go structs over channels.

```
                          In-process (Go structs over channels)
                         ┌──────────────────────────────────────────────┐
                         │                                              │
  goroutine 1 ─→ Reporter ─→ chan Event ─→ Router ─→ Renderer ─→ terminal
  goroutine 2 ─→ Reporter ─┘               │
  goroutine N ─→ Reporter ─┘               │
                                            │  side-effect (serialization)
                                            └─→ json.Encoder ─→ .sdf/logs/*.jsonl
```

## Core Types

### Event

The unit of communication on the bus. A Go struct, never serialized in-process.

```go
type Event struct {
    Type   string    // "task.start", "task.log", "task.end"
    TS     time.Time
    Seq    uint64    // global sequence, assigned by router
    TaskID string
    Data   any       // type-specific payload
}
```

Event types:

| Type | Data | Semantics |
|---|---|---|
| `task.start` | `{name: string}` | Task is running, renderer allocates a slot |
| `task.log` | `{text: string}` | Status update — overwrites previous log in the slot |
| `task.end` | `{status: string, message: string}` | Terminal state — slot shows final result |

`task.progress` (fractional) is not needed initially. No sdf command has fractional progress. Can be added later without breaking changes.

### Reporter

Per-task handle. Commands call its methods instead of `fmt.Printf`. Wraps a `chan<- Event` with convenience methods.

```go
type Reporter struct {
    bus     *Bus
    taskID  string
    taskSeq atomic.Uint64
}

func (r *Reporter) Start(name string)              // sends task.start
func (r *Reporter) Log(text string)                 // sends task.log (overwrites previous)
func (r *Reporter) End(status string, msg string)   // sends task.end
func (r *Reporter) Pause()                          // tells renderer to clear progress area
func (r *Reporter) Resume()                         // tells renderer to re-take terminal
```

Each `r.Log()` call replaces the slot's current status text. The renderer picks up the change on the next tick. Multiple `r.Log()` calls within a tick — only the last one is displayed.

### TaskSpec

Defines a unit of work for the bus.

```go
type TaskSpec struct {
    ID   string
    Name string
    Fn   func(ctx context.Context, r *Reporter) error
}
```

### Renderer Interface

Pluggable output. Swapped based on `--json` flag / TTY detection.

```go
type Renderer interface {
    Init(taskCount int)      // allocate slots
    HandleEvent(Event)       // process an event
    Flush()                  // called on tick (TTY) or no-op (JSON)
    Pause()                  // clear progress area for interactive prompts
    Resume()                 // re-take terminal control
    Finish()                 // final cleanup
}
```

## Bus

The single entry point for commands. One bus per command invocation.

```go
type Bus struct {
    renderer Renderer
    logFile  *LogWriter     // JSONL persistence (always on)
    seq      atomic.Uint64  // global sequence counter
    events   chan Event      // bounded channel
    tasks    []TaskSpec      // registered tasks
}

func NewBus(w io.Writer, opts Options) *Bus
func (b *Bus) AddTask(task TaskSpec)
func (b *Bus) Run(ctx context.Context, task TaskSpec) error     // single task, sequential
func (b *Bus) RunBatch(ctx context.Context) error               // all added tasks, parallel
func (b *Bus) Finish() error                                    // flush logs, finalize renderer
```

**`Run()`** — executes one task. Creates a Reporter, calls the task function, waits for completion. Used for sequential phases where order matters (e.g., rebase steps in sync).

**`RunBatch()`** — launches all added tasks via `errgroup`, each with its own Reporter. The renderer displays results in insertion order. Used for parallel phases (e.g., PR content updates).

**Router** — internal to the bus. A goroutine that reads from the `events` channel, assigns global `seq`, tees to the JSONL log writer, and forwards to the renderer via `HandleEvent()`. Commands never interact with the router directly.

### Command Ownership

The bus does not own the command flow. The command calls `Run()` and `RunBatch()` as needed, interleaving its own logic between calls:

```go
func RunSync(...) error {
    // Sequential logic — no bus needed
    git.FetchAll()
    plan := computeSyncPlan(...)
    if !ui.Confirm("Proceed?") { return nil }

    bus := render.NewBus(os.Stdout, render.Options{LogDir: root + "/.sdf/logs"})

    // Sequential rebase steps (future — use Run())
    for _, step := range plan {
        bus.Run(ctx, render.TaskSpec{
            Name: fmt.Sprintf("rebase %s", step.Branch),
            Fn: func(ctx context.Context, r *render.Reporter) error {
                r.Log("rebasing...")
                if err := git.RebaseOnto(...); err != nil {
                    r.Pause()
                    choice := ui.Select(...)  // huh prompt
                    r.Resume()
                    // handle choice...
                }
                return git.Push(...)
            },
        })
    }

    // Parallel PR content updates (first adoption target)
    for _, j := range jobs {
        bus.AddTask(render.TaskSpec{
            Name: fmt.Sprintf("PR %s", ui.PR(j.node.PR)),
            Fn: func(ctx context.Context, r *render.Reporter) error {
                r.Log("updating title...")
                // ... title logic ...
                r.Log("generating description...")
                // ... description logic ...
                r.End("succeeded", "updated (title + description)")
                return nil
            },
        })
    }
    bus.RunBatch(ctx)

    return bus.Finish()
}
```

## Renderers

### TTYRenderer

Two modes depending on how the bus is used:

#### Batch Mode (`RunBatch`)

Multi-line in-place block. Each task owns a persistent line that updates in place:

```
  PR #56: updating title...          ← slot 0, updates in-place
  PR #57: reading commit content...  ← slot 1, updates in-place
  PR #58: waiting...                 ← slot 2, updates in-place

  ⠋ Updating PR content (0/3)       ← spinner, animates on tick
```

As tasks complete, their slots show final state:

```
  PR #56: ✓ unchanged                ← slot 0, done
  PR #57: writing description...     ← slot 1, still updating
  PR #58: ✓ updated (title)          ← slot 2, done

  ⠋ Updating PR content (2/3)
```

All done:

```
  PR #56: ✓ unchanged
  PR #57: ✓ updated (description)
  PR #58: ✓ updated (title)

  Updated content for 2/3 PRs.
```

Implementation:

```go
type TTYRenderer struct {
    w        io.Writer
    slots    []Slot
    startRow int          // terminal row where block starts
    spinner  int          // current spinner frame
    ticker   *time.Ticker // 10 Hz render loop
}

type Slot struct {
    Label  string // "PR #57"
    Status string // current status, overwritten by task.log
    Done   bool   // true after task.end
    Final  string // final result line
}
```

On each tick (100ms):
1. For each slot: CUP to `startRow + i`, EL to clear, write `label: status`
2. Move to spinner row, write spinner frame + completion counter
3. ~10-20 lines of ANSI writes per frame — negligible cost

Event handling:
- `task.start` → allocate slot, set initial status
- `task.log` → `slot.Status = text` (picked up on next tick)
- `task.end` → `slot.Done = true`, `slot.Final = formatted result line`

Pause/Resume:
- `Pause()` → stop ticker, move cursor below block
- `Resume()` → re-render all slots, restart ticker

#### Sequential Mode (`Run`)

Append-only, no cursor movement. Same as today's behavior:
- `task.log` → prints a line
- `task.end` → prints the final result line

No block, no spinner. Used for sequential phases where tasks execute one at a time.

### JSONRenderer

Silently collects task outcomes. No terminal output.

```go
type JSONRenderer struct {
    results []TaskResult
}

type TaskResult struct {
    TaskID  string
    Status  string
    Message string
    Data    any
}

func (r *JSONRenderer) Results() []TaskResult
```

- `HandleEvent` accumulates `task.end` events into `results`
- All other events silently consumed
- `Pause`/`Resume` are no-ops (no interactive prompts in JSON mode)
- `Finish()` is a no-op — the command retrieves `Results()`, builds its own result struct, and prints

The command owns both the result shape and the output:

```go
results := jsonRenderer.Results()
summary := SyncResult{
    Stack:      s.StackID,
    PRsUpdated: countUpdated(results),
    Tasks:      results,
}
json.NewEncoder(os.Stdout).Encode(summary)
```

## JSONL Log Writer

A router side-effect, not a renderer. Always active regardless of output mode.

```go
type LogWriter struct {
    enc  *json.Encoder // wraps bufio.Writer → file
    path string
}
```

- Router calls `logWriter.Write(event)` for every event
- `json.Encoder.Encode()` appends a newline automatically (natural JSONL)
- `SetEscapeHTML(false)` for readability
- Buffered with `bufio.Writer`, flushed on `bus.Finish()`
- Path: `.sdf/logs/YYYY-MM-DDTHH-MM-SS.jsonl`

The JSONL log captures every event — start, log, end. Useful for debugging and test replay.

```
TTY mode:     Router → TTYRenderer → styled terminal
                 └──→ .sdf/logs/run.jsonl

--json mode:  Router → JSONRenderer (silent) → command prints result
                 └──→ .sdf/logs/run.jsonl
```

## Package Layout

```
internal/render/
  event.go       — Event struct, event type constants
  reporter.go    — Reporter: Start/Log/End/Pause/Resume
  bus.go         — Bus: NewBus/AddTask/Run/RunBatch/Finish, router goroutine
  renderer.go    — Renderer interface definition
  tty.go         — TTYRenderer: batch mode (multi-line block) + sequential mode
  json.go        — JSONRenderer: collects task.end, exposes Results()
  log.go         — LogWriter: JSONL persistence
  ansi.go        — CUP, EL, HideCursor, ShowCursor helpers
```

7 files. No new dependencies — `encoding/json`, `sync`, `context`, `golang.org/x/sync/errgroup`, `charmbracelet/lipgloss`, `golang.org/x/term` are all already in `go.mod`.

## First Consumer: sync.go

Replace `orderedPrinter` in `updatePRContent()`.

Before (today):
```go
printer := newOrderedPrinter(len(jobs), "Updating PR content...")
var wg sync.WaitGroup
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
```

After:
```go
bus := render.NewBus(os.Stdout, render.Options{LogDir: root + "/.sdf/logs"})
for _, j := range jobs {
    j := j
    bus.AddTask(render.TaskSpec{
        ID:   fmt.Sprintf("pr-%d", j.node.PR),
        Name: fmt.Sprintf("PR %s", ui.PR(j.node.PR)),
        Fn: func(ctx context.Context, r *render.Reporter) error {
            r.Log("updating title...")
            // ... title logic ...
            r.Log("generating description...")
            // ... description logic ...
            r.End("succeeded", "updated (title + description)")
            return nil
        },
    })
}
bus.RunBatch(ctx)
bus.Finish()
```

`orderedPrinter` struct and methods get deleted. `contentJob.index` becomes unnecessary.

## Testing Strategy

### Unit tests (no terminal)

- `reporter_test.go` — sends correct events in order, per-task seq increments
- `bus_test.go` — `Run` executes single task, `RunBatch` executes in parallel, errors propagate via errgroup, context cancellation works
- `json_test.go` — accumulates task.end results, ignores other events
- `log_test.go` — produces valid JSONL (one line per event, parseable)

### TTY renderer tests

- Output captured to `bytes.Buffer`
- Assert CUP/EL sequences target correct rows
- Slot updates: Log overwrites previous status, End finalizes
- Ordering: out-of-order completions display in task-index order
- Pause/Resume: progress area clears and re-renders

### Integration test

- After wiring into sync, existing sync tests validate end-to-end flow
- One test that runs `updatePRContent` with fake `gh`/`claude` and asserts the JSONL log contains expected events

## Deferred

| Feature | Reason |
|---|---|
| `r.Progress(fraction)` | No sdf command has fractional progress yet |
| Alt screen buffer | No full-screen commands planned |
| Backpressure / coalescing | 2-10 parallel tasks won't saturate a channel |
| Resize handling | Fixed block of N lines is small, unlikely to break |
| Spinner customization | Default is fine |
| Log pruning | `.sdf/logs/` cleanup can be added later |
