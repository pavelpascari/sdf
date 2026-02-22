# Teaching Claude How to Use SDF

**Date:** 2026-02-22
**Status:** Design

## Problem

When a developer uses Claude Code in a repository that uses SDF, Claude doesn't know SDF exists. It will happily run `git checkout -b feature`, `git rebase --onto`, or `gh pr create` directly — bypassing the stack topology, breaking `.sdf/stacks/*.json` state, and losing context continuity. The developer has to manually instruct Claude to use SDF commands every time, which defeats the purpose of having an AI assistant that understands the workflow.

The question isn't "how do we make Claude smarter" — it's "how do we put SDF's knowledge where Claude already knows to look."

## Design Principles

1. **Meet Claude where it is.** Use Claude Code's existing integration points (CLAUDE.md, skills, hooks, MCP). Don't invent a custom protocol.
2. **Progressive disclosure.** A developer who runs `sdf init` should get Claude integration for free. Advanced integration (MCP server) is opt-in.
3. **Dynamic over static.** Claude should know the current stack state, not just the command reference. A stale CLAUDE.md is worse than none.
4. **Guard rails over instructions.** Where possible, prevent Claude from doing the wrong thing (hooks that intercept raw git) rather than just telling it what to do (CLAUDE.md that says "please use sdf").
5. **SDF owns the integration.** The developer shouldn't have to write CLAUDE.md themselves. SDF generates and maintains it.

## Integration Layers

The design uses four layers, ordered by impact and implementation complexity:

```
Layer 1: CLAUDE.md generation          (static knowledge)
Layer 2: Skills / slash commands        (workflow recipes)
Layer 3: Hooks                          (guard rails + context injection)
Layer 4: MCP server                     (structured tool access)
```

Each layer is independently useful. A team can adopt Layer 1 alone and get value. Layers 2-4 build on top but don't require each other.

---

## Layer 1: CLAUDE.md Generation

### What

`sdf init` and `sdf branch` automatically generate and update a `.claude/rules/sdf.md` file that teaches Claude about SDF. This file is committed alongside the stack metadata.

### Why `.claude/rules/sdf.md` instead of `CLAUDE.md`

- A project may already have a CLAUDE.md with its own conventions. SDF shouldn't overwrite it.
- `.claude/rules/*.md` files are auto-discovered by Claude Code and merged with other instructions.
- Keeps SDF's instructions modular and removable.
- Supports glob-based path filtering if needed later.

### Content Structure

The generated file has two sections: **static reference** (command surface, concepts) and **dynamic state** (current stacks, branch positions).

```markdown
# SDF — Stacked Diffs Flow

This repository uses SDF to manage stacked PRs (chains of dependent pull requests).

## Rules

- NEVER use `git checkout -b` or `git branch` to create feature branches. Use `sdf branch <name>` instead — it registers the branch in the stack, sets up the correct base, and pushes a tracking branch.
- NEVER use `git rebase` directly on stack branches. Use `sdf sync` to cascade-rebase after changes to earlier branches.
- NEVER use `gh pr create` directly. Use `sdf pr` — it sets the correct PR base (the parent branch in the stack, not main) and populates the description.
- NEVER use `gh pr merge` directly. Use `sdf merge` — it handles retargeting the next PR and triggering a cascade sync.
- After amending or force-pushing an earlier branch in a stack, ALWAYS run `sdf sync` to cascade changes forward.
- Before starting work on a stack branch, run `sdf status` to understand the current state.
- When creating a new branch, make sure you're checked out on the branch you want it to follow in the stack. `sdf branch` inserts after the current branch.

## Available Commands

| Command | Purpose |
|---------|---------|
| `sdf init <name> [--branch <name>]` | Create a new stack with its first branch |
| `sdf branch <name>` | Add a branch after the current position in the stack |
| `sdf status [<stack>]` | Show stack topology, PR states, sync status |
| `sdf sync [--with-content]` | Cascade-rebase after merges or amendments |
| `sdf pr [--title "..."]` | Create a GitHub PR with correct base and navigation links |
| `sdf merge [-y] [--method squash\|merge\|rebase]` | Merge head PR and retarget the next |
| `sdf switch [<branch>]` | Checkout a branch and show its stack context |
| `sdf move <commit>...` | Move commits from current branch to parent |
| `sdf fetch` | Discover and register existing PR chains from GitHub |
| `sdf doctor` | Check that git, gh, and claude are available |

## Current Stacks

<!-- sdf:dynamic-state -->
(This section is updated by `sdf init`, `sdf branch`, `sdf sync`, and `sdf merge`.)
<!-- /sdf:dynamic-state -->

## Workflow

1. `sdf init feature-name` — start a stack
2. Implement changes on the first branch
3. `sdf pr` — create the first PR
4. `sdf branch next-layer` — start the next branch in the stack
5. Implement, `sdf pr`, repeat
6. When a PR is reviewed and merged: `sdf sync` cascades changes forward
7. Continue until the stack is fully merged
```

