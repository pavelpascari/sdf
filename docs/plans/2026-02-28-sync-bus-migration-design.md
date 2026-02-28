# Sync Command Bus Migration Design

## Problem

`cmd/sync.go` has ~60 `fmt.Print*` calls scattered across 9 functions. This breaks `--json` mode (text leaks into structured output) and splits output responsibility between the command and the renderer. The render.Bus already supports `Print/Warn/Err` for sequential output and `RunBatch` for parallel sections, but sync.go only uses the bus for PR content and nav updates.

## Goal

Route all sync command output through a single `render.Bus` instance. After this migration, sync.go has zero `fmt.Print*` calls (except `fmt.Errorf` for error values, and `fmt.Fprintf` to `strings.Builder` for string construction).

## Architecture

### Single Bus Lifetime

One Bus is created at the entry point (`runSyncFull` or `runSyncContinue`) and threaded as an explicit `bus *render.Bus` parameter to every function that produces output. The bus owns stdout and stderr for the entire sync run.

```
runSyncFull / runSyncContinue
  │
  ├── bus := render.NewBus(os.Stdout, os.Stderr, opts)
  ├── bus.Printf("Syncing stack %s...", ...)
  ├── bus.Print("Fetching from origin...")
  ├── reconcileSyncPRStates(s, bus)
  ├── printSyncPlan(plan, bus)
  ├── bus.Pause() → confirmSync() → bus.Resume()
  ├── runSyncFrom(root, s, 0, opts, bus)
  │     ├── bus.Printf("  rebasing %s onto %s...", ...)
  │     ├── promptOnConflict(..., bus) → bus.Pause/Resume
  │     └── bus.Printf("  ✓ %s rebased and pushed", ...)
  ├── promptCreateMissingPRs(root, s, opts, bus) → bus.Pause/Resume
  ├── updatePRContent(root, s, opts, bus)  ← reuses parent bus
  ├── updateStackNavForAllPRs(root, s, bus) ← reuses parent bus
  └── bus.Finish()
```

### Output Mapping

| Before | After |
|---|---|
| `fmt.Printf(...)` | `bus.Printf(...)` |
| `fmt.Println(text)` | `bus.Print(text)` |
| `fmt.Fprintf(os.Stderr, "warning: ...")` | `bus.Warnf(...)` |
| `fmt.Fprintf(os.Stderr, "  ✗ ...")` | `bus.Warnf(...)` |

### Interactive Prompts

Conflict resolution menus and PR creation confirms use `huh` which writes directly to the terminal. These are wrapped with Pause/Resume:

```go
bus.Pause()
choice := ui.Select("How to resolve?", options)
bus.Resume()
```

The bus is always in sequential mode during prompts (never in a batch), so Pause/Resume simply ensures the cursor is visible and flushing stops.

### Functions That Receive `bus *render.Bus`

| Function | Notes |
|---|---|
| `runSyncFrom` | Main rebase loop output |
| `reconcileSyncPRStates` | Warning output for PR state drift |
| `printSyncPlan` | Plan display |
| `promptOnConflict` | Conflict info + Pause/Resume for menu |
| `tryClaude` | Claude invocation status + Pause/Resume for fallback menu |
| `pauseForManualResolution` | Instructions output |
| `promptCreateMissingPRs` | Pause/Resume for confirm + PR creation output |
| `updatePRContent` | Reuses parent bus (removes internal bus creation) |
| `updateStackNavForAllPRs` | Reuses parent bus (removes internal bus creation) |

### updatePRContent and updateStackNavForAllPRs Changes

These currently create their own Bus. They'll be updated to accept the parent bus and call `bus.AddTask` + `bus.RunBatch` directly. The parent bus handles interleaving sequential and parallel sections — `RunBatch` emits `batch.start`/`batch.end` events, and between batches the bus is in sequential mode where `Print/Warn` appends lines.

### What Stays as `fmt.Errorf`

Return errors remain as `fmt.Errorf` — they're error values, not output. The cobra `RunE` handler displays them. String builders (`fmt.Fprintf(&b, ...)`) for prompt construction also stay — they build strings, not terminal output.

### What Does NOT Change

- `fmt.Errorf` calls (error values)
- `fmt.Fprintf(&b, ...)` calls (string building into `strings.Builder`)
- `confirmSync()` internals (delegated to `ui.Confirm`)
- `ui.Select` / `ui.Confirm` internals (huh widgets)
- Test files

### Testing

No new tests for the migration — it's a mechanical replacement. Existing render package tests cover Bus behavior. Existing sync tests (`sync_test.go`) verify command logic, not output format.
