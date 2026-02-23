# sdf — Stacked Diff CLI: Design Document

## Overview

`sdf` is a lightweight CLI tool that manages stacked diffs (chains of dependent PRs) in a Git repository. It handles branch topology and cascade rebasing when head PRs merge.

The tool is a thin orchestration layer over three CLIs that developers already have: `git`, `gh`, and `claude`. No external dependencies beyond the Go stdlib.

-----

## Core Problems

**Stack topology management** — knowing the shape of the stack, persisting that metadata, and updating it as PRs merge.

**Branch synchronization** — when a head PR merges into main, rebasing the next branch onto main, then cascading that rebase down the rest of the stack and updating PR bases in GitHub.

-----

## Workflows & Scenarios

The scenarios below define the workflows `sdf` must support. They are ordered from simple to complex and serve as the acceptance criteria for the tool.

-----

### Scenario 1: Building a feature across a stack

The canonical use case. A feature is too large for one PR, so it's split into layers that can be reviewed independently.

```
# Start from main
git checkout main

sdf init --stack users-feature

# Layer 1: schema
sdf branch users/db-schema
# ... implement migration, User table with id, email, created_at
sdf pr

# Layer 2: repository
sdf branch users/repository # auto-bases off users/db-schema
# ... implement UserRepository with FindByID, Create
sdf pr

# Layer 3: controller + tests
sdf branch users/controller # auto-bases off users/repository
# ... implement POST /users, GET /users/:id, integration tests
sdf pr
```

At this point three PRs exist with correct base chains: `main ← db-schema ← repository ← controller`.

-----

### Scenario 2: Amending an earlier branch in the stack

A reviewer or the author notices something missing in an earlier layer. Work needs to happen on that branch, then flow forward automatically.

```
# Switch back to the schema branch
git checkout users/db-schema

# Add the missing column
# ... add display_name varchar(255) to migration and User struct

git add . && git commit -m "add display_name to users table"
git push

# Now cascade the change forward through repository and controller
sdf sync
```

`sdf sync` detects that `users/db-schema` has new commits since `users/repository` was branched. It rebases `users/repository` onto the new tip of `users/db-schema`, then rebases `users/controller` onto the new tip of `users/repository`, pushing each branch and updating its PR base in GitHub.

If `users/repository` has code that conflicts with the new column (e.g. a `Scan()` call that now has the wrong arity), `sdf sync` invokes Claude with the conflict markers plus the upstream diff summary and the PR description for resolution.

After sync, the developer switches to `users/controller` to use the new attribute:

```
git checkout users/controller

# The branch is already rebased and has the updated User struct available
# ... add display_name to the POST /users response payload and test fixture

git add . && git commit -m "include display_name in user response"
git push
```

No manual rebase commands. No hunting for the right `--onto` incantation.

-----

### Scenario 3: Head PR merges, stack shifts onto main

The first PR in a stack is approved and merged by a reviewer. The remaining branches need to shift their bases to `main` and update their GitHub PRs.

```
# PR #142 (users/db-schema) is merged by a reviewer on GitHub

sdf sync
```

`sdf sync` polls `gh pr list`, sees PR #142 is merged, and:

1. Rebases `users/repository` onto `main` (dropping the commits that are now in `main`)
1. Rebases `users/controller` onto the new tip of `users/repository`
1. Force-pushes both branches
1. Runs `gh pr edit 143 --base main` and `gh pr edit 144 --base users/repository`
1. Updates `stacks/<name>.json` — removes `users/db-schema` node, sets new base to `main`

The developer doesn't need to know what merge commit was used or what the new `main` tip is. The PRs on GitHub now show clean, correct diffs.

-----

### Scenario 4: Conflict during cascade rebase

A change in an earlier branch is semantically incompatible with what a downstream branch does — not just a textual conflict, but a shape change that requires understanding intent.

```
# users/db-schema is amended: email is renamed to email_address for consistency

sdf sync
# → rebasing users/repository onto updated users/db-schema
# → CONFLICT in internal/users/repository.go
```

`sdf sync` pauses and constructs a Claude prompt containing:

- The PR description for the current branch (fetched from GitHub)
- A diff summary of what changed in `users/db-schema` during this sync cycle
- The full content of the conflicted file with conflict markers