### Dynamic State Section

The `<!-- sdf:dynamic-state -->` block is regenerated whenever SDF mutates stack state. It looks like:

```markdown
### auth-feature (base: main)

| Branch | PR | Status |
|--------|-----|--------|
| auth/db-schema | #142 | merged |
| auth/session-api | #143 | open |
| auth/ui-login | #144 | open |

Current branch: auth/session-api
```

This gives Claude awareness of the stack topology without running any commands.

### Implementation

Add a new internal package `internal/claudeintegration/` (or extend `internal/claude/`) with:

```go
// GenerateRules regenerates .claude/rules/sdf.md from current stack state.
func GenerateRules(root string) error

// UpdateDynamicState updates only the dynamic state section.
func UpdateDynamicState(root string) error
```

Call `UpdateDynamicState` at the end of:
- `sdf init` (stack created)
- `sdf branch` (branch added)
- `sdf sync` (state changed)
- `sdf merge` (PR merged)
- `sdf fetch` (stacks discovered)

Call `GenerateRules` on `sdf init` (first time) and provide `sdf claude-setup` for manual regeneration.

### File Management

- `.claude/rules/sdf.md` is **committed** (same as `.sdf/stacks/*.json`)
- Add `.claude/rules/sdf.md` to the files SDF manages
- `sdf init` creates the `.claude/rules/` directory if it doesn't exist
- If the file already exists, only the dynamic state section is updated (preserving any manual additions outside the markers)

---

## Layer 2: Skills (Custom Slash Commands)

### What

SDF ships a set of `.claude/skills/` that encode common SDF workflows as Claude Code slash commands. These are committed to the repo so every developer on the team gets them.

### Skills

#### `/sdf-status` — Check Stack Health

```
.claude/skills/sdf-status/SKILL.md
```

```yaml
---
name: sdf-status
description: Check current SDF stack state and suggest next actions
allowed-tools: Bash, Read
context: fork
agent: Explore
---

Run `sdf status` and analyze the output. Report:
1. Which stacks exist and their topology
2. Which branches need syncing
3. Whether any PRs have been merged but not synced
4. Suggested next action (e.g., "run sdf sync", "create PR for branch X")

If sdf is not installed, report that and suggest `go install github.com/pavelpascari/sdf@latest`.
```

#### `/sdf-new-stack` — Start a New Feature Stack

```
.claude/skills/sdf-new-stack/SKILL.md
```

```yaml
---
name: sdf-new-stack
description: Initialize a new SDF stack for a feature
allowed-tools: Bash, Read, Edit
---

Help the user create a new SDF stack. Ask for:
1. Stack name (suggest based on the feature description)
2. First branch name (default: stack name)
3. Base branch (default: auto-detected)

Then run `sdf init <name> --branch <branch>` and confirm the result.
Do NOT use `git checkout -b` or `git branch` — always use `sdf init`.
```

#### `/sdf-next-layer` — Add Next Branch to Stack

```
.claude/skills/sdf-next-layer/SKILL.md
```

```yaml
---
name: sdf-next-layer
description: Add a new branch to the current SDF stack
allowed-tools: Bash, Read, Edit
---

Add a new branch to the current stack. Steps:
1. Run `sdf status` to understand current position
2. Confirm the user is on the branch they want the new branch to follow
3. Ask for the branch name
4. Run `sdf branch <name>`

If the user wants the branch at a different position, help them `sdf switch` first.
Do NOT use `git checkout -b` — always use `sdf branch`.
```

#### `/sdf-ship` — Create PR and Prepare for Review

