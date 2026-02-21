# Terminal UI Design Principles

Date: 2026-02-20
Status: Active

## Core Rules

### 1. Print-once, append-only

Every line is printed exactly once and never rewritten. No ANSI cursor
movement (`\033[A`/`\033[B`), no multi-line rewrites. This eliminates jitter
and works in every terminal, pipe, and log file.

**One exception**: a single progress counter line at the bottom during parallel
work (`\r\033[K` to overwrite in place). This is universally supported.

### 2. Parallel execution, sequential display

Fire goroutines concurrently but display results in order. Buffer early
completions and print them once the preceding slots are filled. The user
sees a steady stream of lines appearing top-to-bottom.

Implementation: `orderedPrinter` — goroutines write results to indexed slots,
a single progress line updates `(completed/total)`, `finish()` prints all
results in order.

### 3. One line per action

```
  ✓ PR #142 (db-schema) merged
  rebasing repository onto main...
  ✓ repository rebased and pushed
  ⚡ conflict in user.go — invoking Claude
  ✓ conflict resolved by Claude
  ✗ push failed for controller
```

No multi-line status blocks. No indented sub-status. One action = one line.

### 4. Symbols are semantic

| Symbol | Meaning              |
|--------|----------------------|
| `✓`    | Completed            |
| `→`    | Planned (in plan)    |
| `✗`    | Failed               |
| `⚡`    | Conflict (attention) |
| `⚠`    | Warning (non-fatal)  |

In-progress lines use no symbol, just indentation:
```
  rebasing branchA onto main...
```

### 5. Include context in the line

Show PR number and branch together so the line is self-contained:
```
  ✓ PR #142 (db-schema) merged
```
Not:
```
  ✓ branchA is merged
```

### 6. Batch related updates

Group related operations into summary lines where possible:
```
  ✓ 2 PR base(s) updated on GitHub
```
Instead of individual lines for each PR base update.

## Output Patterns

### Sync plan (preview)

```
Sync plan:
  ✓ PR #142 (db-schema) merged
  → rebase repository onto main + push
  → update PR #143 base → main
  → rebase controller onto repository + push
  → update PR #143 content
  → update PR #144 content
```

### Sync execution

```
  ✓ PR #142 (db-schema) merged
  rebasing repository onto main...
  ✓ repository rebased and pushed
  rebasing controller onto repository...
  ✓ controller rebased and pushed
  ✓ 2 PR base(s) updated on GitHub

Sync complete. Stack updated.
```

### PR content updates (parallel with progress)

```
  Updating PR content... (2/3)
  ✓ PR #142 updated (title + description)
  ✓ PR #143 updated (title)
  ✓ PR #144 unchanged
```

### Conflict resolution

```
  rebasing repository onto main...
  ⚡ conflict in repository — 2 file(s)

  [c] Ask Claude to resolve
  [m] I'll fix it myself
  [s] Skip this branch
  [a] Abort sync

  > c
  ✓ conflict resolved by Claude
  ✓ repository rebased and pushed
```

## Implementation: orderedPrinter

```go
type orderedPrinter struct {
    mu        sync.Mutex
    w         io.Writer
    results   []string
    completed int
    total     int
    label     string
}
```

- `set(index, result)`: stores result, increments counter, updates progress line
- `finish()`: clears progress line, prints all results in order
- Thread-safe via mutex
- Single `\r\033[K` for progress — no cursor movement
