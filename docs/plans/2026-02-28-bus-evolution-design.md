# Bus Evolution Design — Eliminate fmt.Print from Commands

## Problem

Commands use `fmt.Print*` for informational output, warnings, and summaries alongside `render.Bus` for parallel task rendering. This breaks `--json` mode (text leaks into structured output) and splits output responsibility between the command and the renderer.

## Goal

Route **all** command output through the Bus. Commands never call `fmt.Print*` directly — the Bus and Renderer own stdout and stderr.

## Architecture

Extend the existing Bus with three new output methods (`Print`, `Warn`, `Err`) and two batch lifecycle events. The Renderer interface simplifies — mode transitions become event-driven instead of requiring explicit `Init` calls.

### Data Flow

```
Command
  │
  ├── bus.Print("Syncing stack foo...")     → EventPrint  → Router → Renderer
  ├── bus.Run(ctx, fetchTask)               → task events → Router → Renderer
  ├── bus.Warn("fetch failed: ...")         → EventWarn   → Router → Renderer
  ├── bus.AddTask(...) × N
  ├── bus.RunBatch(ctx)                     → EventBatchStart/End → Router → Renderer
  │     └── task events (parallel)          → task events → Router → Renderer
  ├── bus.Pause() / bus.Resume()            → pause/resume events → Router → Renderer
  ├── bus.Err(err)                          → EventErr    → Router → Renderer
  ├── bus.Print("Sync complete.")           → EventPrint  → Router → Renderer
  └── bus.Finish()
```

All events flow through the same channel and router. The Renderer decides per-event what to do based on its mode (TTY vs JSON).

## Event Types

```go
// Existing — task lifecycle
EventTaskStart = "task.start"
EventTaskLog   = "task.log"
EventTaskEnd   = "task.end"

// Existing — renderer control
EventPause  = "renderer.pause"
EventResume = "renderer.resume"

// New — bus-level output
EventPrint = "bus.print"    // Data: {"text": "..."}
EventWarn  = "bus.warn"     // Data: {"text": "..."}
EventErr   = "bus.err"      // Data: {"text": "..."}

// New — batch lifecycle
EventBatchStart = "batch.start"  // Data: {"count": N, "label": "..."}
EventBatchEnd   = "batch.end"
```

Bus-level events (`print`, `warn`, `err`) have no TaskID. Batch events have no TaskID.

## Bus API

### Constructor

```go
func NewBus(w, errw io.Writer, opts Options) *Bus
```

Takes both stdout and stderr writers. When `opts.Renderer` is nil, creates a TTYRenderer with both writers. When a custom renderer is provided via opts, the caller is responsible for its output streams.

### New Methods

```go
// Print emits informational text. TTY: prints to stdout. JSON: no-op.
func (b *Bus) Print(text string)

// Printf is a convenience wrapper with fmt.Sprintf formatting.
func (b *Bus) Printf(format string, args ...any)

// Warn emits a warning. TTY: prints to stderr. JSON: collected into output.
func (b *Bus) Warn(text string)

// Warnf is a convenience wrapper with fmt.Sprintf formatting.
func (b *Bus) Warnf(format string, args ...any)

// Err emits an error. TTY: prints to stderr. JSON: collected into output.
func (b *Bus) Err(err error)

// Pause sends a renderer.pause event. Use before interactive prompts.
func (b *Bus) Pause()

// Resume sends a renderer.resume event. Use after interactive prompts.
func (b *Bus) Resume()
```

### Existing Methods (unchanged signatures)

```go
func (b *Bus) AddTask(task TaskSpec)
func (b *Bus) Run(ctx context.Context, task TaskSpec) error
func (b *Bus) RunBatch(ctx context.Context) error
func (b *Bus) Finish() error
```

`Run` no longer calls `Init(0)` — it sends task events directly. The renderer handles sequential tasks as append-only output.

`RunBatch` sends `batch.start` before the errgroup and `batch.end` after. The renderer enters/exits batch mode based on these events.

## Renderer Interface

```go
type Renderer interface {
    HandleEvent(Event)
    Flush()
    Finish()
}
```