```
.claude/skills/sdf-ship/SKILL.md
```

```yaml
---
name: sdf-ship
description: Create a PR for the current branch with proper SDF metadata
allowed-tools: Bash, Read
---

Prepare the current branch for review:
1. Run `sdf status` to verify the branch is in a stack
2. Check for uncommitted changes and suggest committing them
3. Run `sdf pr` to create the GitHub PR
4. Report the PR URL and its position in the stack

Do NOT use `gh pr create` — always use `sdf pr` so the base branch and navigation links are set correctly.
```

#### `/sdf-sync` — Sync Stacks After Merges

```
.claude/skills/sdf-sync/SKILL.md
```

```yaml
---
name: sdf-sync
description: Cascade-rebase the stack after PR merges or amendments
allowed-tools: Bash, Read, Edit
---

Run `sdf sync` and handle the result:
1. If sync completes cleanly, report what changed
2. If there are conflicts that Claude can resolve, let the sync's built-in resolution handle them
3. If sync fails, diagnose the issue and suggest recovery steps

After sync, run `sdf status` to confirm the final state.
```

### Installation

These skills are generated by `sdf init` (or `sdf claude-setup`) into `.claude/skills/sdf-*/SKILL.md`. They are committed to the repo.

Alternatively, SDF could ship them in a well-known location (`~/.claude/skills/sdf-*/`) installed alongside the binary, making them available in all repos without committing per-repo. The tradeoff: per-repo skills are visible and customizable by the team; user-level skills are always available but less discoverable.

**Recommendation:** Generate per-repo skills in `.claude/skills/` on `sdf init`. This makes the integration visible in code review and lets teams customize behavior.

---

## Layer 3: Hooks

### What

Hooks provide guard rails and automatic context injection. They fire deterministically at specific points in Claude Code's lifecycle, ensuring Claude gets SDF context even when it doesn't think to ask.

### Hook 1: SessionStart — Inject Stack State

**Purpose:** Every time Claude Code starts a session in a repo with SDF, it automatically sees the current stack state.

```json
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "if command -v sdf >/dev/null 2>&1 && [ -d .sdf ]; then sdf status 2>/dev/null; fi"
          }
        ]
      }
    ]
  }
}
```

**Effect:** Claude's session opens with the output of `sdf status` already in context. It knows which stacks exist, which branches need syncing, and what the current branch is — without being asked.

### Hook 2: PreToolUse — Guard Against Raw Git Branch Operations

