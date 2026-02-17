# sdf — Stacked Diff CLI: Design Document

## Overview

`sdf` is a lightweight CLI tool that manages stacked diffs (chains of dependent PRs) in a Git repository. It handles branch topology, cascade rebasing when head PRs merge, and context continuity so Claude can reason across the full stack when resolving conflicts or continuing work.

The tool is a thin orchestration layer over three CLIs that developers already have: `git`, `gh`, and `claude`. No external dependencies beyond the Go stdlib.

-----

## Core Problems

**Stack topology management** — knowing the shape of the stack, persisting that metadata, and updating it as PRs merge.

**Branch synchronization** — when a head PR merges into main, rebasing the next branch onto main, then cascading that rebase down the rest of the stack and updating PR bases in GitHub.

**Context continuity** — ensuring Claude understands not just the current branch but the semantic intent of the whole stack when working on any individual PR, especially during conflict resolution.

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
sdf context edit            # document: adds users table, pgx v5, AppError pattern
sdf pr                      # gh pr create, body from context doc

# Layer 2: repository
sdf branch users/repository # auto-bases off users/db-schema
# ... implement UserRepository with FindByID, Create
sdf context edit            # document: depends on users table schema, returns *User
sdf pr

# Layer 3: controller + tests
sdf branch users/controller # auto-bases off users/repository
# ... implement POST /users, GET /users/:id, integration tests
sdf context edit            # document: calls repository, response shape, test coverage
sdf pr
```

At this point three PRs exist with correct base chains: `main ← db-schema ← repository ← controller`. All context docs are committed and visible in each PR.

-----

### Scenario 2: Amending an earlier branch in the stack

A reviewer or the author notices something missing in an earlier layer. Work needs to happen on that branch, then flow forward automatically.

```
# Switch back to the schema branch
git checkout users/db-schema

# Add the missing column
# ... add display_name varchar(255) to migration and User struct

sdf context update          # Claude rewrites context doc to note new column
git add . && git commit -m "add display_name to users table"
git push

# Now cascade the change forward through repository and controller
sdf sync
```

`sdf sync` detects that `users/db-schema` has new commits since `users/repository` was branched. It rebases `users/repository` onto the new tip of `users/db-schema`, then rebases `users/controller` onto the new tip of `users/repository`, pushing each branch and updating its PR base in GitHub.

If `users/repository` has code that conflicts with the new column (e.g. a `Scan()` call that now has the wrong arity), `sdf sync` invokes Claude with the conflict markers plus the updated context doc — which now documents `display_name` — so the resolution is correct and consistent.

After sync, the developer switches to `users/controller` to use the new attribute:

```
git checkout users/controller

# The branch is already rebased and has the updated User struct available
# ... add display_name to the POST /users response payload and test fixture

sdf context update
git add . && git commit -m "include display_name in user response"
git push
```

No manual rebase commands. No hunting for the right `--onto` incantation. The context docs ensure Claude knows why `display_name` was added and what the expected downstream usage is.

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
1. Updates `stack.json` — removes `users/db-schema` node, sets new base to `main`

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

- The assembled context docs for `users/db-schema` (which now documents `email_address`) and `users/repository` (which documents its dependency on the schema)
- A diff summary of what changed in `users/db-schema` during this sync cycle
- The full content of the conflicted file with conflict markers

Claude resolves the conflict by updating all `email` references to `email_address` consistently throughout the repository layer, `git add`s the result, and `git rebase --continue`s. The cascade then proceeds to `users/controller`.

If the conflict touches test fixtures or multiple files, Claude handles them all in one session before continuing — the prompt includes every conflicted file.

-----

### Scenario 5: Resuming work with full context

A developer comes back to a branch after a few days away, or a second developer picks up the stack. They need to understand the full picture before making changes.

```
git checkout users/controller

sdf context show
```

Output:

```
=== STACK: users-feature ===

[users/db-schema]
Intent: adds users table with id, email_address, display_name, created_at.
Uses pgx v5. Errors follow AppError{Code, Message} pattern.
Decisions: email stored as-is (no normalisation), display_name nullable.

[users/repository]
Intent: implements UserRepository with FindByID(ctx, id) and Create(ctx, *User).
Depends on: users table schema above.
Decisions: no caching layer, returns ErrNotFound sentinel, Create sets created_at in Go not DB.

[users/controller ← current]
Intent: implements POST /users and GET /users/:id. Calls UserRepository.
Response shape: { id, email_address, display_name, created_at }.
Tests: integration tests against real DB using testcontainers.
Open: rate limiting not implemented yet.
```

This output can be piped directly into Claude: `sdf context show | claude` to start a working session with full stack awareness, or used as a briefing document for a code review.

-----

### Scenario 6: sdf status — at-a-glance stack health

```
sdf status

  users-feature  (base: main)

  ✓  users/db-schema    PR #142  merged
  ●  users/repository   PR #143  open     2 commits ahead, in sync
  ●  users/controller   PR #144  open     3 commits ahead, needs sync

  run `sdf sync` to rebase users/controller onto updated users/repository
