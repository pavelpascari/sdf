# Teaching Claude How to Use SDF

**Date:** 2026-02-22
**Status:** Design (revised)

## Problem

When a developer uses Claude Code in a repository that uses SDF, Claude doesn't know SDF exists. It will happily run `git checkout -b feature`, `git rebase --onto`, or `gh pr create` directly — bypassing the stack topology, breaking `.sdf/stacks/*.json` state, and losing context continuity. The developer has to manually instruct Claude to use SDF commands every time.

## Core Insight: Let Claude Teach Itself

SDF shouldn't try to generate Claude Code integration files (`.claude/rules/`, `.claude/skills/`, `.claude/settings.json`) directly. That approach requires SDF to track Claude Code's evolving file formats, YAML frontmatter schemas, and settings structure — and get it wrong every time Claude Code ships a breaking change.

Instead: **SDF prints a prompt. Claude reads the prompt. Claude creates its own files.**

Claude knows its own skill format, its own rules format, its own settings schema. SDF knows what SDF does. Each side does what it's good at.

## Design

### The `sdf ai` Command Family

```
sdf ai intro           Print a prompt that introduces SDF and asks Claude to save a skill
sdf ai setup           Print a prompt that asks Claude to set up full integration
sdf ai context         Print current stack context in a Claude-friendly format
sdf ai status          Print stack state as a prompt Claude can reason about
```

These commands don't write any files. They print text to stdout. The developer pastes the output into Claude Code (or pipes it), and Claude takes action.

---

### `sdf ai intro` — The Self-Teaching Command

The simplest and most important command. A developer in a Claude Code session runs:

```
sdf ai intro
```

SDF prints a carefully crafted prompt to stdout. The developer pastes it into Claude Code. Claude reads it, understands SDF, and — because the prompt asks it to — uses its Write tool to create a skill file so it remembers SDF in future sessions.

#### What the prompt contains

1. **What SDF is** — one paragraph
2. **The rules** — what to do and what NOT to do (use `sdf branch` not `git checkout -b`, etc.)
3. **Command reference** — every command with flags and purpose
4. **Current state** — all stacks in this repo, their branches, PR numbers, sync status
5. **The ask** — "Save a Claude Code skill for yourself so you remember this in future sessions. Include the rules, the command reference, and the workflow."

#### Example output

```
I'm going to teach you about a tool called SDF (Stacked Diffs Flow) that is
installed in this repository.

## What SDF Does

SDF manages stacked PRs — chains of dependent pull requests where each PR
builds on the previous one. It handles branch topology, cascade rebasing when
PRs merge, and keeps PR bases correct on GitHub.

## Rules — IMPORTANT

When working in this repository, follow these rules for any branch that is
part of an SDF stack:

- Use `sdf branch <name>` instead of `git checkout -b` — it registers the
  branch in the stack and sets the correct base
- Use `sdf sync` instead of `git rebase` — it cascade-rebases the entire
  stack and updates PR bases
- Use `sdf pr` instead of `gh pr create` — it sets the correct PR base
  branch and adds navigation links
- Use `sdf merge` instead of `gh pr merge` — it retargets the next PR and
  syncs the remaining stack
- Run `sdf status` before starting work to understand the current state
- After amending an earlier branch, run `sdf sync` to cascade changes forward

## Commands

| Command | Purpose |
|---------|---------|
| `sdf init <name> [--branch <name>]` | Create a new stack with its first branch |
| `sdf branch <name>` | Add a branch after the current position in the stack |
| `sdf status [<stack>]` | Show stack topology, PR states, sync status |
| `sdf sync [--with-content]` | Cascade-rebase after merges or amendments |
| `sdf pr [--title "..."]` | Create a GitHub PR with correct base and links |
| `sdf merge [-y] [--method squash|merge|rebase]` | Merge head PR and sync |
| `sdf switch [<branch>]` | Checkout a branch and show its stack context |
| `sdf move <commit>...` | Move commits from current branch to parent |
| `sdf fetch` | Discover existing PR chains from GitHub |
| `sdf doctor` | Check that git, gh, and claude are available |

## Current State

Stack: auth-feature (base: main)
  ✓  auth/db-schema     PR #142  merged
  ●  auth/session-api   PR #143  open     in sync
  ●  auth/ui-login      PR #144  open     needs sync

Current branch: auth/session-api

## What To Do Now

Please save a Claude Code skill for yourself that captures the rules above
and the command reference so you remember how to work with SDF in future
sessions in this repository. The skill should be saved to
`.claude/skills/sdf/SKILL.md` using whatever format Claude Code skills
currently use. Include:
1. The rules (what to use, what to avoid)
2. The command reference table
3. A note to run `sdf status` at the start of each session
```

#### Why this works

- **Claude writes the skill file** — it knows the current SKILL.md format with YAML frontmatter, allowed-tools, etc. better than SDF ever will
- **Dynamic state is included** — Claude sees the actual stacks, not just a reference
- **Zero maintenance** — if Claude Code changes its skill format, SDF doesn't need an update
- **One command** — no setup wizard, no flags, no config. Just paste.
- **Discoverable** — a developer who types `sdf ai` sees what's available