**Purpose:** Intercept `git checkout -b`, `git branch`, `git rebase`, and `gh pr create` to warn Claude that it should use SDF instead.

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "sdf hook guard-git"
          }
        ]
      }
    ]
  }
}
```

This delegates to a new `sdf hook guard-git` subcommand that:
1. Reads stdin (JSON with `tool_input.command`)
2. Checks if the command matches dangerous patterns:
   - `git checkout -b` → "Use `sdf branch` instead"
   - `git rebase --onto` → "Use `sdf sync` instead"
   - `gh pr create` → "Use `sdf pr` instead"
   - `gh pr merge` → "Use `sdf merge` instead"
3. Only fires if the current branch is part of a known stack (checked via `.sdf/stacks/*.json`)
4. Exit 2 (block) with a helpful message, or exit 0 (allow)

**Critical:** The hook must be fast (<100ms). It reads local JSON files only — no git or gh calls.

### Hook 3: PostToolUse — Auto-Update Dynamic State

**Purpose:** After Claude runs any SDF command, update the dynamic state section in `.claude/rules/sdf.md`.

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "sdf hook post-update"
          }
        ]
      }
    ]
  }
}
```

`sdf hook post-update` checks if the command was an SDF command (by parsing stdin) and if so, regenerates the dynamic state section.

### Installation

Hooks are configured in `.claude/settings.json` (project scope, committed). Generated by `sdf init` or `sdf claude-setup`.

```
.claude/
  settings.json       ← hooks configuration
  rules/
    sdf.md            ← SDF instructions + dynamic state (Layer 1)
  skills/
    sdf-status/       ← slash commands (Layer 2)
    sdf-new-stack/
    sdf-next-layer/
    sdf-ship/
    sdf-sync/
```

### New SDF Subcommands for Hooks

```
sdf hook guard-git      # PreToolUse guard (reads stdin, exits 0 or 2)
sdf hook post-update    # PostToolUse dynamic state updater
```

These are hidden subcommands (not shown in `sdf help`), designed to be called by Claude Code hooks only.

---

## Layer 4: MCP Server

### What

SDF runs as a local MCP server (stdio transport), giving Claude structured access to SDF operations as typed tools with schemas and return types. Instead of Claude having to compose bash commands and parse text output, it calls `sdf_status`, `sdf_init`, `sdf_branch` as first-class tools.

### Why MCP (and Why Last)

- **Richest integration** — Claude sees tool descriptions, parameter schemas, and structured return types
- **Most complex** — Requires implementing the MCP protocol in Go (JSON-RPC over stdio)
- **Not required** — Layers 1-3 already solve the "Claude doesn't know about SDF" problem. MCP makes it more natural.
- **Independent value** — An MCP server is useful beyond Claude Code (any MCP client — IDEs, other AI tools — could use it)

### Tools Exposed

| Tool | Parameters | Returns |
|------|-----------|---------|
| `sdf_status` | `stack?: string` | Stack topology, PR states, sync status (JSON) |
| `sdf_init` | `name: string, branch?: string, base?: string` | Created stack info (JSON) |
| `sdf_branch` | `name: string, no_prefix?: bool` | Created branch info (JSON) |
| `sdf_sync` | `stack?: string, with_content?: bool` | Sync result: what changed, any conflicts (JSON) |
| `sdf_pr` | `title?: string` | PR URL, number, base branch (JSON) |
| `sdf_merge` | `method?: "squash"|"merge"|"rebase", yes?: bool` | Merge result (JSON) |
| `sdf_switch` | `branch: string` | Current position in stack (JSON) |
| `sdf_context_show` | `stack?: string` | Assembled context for current branch (text) |
| `sdf_move` | `commits: string[]` | Move result (JSON) |

### Architecture

```
Claude Code
    ↕ stdin/stdout (JSON-RPC)
sdf mcp serve
    ↕ calls internal Go functions
sdf commands (same binary)
```

The MCP server reuses the same internal packages (`internal/stack`, `internal/git`, `internal/gh`). No separate binary — `sdf mcp serve` starts the server in the existing `sdf` binary.

### Configuration

Added to `.mcp.json` (project scope) by `sdf claude-setup`:

```json
{
  "mcpServers": {
    "sdf": {
      "type": "stdio",
      "command": "sdf",
      "args": ["mcp", "serve"]
    }
  }
}
```

### Implementation Notes

- Use an existing Go MCP library (e.g., `github.com/mark3labs/mcp-go`) or implement the minimal JSON-RPC subset needed
- Tool descriptions should be concise — Claude reads them on every turn
- Return structured JSON, not formatted text — Claude can interpret JSON better than parsing CLI output
- The `sdf_sync` tool with conflicts is special: it should return the conflict state and let Claude decide how to proceed (call `sdf_sync --continue` after resolution, or ask the user)

---

## The `sdf claude-setup` Command

A new top-level command that generates all Claude Code integration files:

```
sdf claude-setup [--layer 1|2|3|4|all]
```

**Default behavior** (`sdf claude-setup` with no flags): generates Layers 1-3 (rules, skills, hooks). Layer 4 (MCP) requires `--layer 4` or `--layer all` because it adds a runtime dependency.

**What it creates:**

```
.claude/
  rules/
    sdf.md                    # Layer 1: instructions + dynamic state
  skills/
    sdf-status/SKILL.md       # Layer 2: slash commands
    sdf-new-stack/SKILL.md
    sdf-next-layer/SKILL.md
    sdf-ship/SKILL.md
    sdf-sync/SKILL.md
  settings.json               # Layer 3: hooks
.mcp.json                     # Layer 4: MCP server config (if requested)
```

**Idempotent:** Running `sdf claude-setup` again updates existing files without duplicating. The dynamic state section uses markers for surgical updates.

**Integration with `sdf init`:** After creating the stack, `sdf init` prints:

```
Run `sdf claude-setup` to set up Claude Code integration.
```

Or, with a flag: `sdf init --with-claude` runs the setup automatically.

---

## Implementation Plan

### Phase 1: Layer 1 — CLAUDE.md Rules Generation

**Scope:** Generate `.claude/rules/sdf.md` with static reference + dynamic state.

1. Create `internal/claudeintegration/rules.go` — template for sdf.md, render/update functions
2. Create `cmd/claude_setup.go` — the `sdf claude-setup` command (initially just Layer 1)
3. Wire `UpdateDynamicState` into `sdf init`, `sdf branch`, `sdf sync`, `sdf merge`, `sdf fetch`
4. Tests: verify generation, verify dynamic state updates preserve custom content outside markers

### Phase 2: Layer 2 — Skills

**Scope:** Generate `.claude/skills/sdf-*/SKILL.md` files.

1. Add skill templates to `internal/claudeintegration/skills.go`
2. Extend `sdf claude-setup` to generate skills
3. Test: verify skill files are well-formed YAML frontmatter + markdown

### Phase 3: Layer 3 — Hooks

**Scope:** Generate `.claude/settings.json` with hooks; implement `sdf hook` subcommands.

1. Create `cmd/hook.go` — hidden `sdf hook guard-git` and `sdf hook post-update` subcommands
2. Add hook configuration template to `internal/claudeintegration/hooks.go`
3. Extend `sdf claude-setup` to generate `.claude/settings.json`
4. Tests: verify guard-git correctly blocks/allows commands; verify post-update regenerates state

### Phase 4: Layer 4 — MCP Server

**Scope:** Implement `sdf mcp serve` as a stdio MCP server.

1. Add MCP protocol handling (JSON-RPC over stdio)
2. Implement tool handlers wrapping existing internal packages
3. Add `.mcp.json` generation to `sdf claude-setup --layer 4`
4. Tests: verify tool invocations return correct JSON schemas

---

## Alternatives Considered

### "Just write a good README"

Claude Code can reference README.md via `@README` imports in CLAUDE.md. But:
- The README is written for humans, not for Claude's instruction format
- No dynamic state — Claude doesn't know which stacks exist or their current state
- No guard rails — Claude can still run raw git commands
- Puts the burden on the developer to set up the CLAUDE.md import

### "Just use `--append-system-prompt` when SDF shells out to Claude"

This only helps when SDF invokes Claude (conflict resolution, PR descriptions). It doesn't help when the developer is using Claude Code interactively and wants it to use SDF commands. The interactive case is the primary use case for this design.

### "Build a VS Code extension instead"

SDF is CLI-first. Claude Code is the natural integration point for CLI tools. A VS Code extension would be a separate project with a different audience. Nothing prevents both from existing, but the CLI integration comes first.

### "Skip Layers 2 and 3, go straight to MCP"

MCP gives the richest integration but is the most complex to implement and maintain. Layers 1-3 are simpler, independently valuable, and establish patterns that make Layer 4 better (the rules file teaches Claude concepts that make MCP tool descriptions shorter). The layered approach also lets us ship value incrementally.

---

## Open Questions

1. **Should `sdf init` auto-run `sdf claude-setup`?** Pro: zero-friction setup. Con: creates `.claude/` directory structure that might surprise users who don't use Claude Code. Recommendation: print a suggestion but don't auto-run. Add `--with-claude` flag for opt-in.

2. **Should the guard-git hook hard-block or soft-warn?** Hard-blocking (exit 2) prevents mistakes but might frustrate users doing legitimate non-SDF git operations on non-stack branches. The hook should check if the current branch is in a stack before blocking. If not in a stack, exit 0 (allow).

3. **Where should skills live — per-repo or per-user?** Per-repo (`.claude/skills/`) makes them visible and customizable by the team. Per-user (`~/.claude/skills/`) makes them always available. Recommendation: per-repo by default, with a `--global` flag on `sdf claude-setup` for user-level installation.

4. **Should the MCP server be a long-running daemon or per-invocation?** Stdio MCP servers in Claude Code are long-running (started once per session). SDF operations are stateless (they read `.sdf/` files each time), so a long-running process works fine. No daemon management needed — Claude Code handles the lifecycle.

5. **How do we handle repos with existing `.claude/` configuration?** `sdf claude-setup` should merge, not overwrite. For `settings.json`, merge hooks arrays. For `rules/`, add `sdf.md` without touching other rule files. For `skills/`, add `sdf-*` without touching other skills.