Removed from current interface: `Init(int)`, `Pause()`, `Resume()` — all handled via events now.

## TTYRenderer Behavior

| Event | Behavior |
|-------|----------|
| `bus.print` | `fmt.Fprintf(r.w, "%s\n", text)` — stdout |
| `bus.warn` | `fmt.Fprintf(r.errw, "%s\n", text)` — stderr |
| `bus.err` | `fmt.Fprintf(r.errw, "%s\n", text)` — stderr |
| `batch.start` | Enter batch mode: reserve N+2 lines, hide cursor, allocate slots |
| `task.start` (in batch) | Add slot with task name |
| `task.start` (outside batch) | No-op or print task name |
| `task.log` (in batch) | Update slot status text |
| `task.log` (outside batch) | `fmt.Fprintf(r.w, "  %s\n", text)` |
| `task.end` (in batch) | Mark slot done with final message |
| `task.end` (outside batch) | `fmt.Fprintf(r.w, "  %s\n", message)` |
| `batch.end` | Final flush, show cursor, reset batch state |
| `renderer.pause` | Show cursor, stop flushing |
| `renderer.resume` | Hide cursor, resume flushing |

### Constructor

```go
func NewTTYRenderer(w, errw io.Writer) *TTYRenderer
```

Takes both writers. `SetLabel` still works — applied when `batch.start` arrives.

## JSONRenderer Behavior

| Event | Behavior |
|-------|----------|
| `bus.print` | No-op |
| `bus.warn` | Append to `warnings []string` |
| `bus.err` | Append to `errors []string` |
| `batch.start` | No-op |
| `task.start` | No-op |
| `task.log` | No-op |
| `task.end` | Append to `results []TaskResult` |
| `batch.end` | No-op |
| `renderer.pause` | No-op |
| `renderer.resume` | No-op |

### New Accessors

```go
func (r *JSONRenderer) Warnings() []string
func (r *JSONRenderer) Errors() []string
```

The command calls `Results()`, `Warnings()`, `Errors()` after `bus.Finish()` to assemble its own JSON output structure. The renderer collects; the command shapes.

## LogWriter Behavior

All events (including new types) are written as JSONL. No behavior changes needed — it already serializes any Event.

## Interactive Prompts

Interactive prompts (conflict resolution menus, confirmation dialogs) use `huh` (Charm) which writes directly to stdout and reads stdin. They cannot go through the renderer.

Pattern:
```go
bus.Pause()                    // sends renderer.pause event
choice := ui.Select(...)       // huh widget, direct terminal access
bus.Resume()                   // sends renderer.resume event
```

In `--json` mode, commands should skip interactive prompts (auto-confirm or fail). This is a separate concern from this design.

## Usage Example

```go
bus := render.NewBus(os.Stdout, os.Stderr, render.Options{})

bus.Printf("Syncing stack %s...", stackID)
bus.Print("Fetching from origin...")

err := bus.Run(ctx, render.TaskSpec{
    ID: "fetch", Name: "fetch",
    Fn: func(ctx context.Context, r *render.Reporter) error {
        return gitpkg.FetchAll()
    },
})
if err != nil {
    bus.Warnf("fetch failed: %v", err)
}

// Parallel section
for _, task := range rebaseTasks {
    bus.AddTask(task)
}
if err := bus.RunBatch(ctx); err != nil {
    bus.Warnf("some rebases failed: %v", err)
}

// Another parallel section
for _, task := range navTasks {
    bus.AddTask(task)
}
if err := bus.RunBatch(ctx); err != nil {
    bus.Warnf("some nav updates failed: %v", err)
}

bus.Print("Sync complete.")
bus.Finish()
```

## Migration Strategy

This design is backwards-compatible at the render package level — existing Bus users still work. The migration is:

1. Update render package (new events, new Bus methods, simplified Renderer interface)
2. Update existing renderers (TTYRenderer, JSONRenderer)
3. Update existing tests
4. Migrate commands one at a time (replace fmt.Print with bus.Print/Warn/Err)

Commands can be migrated incrementally — mixing fmt.Print and bus.Print works during transition (messy but functional).