---

### `sdf ai setup` — Full Integration Prompt

A more comprehensive version that asks Claude to set up everything:

```
sdf ai setup
```

Prints a prompt that asks Claude to:

1. **Create a skill** (same as `sdf ai intro`)
2. **Create a `.claude/rules/sdf.md`** file with the rules and command reference — this persists across sessions without needing the skill
3. **Set up a SessionStart hook** that runs `sdf status` at the start of each session, so Claude always sees the stack state
4. **Set up a PreToolUse hook** that guards against raw git/gh commands on stack branches

The prompt includes enough context for Claude to do all of this correctly, but Claude makes the formatting decisions.

#### Example (the hook setup section)

```
## Hook Setup

Please also set up the following hooks in `.claude/settings.json`:

1. A SessionStart hook that runs:
   `if command -v sdf >/dev/null 2>&1 && [ -d .sdf ]; then sdf status 2>/dev/null; fi`
   This injects the current stack state into every session.

2. A PreToolUse hook on Bash commands that checks whether the command
   is `git checkout -b`, `git rebase`, `gh pr create`, or `gh pr merge`
   while the current branch is part of an SDF stack. If so, block it
   with a message suggesting the SDF equivalent. Be careful not to
   block these commands when the branch is NOT in a stack.

Merge these hooks with any existing hooks in the settings file —
don't overwrite other hooks that might already be configured.
```

---

### `sdf ai context` — Stack Context for Claude

Prints the assembled context for the current branch's stack. This is the same information that `sdf context show` would produce (from the DESIGN.md vision), formatted as a prompt:

```
sdf ai context
```

Useful when a developer wants to bring Claude up to speed mid-session:

```
Here is the full context for the stack I'm working on:
[output of sdf ai context]
```

This reads all `.sdf/stacks/*.json` files and, if context docs exist (`.sdf/context/*.md`), assembles them into a unified view.

---

### `sdf ai status` — Machine-Readable Stack State

```
sdf ai status
```

Prints `sdf status` output wrapped in a Claude-friendly frame:

```
This repository uses SDF for stacked PRs. Here is the current state:

[sdf status output]

Based on this state, the next recommended action is: [suggestion]
```

Lighter than `intro` — for when Claude already has the skill but needs a state refresh.

---

## Implementation

### What SDF Needs to Build

The implementation is remarkably simple because SDF doesn't generate any Claude Code files — it just prints text.

#### 1. `cmd/ai.go` — The `sdf ai` command group

```go
var aiCmd = &cobra.Command{
    Use:   "ai",
    Short: "AI assistant integration commands",
}

var aiIntroCmd = &cobra.Command{
    Use:   "intro",
    Short: "Print a prompt that introduces SDF to your AI assistant",
    Long:  `Outputs a prompt that teaches Claude (or any AI assistant) about SDF,
including the command reference, rules, and current stack state. Paste the
output into your AI assistant session. If using Claude Code, the prompt asks
Claude to save a skill so it remembers SDF in future sessions.`,
    RunE: runAIIntro,
}

var aiSetupCmd = &cobra.Command{
    Use:   "intro",
    Short: "Print a prompt that asks Claude to set up full SDF integration",
    RunE: runAISetup,
}

var aiContextCmd = &cobra.Command{
    Use:   "context",
    Short: "Print assembled stack context for the current branch",
    RunE: runAIContext,
}

var aiStatusCmd = &cobra.Command{
    Use:   "status",
    Short: "Print current stack state as a prompt",
    RunE: runAIStatus,
}
```

#### 2. `internal/ai/prompt.go` — Prompt builders

Each function builds a prompt string from:
- A static template (the rules, command reference, workflow)
- Dynamic state (loaded from `.sdf/stacks/*.json` via existing `internal/stack` package)
- The current git branch (via existing `internal/git` package)

```go
// BuildIntroPrompt assembles the full introduction prompt.
func BuildIntroPrompt(root string) (string, error) {
    // 1. Load all stacks
    // 2. Get current branch
    // 3. Render static template + dynamic state
    // 4. Append the "save a skill" instruction
    return prompt, nil
}
```

#### 3. That's it

No `.claude/` file generation. No YAML frontmatter templates. No settings.json merging. No MCP protocol implementation.

The entire feature is ~200 lines of Go: a command group and a prompt builder.

### What Claude Does (When the Developer Pastes the Prompt)

Claude reads the prompt and:

1. **Understands SDF** — the rules and commands are now in context
2. **Creates a skill file** — writes `.claude/skills/sdf/SKILL.md` with proper frontmatter
3. **(If `sdf ai setup`)** Creates `.claude/rules/sdf.md` and `.claude/settings.json` with hooks
4. **Immediately applies the knowledge** — starts using `sdf` commands in the current session

In future sessions:
- The skill file loads automatically → Claude remembers SDF
- The rules file reinforces the rules → Claude follows them
- The SessionStart hook fires → Claude sees current stack state

---

## Trade-offs

### What we gain

