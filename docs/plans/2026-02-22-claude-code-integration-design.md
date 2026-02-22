# Teaching Claude How to Use SDF

**Date:** 2026-02-22
**Status:** Design (revised)

## Problem

When a developer uses Claude Code in a repository that uses SDF, Claude doesn't know SDF exists. It will happily run `git checkout -b feature`, `git rebase --onto`, or `gh pr create` directly — bypassing the stack topology, breaking `.sdf/stacks/*.json` state, and losing context continuity. The developer has to manually instruct Claude to use SDF commands every time.

## Core Insight: Let Claude Teach Itself

SDF shouldn't try to generate Claude Code integration files (`.claude/rules/`, `.claude/skills/`, `.claude/settings.json`) directly. That approach requires SDF to track Claude Code's evolving file formats, YAML frontmatter schemas, and settings structure — and get it wrong every time Claude Code ships a breaking change.

Instead: **SDF spawns Claude as a child process, tells it about SDF, and asks it to create a skill for itself.** Claude knows its own skill format. SDF knows what SDF does. Each side does what it's good at.

## Design

### The `sdf ai` Command Family

```
sdf ai intro           Spawn Claude and ask it to save a skill teaching itself about SDF
sdf ai setup           Spawn Claude and ask it to set up full integration (skill + hooks + rules)
```

That's it. Two commands.

There are no AI wrappers around existing commands. Claude can run `sdf ls` and `sdf status` directly — the intro prompt teaches it which commands to run and when.

These commands spawn `claude` as a child process, stream its output to the terminal so the developer can watch it work, and let it create files using its own tools.

---

### `sdf ai intro` — The Self-Teaching Command

```
$ sdf ai intro
```

What happens:

1. SDF builds a prompt (static text: what SDF is, the rules, command reference, "when to run what")
2. SDF spawns `claude -p` with `--allowedTools` for file writes, streaming output
3. Claude's response streams to the terminal — the developer sees it thinking and working
4. Claude creates `.claude/skills/sdf/SKILL.md` using its Write tool
5. Done. Future Claude Code sessions in this repo automatically load the skill.

#### The prompt

The prompt is fully static — no dynamic stack state embedded. It teaches Claude to run `sdf ls` and `sdf status` itself.

```
You are being asked to set up a Claude Code skill so that you remember how
to use SDF (Stacked Diffs Flow) in future sessions in this repository.

## What SDF Does

SDF manages stacked PRs — chains of dependent pull requests where each PR
builds on the previous one. It handles branch topology, cascade rebasing when
PRs merge, and keeps PR bases correct on GitHub.

## Rules — IMPORTANT

When working in a repository that uses SDF, follow these rules for any branch
that is part of an SDF stack:

- Use `sdf branch <name>` instead of `git checkout -b` — it registers the
  branch in the stack and sets the correct base
- Use `sdf sync` instead of `git rebase` — it cascade-rebases the entire
  stack and updates PR bases
- Use `sdf pr` instead of `gh pr create` — it sets the correct PR base
  branch and adds navigation links
- Use `sdf merge` instead of `gh pr merge` — it retargets the next PR and
  syncs the remaining stack
- After amending an earlier branch, run `sdf sync` to cascade changes forward

## Commands

| Command | Purpose |
|---------|---------|
| `sdf init <name> [--branch <name>]` | Create a new stack with its first branch |
| `sdf branch <name>` | Add a branch after the current position in the stack |
| `sdf ls` | List all tracked stacks with a short summary of each |
| `sdf status [<stack>]` | Show detailed stack topology, PR states, sync status |
| `sdf sync [--with-content]` | Cascade-rebase after merges or amendments |
| `sdf pr [--title "..."]` | Create a GitHub PR with correct base and links |
| `sdf merge [-y] [--method squash|merge|rebase]` | Merge head PR and sync |
| `sdf switch [<branch>]` | Checkout a branch and show its stack context |
| `sdf move <commit>...` | Move commits from current branch to parent |
| `sdf fetch` | Discover existing PR chains from GitHub |
| `sdf doctor` | Check that git, gh, and claude are available |

## When to Run What

- **Starting a session:** Run `sdf ls` to see all tracked stacks at a glance,
  then `sdf status` on the relevant stack for details.
- **Before creating a branch:** Run `sdf status` to confirm you're on the
  right branch (new branch inserts after current position).
- **After pushing changes to an earlier branch:** Run `sdf sync` to cascade.
- **After a PR is merged on GitHub:** Run `sdf sync` to rebase remaining
  branches onto the new base.

## Your Task

Create a Claude Code skill file at `.claude/skills/sdf/SKILL.md` that
captures all of the above — rules, commands, and "when to run what" — so
you remember how to work with SDF in every future session in this repo.
Use whatever skill format and frontmatter Claude Code currently expects.
```

#### Streaming UX

The developer sees Claude working in real-time:

```
$ sdf ai intro

  Setting up SDF skill for Claude Code...

  I'll create a skill file that teaches me about SDF for future sessions.

  Creating .claude/skills/sdf/SKILL.md...

  ✓ Skill created. Claude Code will load SDF knowledge automatically
    in future sessions.
```

SDF uses `RunPromptStreaming` (already in `internal/claude/claude.go`) to stream Claude's text tokens to stdout. The existing stream-json parser handles incremental display.

#### What needs to change in `RunPromptStreaming`

The current `RunPromptStreaming` invokes `claude -p` without tool permissions. For `sdf ai intro`, Claude needs to write files. Two options:

**Option A: Extend `RunPromptStreaming` with an options struct**

```go
type PromptOptions struct {
    AllowedTools []string // e.g. ["Write", "Read", "Bash(mkdir *)"]
}

func RunPromptStreamingWithOpts(name, prompt string, display io.Writer, opts PromptOptions) (string, error) {
    args := []string{"-p", "--verbose",
        "--output-format", "stream-json",
        "--include-partial-messages",
    }
    for _, tool := range opts.AllowedTools {
        args = append(args, "--allowedTools", tool)
    }
    args = append(args, prompt)
    cmd := exec.Command("claude", args...)
    // ... rest same as current RunPromptStreaming
}
```

**Option B: New function specific to `ai intro`**

Keep `RunPromptStreaming` unchanged. Add a new `RunWithTools` that builds the right invocation for the ai commands.

**Recommendation: Option A.** Small, backward-compatible change. The existing callers pass an empty `PromptOptions{}` and get current behavior.

#### Handling tool_use events in the stream

When Claude uses its Write tool to create the skill file, the stream-json output includes `tool_use` events alongside `assistant` text events. The current parser ignores non-text events. We should display them:

```go
// In the event parsing loop:
if event.Type == "tool_use" {
    // Show: "  Writing .claude/skills/sdf/SKILL.md..."
    fmt.Fprintf(display, "  Writing %s...\n", event.ToolInput.FilePath)
}
```

This gives the developer visibility into what Claude is doing without dumping raw JSON.

---

### `sdf ai setup` — Full Integration

```
$ sdf ai setup
```

Same mechanism as `intro`, but the prompt additionally asks Claude to:

1. **Create a skill** (same as `intro`)
2. **Create `.claude/rules/sdf.md`** — rules load every session without needing the skill to fire
3. **Set up a SessionStart hook** in `.claude/settings.json` that runs `sdf ls` at session start
4. **Set up a PreToolUse hook** that guards against raw git/gh commands on stack branches

The prompt tells Claude what behavior each hook should have. Claude decides the exact JSON format for `.claude/settings.json`.

#### The extra section in the setup prompt

```
## Additional Setup (beyond the skill)

### Rules File

Also create `.claude/rules/sdf.md` containing the rules and command reference
from above. This ensures the rules load automatically every session even
if the skill doesn't fire.

### Hooks

Set up the following hooks in `.claude/settings.json`:

1. A SessionStart hook that runs:
   `if command -v sdf >/dev/null 2>&1 && [ -d .sdf ]; then sdf ls 2>/dev/null; fi`
   This shows tracked stacks at the start of every session.

2. A PreToolUse hook on Bash commands that checks whether the command
   is `git checkout -b`, `git rebase`, `gh pr create`, or `gh pr merge`
   while the current branch is part of an SDF stack. If so, block it
   with a message suggesting the SDF equivalent. Only block when the
   branch IS in a stack — allow these commands for non-stack branches.

If `.claude/settings.json` already exists, merge the hooks with existing
configuration — don't overwrite other hooks.
```

---

## Implementation

### `cmd/ai.go`

```go
var aiCmd = &cobra.Command{
    Use:   "ai",
    Short: "AI assistant integration commands",
}

var aiIntroCmd = &cobra.Command{
    Use:   "intro",
    Short: "Teach Claude about SDF and save a skill for future sessions",
    Long:  `Spawns Claude as a child process, introduces SDF (rules, commands,
workflows), and asks Claude to create a skill file so it remembers how
to use SDF in future sessions. Claude's output streams to the terminal.`,
    RunE: runAIIntro,
}

var aiSetupCmd = &cobra.Command{
    Use:   "setup",
    Short: "Full Claude Code integration — skill, rules, and hooks",
    Long:  `Like intro, but also asks Claude to create a rules file and
configure session hooks for automatic SDF awareness.`,
    RunE: runAISetup,
}

func init() {
    rootCmd.AddCommand(aiCmd)
    aiCmd.AddCommand(aiIntroCmd)
    aiCmd.AddCommand(aiSetupCmd)
}
```

### `internal/ai/prompt.go`

Pure string templates. No dynamic state.

```go
func BuildIntroPrompt() string { /* static template */ }
func BuildSetupPrompt() string { /* intro + hooks/rules instructions */ }
```

### `internal/claude/claude.go`

Extend `RunPromptStreaming` (or add a new variant) to accept `--allowedTools`:

```go
type PromptOptions struct {
    AllowedTools []string
}

