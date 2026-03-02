# sdf UI Overhaul — Library Research & Plan

**Date:** 2026-02-20
**Status:** Research / Planning

---

## Part 1: Go CLI UI Library Landscape

### TUI Frameworks (Full Terminal UI)

| Library | Stars | Maintained | Best For |
|---------|-------|------------|----------|
| [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) | ~29k | Yes (very active) | Elm-architecture TUI apps with composable components |
| [rivo/tview](https://github.com/rivo/tview) | ~11k | Yes | Traditional widget-based TUIs (forms, tables, trees) |
| [gdamore/tcell](https://github.com/gdamore/tcell) | ~5.1k | Yes (v3 released) | Low-level terminal cell manipulation (used by tview) |
| [gizak/termui](https://github.com/gizak/termui) | ~13k | Slow | Dashboard-style terminal UIs with charts |
| [mum4k/termdash](https://github.com/mum4k/termdash) | ~2.9k | Slow | Terminal dashboards with rich widgets |

### Styling & Rendering

| Library | Stars | Maintained | Best For |
|---------|-------|------------|----------|
| [charmbracelet/lipgloss](https://github.com/charmbracelet/lipgloss) | ~8.5k | Yes | CSS-like styling for terminal output (borders, colors, layout) |
| [charmbracelet/glamour](https://github.com/charmbracelet/glamour) | ~3.2k | Yes (v2) | Rendering markdown in the terminal |
| [fatih/color](https://github.com/fatih/color) | ~7.9k | Yes (v1.18) | Simple ANSI color output |
| [gookit/color](https://github.com/gookit/color) | ~1.5k | Yes | Extended color support (8/16/256/RGB) |
| [pterm/pterm](https://github.com/pterm/pterm) | ~5k | Yes | Batteries-included output: tables, spinners, progress bars, trees |

### Interactive Prompts & Forms

| Library | Stars | Maintained | Best For |
|---------|-------|------------|----------|
| [charmbracelet/huh](https://github.com/charmbracelet/huh) | ~4.5k | Yes | Beautiful interactive forms (successor to survey) |
| [AlecAivazis/survey](https://github.com/AlecAivazis/survey) | ~4.1k | **Archived** (Apr 2024) | Legacy — interactive prompts |
| [charmbracelet/bubbles](https://github.com/charmbracelet/bubbles) | ~5.5k | Yes | Composable TUI components (spinners, text inputs, lists, tables) |

### CLI Framework (Command Routing)

| Library | Stars | Maintained | Best For |
|---------|-------|------------|----------|
| [spf13/cobra](https://github.com/spf13/cobra) | ~43k | Yes | Industry-standard CLI command framework |
| [urfave/cli](https://github.com/urfave/cli) | ~22k | Yes | Lightweight CLI apps |

---

## Part 2: Recommendation for sdf

### Current State

sdf currently has **zero external Go dependencies**. All output is hand-rolled `fmt.Printf` with manual ANSI escape codes (see `parallelDisplay` in `sync.go`). The CLI routing is a raw `switch` statement in `main.go`. Interactive prompts are bare `bufio.NewReader(os.Stdin)`.

This works but produces output that is:
- Hard to maintain (raw ANSI cursor math)
- Inconsistent across commands (each formats differently)
- Not visually distinctive or polished
- Missing affordances users expect (spinners, progress, color-coded status)

### Recommended Stack: Charm Ecosystem

The **Charm ecosystem** (charmbracelet) is the clear winner for sdf because:

1. **Composable, not monolithic** — use only what you need, from simple styling to full TUI
2. **Actively maintained** by a funded company (Charm)
3. **Elm architecture** (bubbletea) matches sdf's event-driven patterns (sync progress, streaming Claude output)
4. **Used by major Go CLIs** — GitHub CLI's `gh` uses lipgloss and glamour

#### Recommended libraries to adopt:

| Library | Purpose in sdf |
|---------|---------------|
| **lipgloss** | Consistent styling: colors, borders, padding for all output |
| **bubbles** | Spinner (during fetch/push), progress bar (sync), table (status) |
| **huh** | Interactive prompts (conflict resolution, confirmations, branch selection) |
| **glamour** | Rendering PR descriptions in terminal |

#### What NOT to adopt (yet):

| Library | Why skip |
|---------|----------|
| **bubbletea** | Full TUI framework is overkill — sdf is a command-and-exit CLI, not an interactive app |
| **cobra** | Adds complexity for 12 commands. Current `switch` is fine for now |
| **tview/tcell** | Too low-level; Charm ecosystem covers sdf's needs at a higher level |

---

## Part 3: UI Design Plan

### Design Goals

1. **Consistent visual language** — every command uses the same color scheme, spacing, icons
2. **Progressive disclosure** — show summary first, details on demand
3. **Non-destructive confidence** — make it obvious what will happen before it does
4. **Streaming-friendly** — sync and Claude operations need live-updating output

### Color Palette & Conventions

```
Success/merged:  Green    ✓
Active/current:  Cyan     →
Warning/drift:   Yellow   ⚠
Error/failed:    Red      ✗
Muted/info:      Gray     ●
PR numbers:      Magenta  #42
Branch names:    Bold     feature/auth
```

### Command-by-Command UI Redesign

#### `sdf status`

Current:
```
  my-stack  (base: main)

   ● branch-a  PR #16  open     2 commits ahead, in sync
 → ● branch-b          pending
```

Proposed (using lipgloss + bubbles table):
```
╭─ my-stack ──────────────────────────────────────────╮
│  base: main                                         │
├─────────────────────────────────────────────────────┤
│  ✓  branch-a    PR #16  merged                      │
│  ●  branch-b    PR #17  open    2 ahead · in sync   │
│→ ●  branch-c            —       no PR yet           │
╰─────────────────────────────────────────────────────╯
  run `sdf sync` to rebase branch-b
```

#### `sdf sync`

Current: Raw printf with manual ANSI cursor movement.

Proposed (using bubbles spinner + lipgloss):
```
Syncing my-stack...

  ✓ Fetched from origin
  ✓ branch-a is merged
  ⏳ Rebasing branch-b onto main...
    ✓ Rebased
    ⏳ Pushing branch-b...

  Updating PR content:
    PR #17  ⏳ generating title...     (spinner)
    PR #18  ✓ feat: add auth flow

Proceed? [Y/n]
```

Replace `parallelDisplay` raw ANSI with a proper bubbles-based multi-line progress component.

#### `sdf pr`

Proposed:
```
  ⏳ Pushing branch-c to origin...    (spinner)
  ⏳ Creating PR...                   (spinner)

  ✓ Created PR #19: feat: add user settings
    https://github.com/user/repo/pull/19

  Updating stack navigation...  ✓
```

#### Conflict Resolution (`sdf sync` — conflict prompt)

Current: Raw text menu.

Proposed (using huh):
```
  ⚠ Conflict in branch-b — 2 file(s):
    src/auth.go
    src/config.go

  How would you like to resolve?
  > Ask Claude to resolve
    Fix manually (pauses sync)
    Skip this branch
    Abort sync
```

Use `huh.NewSelect()` for a proper interactive selector with arrow-key navigation.

#### `sdf switch`

Current: Bare branch checkout.

Proposed (using huh):
```
  Switch to:
  > branch-a    PR #16  merged
    branch-b    PR #17  open
    branch-c    (no PR)
```

Interactive branch picker when no argument given.

#### `sdf doctor`

Proposed (using lipgloss):
```
╭─ sdf doctor ────────────────────────────────────────╮
│  ✓  git      2.43.0    /usr/bin/git                 │
│  ✓  gh       2.45.0    /usr/bin/gh                  │
│  ●  claude   not found (optional)                   │
╰─────────────────────────────────────────────────────╯
  All required dependencies available.
```

### Architecture

#### New package: `internal/ui/`

```
internal/ui/
  styles.go      — lipgloss style definitions (colors, borders, spacing)
  status.go      — status table renderer
  spinner.go     — spinner wrapper for async operations
  prompt.go      — huh-based prompts (confirm, select, text input)
  progress.go    — multi-line progress display (replaces parallelDisplay)
  markdown.go    — glamour-based markdown rendering
```

This keeps all UI concerns in one place and lets `cmd/` files focus on logic.

### Migration Strategy

**Phase 1: Foundation** — Add lipgloss, define shared styles, apply to `status` and `doctor`
**Phase 2: Prompts** — Replace raw stdin prompts with huh (conflict resolution, confirm sync, switch picker)
**Phase 3: Progress** — Replace `parallelDisplay` with bubbles-based spinner/progress
**Phase 4: Polish** — Add glamour for markdown, refine spacing and borders across all commands

Each phase is independently shippable and improves the UX incrementally.

### Dependency Impact

Adding the Charm libraries introduces external dependencies for the first time. The trade-off:

- **Cost:** ~4 new direct dependencies (lipgloss, bubbles, huh, glamour), each bringing transitive deps
- **Benefit:** Eliminates ~100 lines of fragile ANSI cursor code, provides consistent cross-platform terminal support, enables features that would be very hard to build from scratch (interactive selectors, spinners, styled tables)

The Charm ecosystem is MIT-licensed, well-tested, and widely adopted (used by GitHub CLI, etc.).