- **Zero coupling to Claude Code internals.** SDF doesn't need to know SKILL.md format, settings.json schema, or hook configuration syntax. Claude knows its own formats.
- **Trivial implementation.** ~200 lines of prompt-building Go code vs. a full file generation system with format tracking, merging, and idempotency.
- **Works with any AI assistant.** The prompt output is plain text. It works with Claude Code, but also with any other AI tool that accepts text input.
- **Always current.** The prompt includes live stack state from `.sdf/stacks/*.json`.
- **Maintainable.** When SDF adds a new command, updating the prompt template is a one-line change. No format migration needed.

### What we lose

- **Not fully automatic.** The developer has to paste the prompt (once). With the file-generation approach, `sdf init` could create everything silently.
- **Non-deterministic output.** Claude might format the skill file differently each time. Teams who want exact, committed integration files won't get byte-identical results.
- **Depends on Claude being smart.** If Claude misunderstands the prompt and creates a broken skill file, the developer has to notice and fix it. The file-generation approach produces known-good files.

### Mitigations

- **The paste is a one-time action.** After Claude creates the skill, it persists across sessions. `sdf ai intro` is run once per repo, not once per session.
- **`sdf ai setup`** can include validation: "After creating the files, verify them by running `sdf doctor` and checking that the skill loads correctly."
- **For teams wanting deterministic files:** SDF can also provide a `sdf ai export-skill` that prints a ready-made SKILL.md to stdout (no AI involved). The team commits it directly. This is a fallback, not the primary path.

---

## Optional: Deterministic Fallback

For teams who want committed, deterministic integration files without going through Claude:

```
sdf ai export-rules      # Print a .claude/rules/sdf.md to stdout
sdf ai export-skill      # Print a .claude/skills/sdf/SKILL.md to stdout
```

These print files that the developer can redirect into place:

```bash
mkdir -p .claude/rules .claude/skills/sdf
sdf ai export-rules > .claude/rules/sdf.md
sdf ai export-skill > .claude/skills/sdf/SKILL.md
git add .claude/
git commit -m "add SDF integration for Claude Code"
```

This is the "escape hatch" for teams who want version-controlled, reproducible integration without AI-generated files.

---

## Optional: MCP Server (Future)

The MCP server from the original design remains a valid future enhancement. It's orthogonal to the `sdf ai` approach — the prompt teaches Claude about SDF, the MCP server gives Claude structured tools. Both can coexist.

When/if implemented, `sdf ai setup` can include an instruction for Claude to configure the MCP server in `.mcp.json`.

---

## Optional: Hooks (Future)

Hooks (SessionStart, PreToolUse guard rails) remain valuable additions. They can be set up either:
- By Claude, when the developer runs `sdf ai setup` (Claude creates `.claude/settings.json`)
- By SDF, via `sdf ai export-hooks > .claude/settings.json`

The hook subcommands (`sdf hook guard-git`, `sdf hook post-update`) would still be implemented in SDF — they're called by the hooks, not by Claude.

---

## Implementation Plan

### Phase 1: `sdf ai intro` + `sdf ai status`

1. Create `cmd/ai.go` with the command group
2. Create `internal/ai/prompt.go` with `BuildIntroPrompt` and `BuildStatusPrompt`
3. `BuildIntroPrompt` loads stacks, current branch, renders template
4. Test: verify prompt includes command reference, rules, and dynamic state

### Phase 2: `sdf ai setup` + `sdf ai context`

1. Extend `internal/ai/prompt.go` with `BuildSetupPrompt` (adds hook/rules setup instructions)
2. Implement `BuildContextPrompt` (assembles stack context docs if they exist)
3. Test: verify setup prompt includes hook configuration instructions

### Phase 3: `sdf ai export-*` (Deterministic Fallback)

1. Add `sdf ai export-rules` and `sdf ai export-skill`
2. These produce static files (SDF does need to know the format here, but it's opt-in)
3. Test: verify exported files are valid

### Phase 4: Hook Subcommands

1. Implement `sdf hook guard-git` (reads stdin JSON, checks stack membership, exits 0 or 2)
2. Implement `sdf hook post-update` (regenerates dynamic state in rules file)
3. These are called by hooks that Claude (or the export) sets up

---

## Open Questions

1. **Should `sdf ai intro` output be copied to clipboard automatically?** On macOS (`pbcopy`), Linux (`xclip`), etc. Pro: one less step. Con: platform-specific, might overwrite clipboard.

2. **Should there be a `--pipe` mode?** e.g., `sdf ai intro | claude -p` to send the prompt directly to Claude without pasting. This works for the `intro` case but not for `setup` (which needs Claude to create files, requiring interactive mode).

3. **Should `sdf ai intro` detect if Claude Code is running and offer to inject directly?** Claude Code has no such API today, but it's worth noting as a future possibility.

4. **Naming: `sdf ai` vs `sdf claude` vs `sdf assist`?** `ai` is generic (works with any assistant). `claude` is specific (SDF already has a Claude dependency). Recommendation: `sdf ai` — keeps the door open for other assistants while being descriptive.