func RunPromptStreamingWithOpts(name, prompt string, display io.Writer, opts PromptOptions) (string, error)
```

Optionally enhance the stream parser to display `tool_use` events (file writes) so the developer sees what Claude is creating.

### `runAIIntro` implementation sketch

```go
func runAIIntro(cmd *cobra.Command, args []string) error {
    if !claude.Available() {
        return fmt.Errorf("claude CLI is not installed (run sdf doctor)")
    }

    prompt := ai.BuildIntroPrompt()
    opts := claude.PromptOptions{
        AllowedTools: []string{"Write", "Read", "Bash(mkdir *)"},
    }

    fmt.Println(ui.Cyan.Render("Setting up SDF skill for Claude Code..."))
    fmt.Println()

    _, err := claude.RunPromptStreamingWithOpts("ai-intro", prompt, os.Stdout, opts)
    if err != nil {
        return fmt.Errorf("claude failed: %w", err)
    }

    fmt.Println()
    fmt.Printf("  %s Skill created.\n", ui.SymOK)
    return nil
}
```

---

## Trade-offs

### What we gain

- **Zero coupling to Claude Code internals.** SDF doesn't need to know SKILL.md format, settings.json schema, or hook configuration syntax. Claude knows its own formats.
- **Trivial implementation.** A prompt template + one new `--allowedTools` parameter on the existing streaming function.
- **Great UX.** One command, no copy-paste, streamed output so the developer sees progress.
- **No stale state.** The prompt teaches Claude to run `sdf ls` and `sdf status` itself rather than embedding a snapshot.
- **Maintainable.** When SDF adds a new command, update the prompt template string. No format migration.

### What we lose

- **Requires `claude` CLI.** Unlike a print-to-stdout approach, this only works with Claude. But SDF already depends on Claude for conflict resolution — this is consistent.
- **Non-deterministic output.** Claude might format the skill file differently each time. For teams wanting exact files, the export fallback exists.
- **Network dependency.** Spawning Claude requires API access. A developer offline can't run `sdf ai intro`. Mitigation: it's a one-time setup, not part of the daily workflow.

---

## Optional: Deterministic Fallback

For teams who want committed, deterministic integration files without invoking Claude:

```
sdf ai export-rules      # Print a .claude/rules/sdf.md to stdout
sdf ai export-skill      # Print a .claude/skills/sdf/SKILL.md to stdout
```

These print files the developer can redirect and commit. This is the only path where SDF needs to know Claude Code file formats, and it's opt-in.

---

## Optional: MCP Server (Future)

An MCP server remains a valid future enhancement, orthogonal to this approach. When implemented, `sdf ai setup` can include an instruction for Claude to configure `.mcp.json`.

---

## Implementation Plan

### Phase 1: `sdf ai intro`

1. Create `internal/ai/prompt.go` with `BuildIntroPrompt()`
2. Extend `internal/claude/claude.go` — add `PromptOptions` with `AllowedTools` field
3. Create `cmd/ai.go` with `ai intro` subcommand
4. Test: verify Claude is spawned with the right flags, prompt content is correct

### Phase 2: `sdf ai setup`

1. Add `BuildSetupPrompt()` to `internal/ai/prompt.go`
2. Add `ai setup` subcommand to `cmd/ai.go`
3. Test: verify setup prompt includes hook and rules instructions

### Phase 3: Stream UX polish (Optional)

1. Parse `tool_use` events in the stream to display "Writing <file>..." messages
2. Add a completion summary ("Skill created at .claude/skills/sdf/SKILL.md")
3. Handle error cases gracefully (Claude unavailable, API failure, partial writes)

### Phase 4: Hook subcommands (Optional)

1. Implement `sdf hook guard-git` for the PreToolUse hook that `sdf ai setup` configures
2. Hidden subcommand, called by the hook, not by the user

---

## Open Questions

1. **Naming: `sdf ai` vs `sdf claude` vs `sdf assist`?** `ai` is generic (works with any assistant). `claude` is specific (SDF already has a Claude dependency). Recommendation: `sdf ai` — keeps the door open, and the commands already require `claude` CLI anyway.

2. **What `--allowedTools` should `sdf ai intro` grant?** Minimum: `Write` (to create the skill file). Probably also `Read` (to check existing files before writing) and `Bash(mkdir *)` (to create the `.claude/skills/sdf/` directory). Should NOT grant broad Bash access.

3. **Should `sdf ai intro` be idempotent?** If a skill already exists, should Claude overwrite it, skip it, or merge? Recommendation: let Claude decide — the prompt can say "create or update the skill file."