Claude resolves the conflict by updating all `email` references to `email_address` consistently throughout the repository layer, `git add`s the result, and `git rebase --continue`s. The cascade then proceeds to `users/controller`.

If the conflict touches test fixtures or multiple files, Claude handles them all in one session before continuing — the prompt includes every conflicted file.

-----

### Scenario 5: sdf status — at-a-glance stack health

```
sdf status

  users-feature  (base: main)

  ✓  users/db-schema    PR #142  merged
  ●  users/repository   PR #143  open     2 commits ahead, in sync
  ●  users/controller   PR #144  open     3 commits ahead, needs sync

  run `sdf sync` to rebase users/controller onto updated users/repository
```

`needs sync` means the branch's recorded base tip in `stacks/<name>.json` differs from the current tip of its parent — there are commits on the parent that this branch hasn't seen yet. This happens after Scenario 2 (amending an earlier branch) before `sdf sync` is run.

-----

## Command Surface

```
sdf init <name>           # initialize stack, auto-detect base branch from origin HEAD
sdf branch [--no-prefix] <name>  # create branch, register in stack, push tracking branch
sdf pr                    # gh pr create for the current branch
sdf sync [<stack>]        # detect merged PRs via gh, cascade rebase, push
sdf status [<stack>]      # show stack topology with PR state
sdf switch [<branch>]     # checkout a branch
sdf <branch>              # shorthand for switch
sdf config show           # display effective (merged) configuration
sdf config set <key> <val>  # set a value in repo config (or --global)
```

Commands that operate on a stack (sync, status, branch) auto-detect which stack
to use from the current branch. When multiple stacks exist and the current branch
is ambiguous, pass `--stack <name>` or use a positional argument.

`sdf init` always roots the stack at the base branch (auto-detected from
`origin/HEAD`, or specified with `--base`). Your current branch doesn't matter —
the stack is defined by its base, not by where you run the command.

All three dependencies (`git`, `gh`, `claude`) are version-checked at startup.

-----

## Repository Layout

```
.sdf/
  config.json             # repo-level configuration (committed)
  stacks/
    auth-overhaul.json    # stack topology, PR numbers, sync state
    billing.json          # multiple stacks can coexist
  local.json              # ephemeral state (gitignored)
```

Stack files are committed alongside code, so stack topology is available on any clone and in CI.

The only exception is ephemeral state (active Claude session IDs, in-progress sync state), which lives in `.sdf/local.json` and is gitignored.

### stack.json Schema

```json
{
  "stack_id": "auth-overhaul",
  "base": "main",
  "nodes": [
    { "branch": "auth/db-schema",   "pr": 142, "status": "merged" },
    { "branch": "auth/session-api", "pr": 143, "status": "open"   },
    { "branch": "auth/ui-login",    "pr": 144, "status": "open"   }
  ]
}
```

-----

## Sync Loop

`sdf sync` is the workhorse command. The sequence:

1. `gh pr list --json number,headRefName,state,mergeCommit` for all PRs in the stack
1. Walk merged nodes from the bottom up
1. For each unmerged branch above a merge point:
- `git rebase --onto <new-base> <old-base> <branch>`
- If conflict → invoke Claude session (see below)
- `git push --force-with-lease origin <branch>`
- `gh pr edit <number> --base <new-base>` — update the PR's base in GitHub so the diff renders correctly
1. Update `stacks/<name>.json` (mark merged nodes, shift base pointers)
1. Commit the updated `stacks/<name>.json`

The `gh pr edit --base` step is easy to miss but critical — without it, GitHub shows every commit from `main` in the PR diff after rebasing.

-----

## Configuration

`sdf` uses a two-tier configuration model. Values are loaded from two files and merged field-by-field, with repo-level values taking precedence over global defaults:

| Level | Path | Committed |
|-------|------|-----------|
| Global | `~/.config/sdf/config.json` | No (user-specific) |
| Repo | `.sdf/config.json` | Yes (shared with team) |

Missing files are not errors — the tool works out of the box with sensible defaults.

### Branch Prefix Enforcement

