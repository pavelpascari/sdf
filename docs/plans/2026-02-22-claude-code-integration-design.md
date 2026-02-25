# Teaching Claude How to Use SDF

**Date:** 2026-02-22
**Updated:** 2026-02-25
**Status:** Implemented

## Problem

When a developer uses Claude Code in a repository that uses SDF, Claude doesn't know SDF exists. It will happily run `git checkout -b feature`, `git rebase --onto`, or `gh pr create` directly — bypassing the stack topology, breaking `.sdf/stacks/*.json` state, and losing context continuity. The developer has to manually instruct Claude to use SDF commands every time.

## Core Insight: Let Claude Teach Itself

SDF shouldn't try to generate Claude Code integration files (`.claude/rules/`, `.claude/skills/`, `.claude/settings.json`) directly. That approach requires SDF to track Claude Code's evolving file formats, YAML frontmatter schemas, and settings structure — and get it wrong every time Claude Code ships a breaking change.

Instead: **SDF spawns Claude as a child process, tells it about SDF, and asks it to create a skill for itself.** Claude knows its own skill format. SDF knows what SDF does. Each side does what it's good at.

## Design

### The `sdf ai` Command

```
sdf ai intro           Spawn Claude and ask it to save a skill teaching itself about SDF
```

That's it. One command.

There are no AI wrappers around existing commands. Claude can run `sdf ls` and `sdf status` directly — the intro prompt teaches it which commands to run and when.

The command spawns `claude` as a child process, streams its output to the terminal so the developer can watch it work, and lets it create files using its own tools.

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

### Dropped: `sdf ai setup`

Originally planned as a superset of `intro` that also created a `.claude/rules/sdf.md` file (redundant with the skill), a SessionStart hook (`sdf ls`), and a PreToolUse hook blocking `git checkout -b`/`git rebase`/`gh pr create`/`gh pr merge` on stack branches.

**Why it was dropped:**

- **Rules file is redundant.** The skill already teaches Claude the same rules and commands. If the skill description is good, it fires whenever it matters.
- **PreToolUse guard is too restrictive.** We don't want to block Claude from using git. Clear instructions explaining *why* to use SDF commands are better than guard rails that prevent git usage. Claude should understand the intent, not be fenced in.
- **SessionStart hook is unnecessary.** The skill's "when to run what" section already teaches Claude to run `sdf ls` at session start.

With the restrictive hook dropped and the other pieces redundant with the skill, `setup` added no value over `intro`.

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

func init() {
    rootCmd.AddCommand(aiCmd)
    aiCmd.AddCommand(aiIntroCmd)
}
```

### `internal/ai/prompt.go`

Pure string templates. No dynamic state.

```go
func BuildIntroPrompt() string { /* static template */ }
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

- **Zero coupling to Claude Code internals.** SDF doesn't need to know SKILL.md format or settings.json schema. Claude knows its own formats.
- **Trivial implementation.** A prompt template + one new `--allowedTools` parameter on the existing streaming function.
- **Great UX.** One command, no copy-paste, streamed output so the developer sees progress.
- **No stale state.** The prompt teaches Claude to run `sdf ls` and `sdf status` itself rather than embedding a snapshot.
- **Maintainable.** When SDF adds a new command, update the prompt template string. No format migration.

### What we lose

- **Requires `claude` CLI.** Unlike a print-to-stdout approach, this only works with Claude. But SDF already depends on Claude for conflict resolution — this is consistent.
- **Non-deterministic output.** Claude might format the skill file differently each time.
- **Network dependency.** Spawning Claude requires API access. A developer offline can't run `sdf ai intro`. Mitigation: it's a one-time setup, not part of the daily workflow.

---

## Future: MCP Server

An MCP server remains a valid future enhancement, orthogonal to this approach. When implemented, `sdf ai intro` could include an instruction for Claude to configure `.mcp.json`.

---

## Implementation Status

### Done

1. `internal/ai/prompt.go` — `BuildIntroPrompt()` with static prompt template
2. `internal/claude/claude.go` — `PromptOptions` struct, `RunPromptStreamingWithOpts` with `--allowedTools`
3. `cmd/ai.go` — `ai intro` subcommand
4. Stream parser displays `tool_use` events (`[Using tool: <name>]`)
5. Unit tests (`cmd/ai_test.go`, `internal/ai/prompt_test.go`, `internal/claude/claude_test.go`)
6. E2E tests (`e2e/ai_test.go`, gated behind `-with-claude`)

### Dropped

- **`sdf ai setup`** — redundant with intro (see "Dropped" section above)
- **Phase 4: Hook subcommands** — PreToolUse guard was too restrictive. Good instructions beat guard rails.
- **Deterministic fallback** (`sdf ai export-rules`/`export-skill`) — not needed yet

---

## Resolved Questions

1. **Naming:** `sdf ai` — generic, keeps the door open even though it requires `claude` CLI.

2. **Allowed tools:** `Write`, `Read`, `Bash(mkdir *)`. Enough to create the skill file and its directory. No broad Bash access.

3. **Idempotency:** Left to Claude — it can overwrite or update the skill file as it sees fit.