```

`needs sync` means the branch's recorded base tip in `stack.json` differs from the current tip of its parent — there are commits on the parent that this branch hasn't seen yet. This happens after Scenario 2 (amending an earlier branch) before `sdf sync` is run.

-----

## Command Surface

```
sdf init                  # initialize .sdf/ in current repo, create stack.json
sdf branch <name>         # create branch, register in stack, push tracking branch
sdf pr                    # gh pr create with body pre-populated from context doc
sdf sync                  # detect merged PRs via gh, cascade rebase, push
sdf status                # show stack topology with PR state
sdf context show          # print assembled context for the current branch
sdf context edit          # open context doc in $EDITOR
sdf context update        # ask Claude to rewrite context doc based on current state
```

All three dependencies (`git`, `gh`, `claude`) are version-checked at startup.

-----

## Repository Layout

```
.sdf/
  stack.json              # stack topology, PR numbers, sync state
  context/
    auth/db-schema.md
    auth/session-api.md
    auth/ui-login.md
```

Both `stack.json` and the context docs are committed alongside code. This means:

- Stack topology is available on any clone and in CI
- Context docs appear in PR reviews — reviewers see the intent alongside the diff
- `git log` on a context doc shows how intent evolved over the life of the PR

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
1. Update `stack.json` (mark merged nodes, shift base pointers)
1. Commit the updated `stack.json`

The `gh pr edit --base` step is easy to miss but critical — without it, GitHub shows every commit from `main` in the PR diff after rebasing.

-----

## Context Documents

Each branch carries a context document at `.sdf/context/<branch-name>.md`. This is the mechanism for maintaining semantic continuity across the stack.

### Structure

```markdown
# auth/session-api

## Intent
Implements session management API on top of the DB schema added in auth/db-schema.
Downstream branches (auth/ui-login) will call POST /sessions and GET /sessions/:id.

## Constraints from upstream
- Sessions table has columns: id, user_id, token_hash, expires_at, created_at
- Auth service uses pgx v5, not database/sql
- Error types follow the pattern in db-schema: AppError with Code field

## Decisions made here
- Sessions are stored with bcrypt-hashed tokens, not raw
- Expiry is enforced at the API layer, not the DB (no pg triggers)
- Rate limiting is deferred to auth/ui-login to handle at the edge

## Open questions / known debt
- Token rotation not implemented (tracked in issue #89)
```

### Lifecycle

|Event                |Action                                                                          |
|---------------------|--------------------------------------------------------------------------------|
|`sdf branch <name>`  |Creates stub context doc for the new branch                                     |
|`sdf context edit`   |Opens context doc in `$EDITOR`                                                  |
|`sdf context update` |Pipes current diff + upstream context to Claude, asks it to rewrite the doc     |
|`sdf pr`             |Uses the Intent section as the PR description body                              |
|`sdf sync` (conflict)|Upstream and current context docs are included in the conflict resolution prompt|

### Context Assembly

`sdf context show` walks the stack from base to current branch and concatenates all ancestor context docs, producing a unified view. This is what gets piped to Claude in any operation that needs full stack awareness.

```
[base context]
[auth/db-schema context]
[auth/session-api context]   ← current
```

-----

## Conflict Resolution

When `git rebase` exits non-zero during `sdf sync`, the tool hands off to Claude CLI rather than failing or opening a merge tool.

### Prompt Construction

```
You are rebasing auth/ui-login onto the updated auth/session-api.

=== STACK CONTEXT ===
<assembled context docs for all ancestor branches>

=== UPSTREAM CHANGE SUMMARY ===
auth/session-api changed after its own rebase:
  - bcrypt token hashing was changed to argon2id
  - POST /sessions now returns { token, session_id } instead of { token }

=== CURRENT BRANCH INTENT (auth/ui-login) ===
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
    prompt.WriteString(assembleStackContext(stack, branch))
    prompt.WriteString(buildUpstreamChangeSummary(stack, branch))
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
- `stack.json` management
- `gh` integration for PR state polling
- `sdf sync` with cascade rebase — error on conflict (no Claude yet)

### Phase 2 — Context Scaffolding

- Context doc creation and stub generation in `sdf branch`
- `sdf context show/edit/update`
- Context assembler that produces the unified stack view
- `sdf pr` using context doc for PR body

### Phase 3 — Claude Integration

- Conflict resolution via Claude CLI session
- `sdf context update` using Claude to rewrite docs post-change
- Iterative resolution loop if Claude produces unparseable output

-----

## Design Principles

**Shell out, don't reimplement.** `gh` handles GitHub auth, pagination, and API quirks. `git` handles the rebase mechanics. `claude` handles the intelligence. `sdf` is pure orchestration.

**Context is a first-class artifact.** Intent doesn't live in someone's head or a Notion doc — it lives in the repo, versioned, visible in reviews, and available to Claude without reconstruction.

**Fail loudly, recover gracefully.** Sync failures leave the repo in a known state (mid-rebase or pre-rebase). The tool always tells the user what state they're in and how to recover manually if automation fails.

**Deterministic session names.** Claude sessions are named after the operation and branch, not random UUIDs, so they're resumable and debuggable.