When `sdf branch <name>` creates a branch, it auto-prefixes the name with the stack ID and a separator (default `/`). This is controlled by the `branch_prefix` config block:

```json
{
  "branch_prefix": {
    "enabled": true,
    "prefix": "",
    "separator": "/"
  }
}
```

- **enabled** (default `true`) — set to `false` to disable auto-prefixing
- **prefix** (default: stack ID) — override the prefix string; when empty, the stack's `stack_id` is used
- **separator** (default `/`) — character between prefix and branch name

For example, in a stack with `stack_id = "auth"`, `sdf branch db-schema` creates `auth/db-schema`. The `--no-prefix` flag on `sdf branch` skips prefixing for a single invocation without changing config.

`sdf init` and `sdf register` create a default `.sdf/config.json` so the file is discoverable immediately.

### Merge Semantics

When both files exist, non-zero/non-nil fields in the repo config override the corresponding global fields. Unset fields in the repo config fall through to global values. After merging, any remaining unset fields receive hardcoded defaults (`enabled=true`, `separator="/"`).

-----

## Conflict Resolution

When `git rebase` exits non-zero during `sdf sync`, the tool hands off to Claude CLI rather than failing or opening a merge tool.

### Prompt Construction

```
You are rebasing auth/ui-login onto the updated auth/session-api.

=== UPSTREAM CHANGE SUMMARY ===
auth/session-api changed after its own rebase:
  - bcrypt token hashing was changed to argon2id
  - POST /sessions now returns { token, session_id } instead of { token }

=== BRANCH PR DESCRIPTION ===
Implements login UI calling POST /sessions, stores token in localStorage.

=== CONFLICTS ===
File: src/auth/client.ts
<<<<<<< HEAD
const { token, session_id } = await postSession(credentials)
=======
const { token } = await postSession(credentials)
>>>>>>> auth/ui-login

Resolve all conflicts. For each file output the complete resolved content in a
fenced code block with the filename: ```typescript src/auth/client.ts
```

### Claude CLI Integration

```go
func resolveConflicts(stack *Stack, branch string) error {
    conflicted, _ := gitConflictedFiles()

    var prompt strings.Builder
    prompt.WriteString(buildUpstreamChangeSummary(stack, branch))
    prompt.WriteString(branchPRDescription(stack, branch))
    for _, f := range conflicted {
        content, _ := os.ReadFile(f)
        fmt.Fprintf(&prompt, "File: %s\n```\n%s\n```\n\n", f, content)
    }
    prompt.WriteString(resolutionInstructions)

    sessionName := "conflict-" + sanitize(branch)
    cmd := exec.Command("claude", "--session", sessionName, "-p", prompt.String())
    output, err := cmd.Output()
    if err != nil {
        return fallbackToEditor(conflicted)
    }

    if err := writeResolvedFiles(output, conflicted); err != nil {
        return fallbackToEditor(conflicted)
    }

    exec.Command("git", "add", ".").Run()
    exec.Command("git", "rebase", "--continue").Run()
    return nil
}
```

The session name `conflict-<branch>` is deterministic, so the session can be resumed if the process crashes mid-resolution. If Claude's output can't be parsed (fenced block extraction fails), the tool falls back to opening `$EDITOR` rather than silently corrupting files.

-----

## Implementation Phases

### Phase 1 — Stack Plumbing

- `sdf init`, `sdf branch`, `sdf status`
- `stacks/<name>.json` management
- `gh` integration for PR state polling
- `sdf sync` with cascade rebase — error on conflict (no Claude yet)

### Phase 2 — Claude Integration

- Conflict resolution via Claude CLI session
- PR content generation (titles and descriptions)
- Iterative resolution loop if Claude produces unparseable output

-----

## Design Principles

**Shell out, don't reimplement.** `gh` handles GitHub auth, pagination, and API quirks. `git` handles the rebase mechanics. `claude` handles the intelligence. `sdf` is pure orchestration.

**Fail loudly, recover gracefully.** Sync failures leave the repo in a known state (mid-rebase or pre-rebase). The tool always tells the user what state they're in and how to recover manually if automation fails.

**Deterministic session names.** Claude sessions are named after the operation and branch, not random UUIDs, so they're resumable and debuggable.
