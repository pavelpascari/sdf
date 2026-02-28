# Sync --json Mode Design

## Problem

The sync command now routes all output through `render.Bus`, but there's no way to get structured JSON output. Scripts and CI systems need machine-readable results from `sdf sync` — which branches were rebased, which PRs were updated, whether sync succeeded or hit conflicts.

## Goal

Add `--json` flag to `sdf sync` that emits a single JSON object to stdout, suppresses all TTY output, skips interactive prompts, and aborts on conflicts.

## Architecture

### Approach: Result struct alongside Bus

Follow the existing pattern in `pr.go` and `new.go`: the command builds a `SyncResult` struct during execution. When `--json`, the Bus uses a quiet renderer (JSONRenderer) to suppress TTY output. After sync completes, the result struct is marshaled to stdout.

### Output Schema

```json
{
  "stack": "my-feature",
  "base": "main",
  "branches": [
    {"branch": "feat-a", "pr": 42, "action": "merged"},
    {"branch": "feat-b", "pr": 43, "action": "rebased", "pushed": true, "base_updated": true},
    {"branch": "feat-c", "pr": 0, "action": "skipped", "reason": "blocked by failed branch"}
  ],
  "pr_updates": [
    {"pr": 43, "field": "nav", "status": "updated"}
  ],
  "warnings": ["could not fast-forward main: ..."],
  "error": ""
}
```

### Result Types

```go
type SyncResult struct {
    Stack     string         `json:"stack"`
    Base      string         `json:"base"`
    Branches  []BranchResult `json:"branches"`
    PRUpdates []PRUpdate     `json:"pr_updates,omitempty"`
    Warnings  []string       `json:"warnings,omitempty"`
    Error     string         `json:"error,omitempty"`
}

type BranchResult struct {
    Branch      string `json:"branch"`
    PR          int    `json:"pr,omitempty"`
    Action      string `json:"action"`
    Pushed      bool   `json:"pushed,omitempty"`
    BaseUpdated bool   `json:"base_updated,omitempty"`
    Reason      string `json:"reason,omitempty"`
}

type PRUpdate struct {
    PR     int    `json:"pr"`
    Field  string `json:"field"`
    Status string `json:"status"`
}
```

### Interactive Prompts in JSON Mode

| Prompt | JSON behavior |
|---|---|
| `confirmSync()` | Skip (imply `--yes`) |
| `promptOnConflict()` | Abort sync, set error in result |
| `promptCreateMissingPRs()` | Skip entirely |

### Data Flow

```
runSyncCmd
  ├── --json → bus uses JSONRenderer, result := &SyncResult{}
  ├── runSyncFull(root, ..., result, bus)
  │     ├── bus.Printf (TTY: printed, JSON: suppressed)
  │     ├── result.Branches = append(result.Branches, ...)
  │     ├── runSyncFrom(root, s, 0, opts, result, bus)
  │     │     ├── per-branch: result.Branches = append(...)
  │     │     ├── conflict: result.Error = "...", return
  │     │     └── PR content/nav: result.PRUpdates = append(...)
  │     └── warnings collected on result.Warnings
  ├── bus.Finish()
  └── json.NewEncoder(os.Stdout).Encode(result)
```

### Warning Collection

Warnings are collected in `result.Warnings` when in JSON mode. The bus still receives `bus.Warnf(...)` calls (the JSONRenderer collects them too), but for the structured output, warnings are explicitly appended to the result struct — this avoids depending on the renderer to round-trip warning text.

### Functions That Change

| Function | Change |
|---|---|
| `syncOptions` | Add `jsonMode bool` |
| `runSyncCmd` | Add `--json` flag, pick renderer, marshal at end |
| `runSyncFull` | Accept `*SyncResult`, populate, skip confirm when json |
| `runSyncFrom` | Accept `*SyncResult`, populate per-branch results |
| `promptOnConflict` | When json: abort, set result.Error |
| `promptCreateMissingPRs` | When json: return early |
| `updatePRContent` | Accept `*SyncResult`, populate PRUpdates |
| `updateStackNavForAllPRs` | Accept `*SyncResult`, populate PRUpdates |

### What Does NOT Change

- `render.Bus` API
- `render.JSONRenderer` — used as-is
- `render.TTYRenderer` — unchanged
- `fmt.Errorf` calls — still error values
- `fmt.Fprintf(&b, ...)` — still string building

### `--continue` with `--json`

When `--json` is combined with `--continue`, the result includes only the branches processed after resume. The stack/base fields reflect the resumed stack. If a rebase is in progress, it fails with a structured error.

### Error Handling

When sync encounters an error (conflict, rebase failure), the result is still emitted with the `error` field set. Already-processed branches appear in the `branches` array. Exit code is non-zero.
