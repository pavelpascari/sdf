# sdf — Stacked Diffs Flow

A lightweight CLI that manages stacked diffs (chains of dependent pull requests) in Git repositories. It orchestrates `git`, `gh`, and `claude` to handle stack topology and cascade rebasing across branches.

## Why

Large features rarely fit in a single PR. Splitting work into a chain of dependent PRs keeps reviews focused, but the maintenance burden is real: rebasing cascades when upstream changes and PR bases drift after merges. `sdf` eliminates that overhead.

## What it does

- **Stack topology** — tracks branch ordering and PR metadata in `.sdf/stacks/<name>.json`, stored locally on your machine
- **Multiple stacks** — a single repo can have several independent stacks, each with its own base branch
- **Cascade rebase** — when a head PR merges or an earlier branch is amended, `sdf sync` rebases every downstream branch, force-pushes, and updates PR bases in GitHub
- **AI conflict resolution** — when rebase conflicts occur, Claude receives the PR description and upstream diff summary plus the conflicted files and resolves them in-place
- **PR creation** — `sdf pr` creates a GitHub PR for the current branch

## Prerequisites

- [git](https://git-scm.com/)
- [gh](https://cli.github.com/) — GitHub CLI (required for PR operations)
- [claude](https://docs.anthropic.com/en/docs/claude-code) — Claude CLI (optional, for conflict resolution and PR content generation)

Run `sdf doctor` to verify all dependencies are available.

## Install

### Homebrew (macOS and Linux)

```sh
brew install pavelpascari/tap/sdf
```

### Download a binary

Grab the latest archive for your platform from [GitHub Releases](https://github.com/pavelpascari/sdf/releases/latest), or use curl:

```sh
# macOS (Apple Silicon)
curl -fsSL https://github.com/pavelpascari/sdf/releases/latest/download/sdf-darwin-arm64.tar.gz | tar xz
# macOS (Intel)
curl -fsSL https://github.com/pavelpascari/sdf/releases/latest/download/sdf-darwin-amd64.tar.gz | tar xz
# Linux (x86_64)
curl -fsSL https://github.com/pavelpascari/sdf/releases/latest/download/sdf-linux-amd64.tar.gz | tar xz
# Linux (ARM)
curl -fsSL https://github.com/pavelpascari/sdf/releases/latest/download/sdf-linux-arm64.tar.gz | tar xz
```

Then move the binary to your PATH:

```sh
sudo mv sdf /usr/local/bin/
```

Each release includes a `checksums.txt` for verification:

```sh
curl -fsSL https://github.com/pavelpascari/sdf/releases/latest/download/checksums.txt | sha256sum --check --ignore-missing
```

### From source

Requires [Go](https://go.dev/) 1.24+.

```sh
git clone https://github.com/pavelpascari/sdf.git
cd sdf
make build        # → bin/sdf
sudo cp bin/sdf /usr/local/bin/
```

Or install directly to `$GOPATH/bin`:

```sh
make install
```

## Quick start

```sh
# Create a stack and its first branch in one step
sdf new users-feature --branch db-schema
# ... write code ...
sdf pr                    # create PR

# Stack another branch on top
sdf branch repository
# ... write code ...
sdf pr

# Add a third layer
sdf branch controller
# ... write code ...
sdf pr
```

You now have three PRs chained as `main <- users-feature/db-schema <- users-feature/repository <- users-feature/controller`.

When the first PR merges:

```sh
sdf sync
```

This rebases the remaining branches onto `main`, pushes them, and updates their PR bases in GitHub — no manual rebase required.

By default, `sdf sync` only acts on changes within the stack (merged PRs, amended parents). If the base branch has advanced from unrelated work, a hint is shown. Use `sdf sync --full` to also rebase onto the latest base branch tip.

### Working with multiple stacks

You can have multiple independent stacks in the same repo:

```sh
sdf new auth-feature --branch login
# ... work on auth ...

sdf new billing-feature --branch models
# ... work on billing ...

# Sync a specific stack
sdf sync auth-feature

# Check status of a specific stack
sdf status billing-feature

# Switch between branches
sdf switch auth/db-schema
# or just:
sdf auth/db-schema
```

When you're on a branch that belongs to a stack, commands like `sdf sync`, `sdf status`, and `sdf branch` auto-detect which stack to use. If you have multiple stacks and your current branch isn't in any of them, pass `--stack <name>` or use the positional argument.

## Commands

```
Stack commands:
  new [flags] <name>                 Create a new stack and its first branch
  new --worktrees <name>             Create a stack in worktree mode
  register                           Discover and register existing PR stacks
  branch [--no-prefix] <name>        Add another branch to the stack
  status [--stack <name>]            Show stack topology and sync state
  sync [<stack>] [--stack <name>]    Detect merged PRs, cascade rebase, push
  move <commit>...                   Move commits from current branch to parent
  pr                                 Create a GitHub PR for the current branch

Worktree commands:
  worktree enable [--stack <name>]   Enable worktree mode; materialize open branches

Navigation:
  switch [<branch>]                  Switch to a branch (worktree mode: prints path)
  switch <branch> --path-only        Print only the worktree path
  <branch>                           Shorthand for switch <branch>

Config commands:
  config show               Display effective (merged) configuration
  config set <key> <value>  Set a config value in repo or --global config

Other:
  doctor                    Check that dependencies are available (includes worktree integrity)
  version                   Print version
  help                      Show this help
```

## How `sdf new` works

`sdf new <name>` creates a stack and its first branch in one step:

```
sdf new <name> [--base <branch>] [--branch <name>] [--json]
```

- **Stack + branch** — creates the `.sdf/` metadata, a git branch, and pushes to origin
- **Branch name** defaults to the stack name. Override with `--branch <name>`
- **Base branch** is auto-detected from `origin/HEAD`. Override with `--base <branch>`
- **Branch prefix** is applied automatically (e.g. `sdf new users --branch db-schema` creates `users/db-schema`)
- **Your current branch doesn't matter** — the stack is always rooted at the base branch. After `sdf new`, you're checked out on the new branch

> **Note:** `sdf init` still works as a backward-compatible alias but `sdf new` is the recommended command.

### Machine-readable output

Use `--json` for scripting or AI agent integration:

```sh
sdf new my-feature --json
```

```json
{
  "stack": "my-feature",
  "base": "main",
  "branch": "my-feature/my-feature",
  "pushed": true
}
```

## How sync works

1. Polls GitHub for PR state via `gh`
2. Walks merged nodes from the bottom of the stack upward
3. Rebases each unmerged branch onto its new base
4. If conflicts arise, invokes Claude with the PR description and upstream diff for resolution
5. Force-pushes updated branches and runs `gh pr edit --base` to fix PR diffs
6. Updates `.sdf/stacks/<name>.json`

## Worktree mode

Worktree mode is an opt-in stack setting where every branch lives in its own [git worktree](https://git-scm.com/docs/git-worktree) — a separate checkout directory. Multiple agents (or terminal sessions) can work on different branches simultaneously without stepping on each other, because each branch occupies its own directory on disk. The main repo stays checked out on the base branch.

### Enabling worktree mode

Create a new stack in worktree mode:

```sh
sdf new users-feature --branch db-schema --worktrees
```

Or convert an existing normal stack:

```sh
sdf worktree enable [--stack <name>]
```

`sdf worktree enable` flips the stack's worktree flag and materializes a checkout directory for every open branch. Merged and closed branches are skipped.

### Adding branches

`sdf branch <name>` works the same way. When the stack is in worktree mode, the new branch is created in its own worktree and the main repo is left untouched:

```sh
sdf branch repository
# Created branch "users-feature/repository" in stack "users-feature"
# Worktree: /path/to/repo.worktrees/users-feature-repository
#   cd /path/to/repo.worktrees/users-feature-repository
```

### Navigating between branches

`sdf switch <branch>` prints the worktree path instead of checking out:

```sh
sdf switch users-feature/db-schema
# Worktree for users-feature/db-schema:
#   cd /path/to/repo.worktrees/users-feature-db-schema
```

Use `--path-only` to emit just the path, which shell-integrates cleanly:

```sh
cd "$(sdf switch users-feature/db-schema --path-only)"
```

### Syncing in worktree mode (pull model)

Instead of a single cascade rebase across all branches, worktree mode uses a pull model: each branch's agent is responsible for rebasing its own branch onto its parent.

**Run `sdf sync` from inside a branch's worktree** to rebase just that branch:

```sh
cd "$(sdf switch users-feature/repository --path-only)"
# ... commit your changes first ...
sdf sync          # rebases users-feature/repository onto users-feature/db-schema
```

Rules:
- The worktree must be clean (no uncommitted changes) before sync is accepted.
- Conflicts pause the rebase. Resolve them in the worktree, stage the files, then run `sdf sync --continue`.
- After a branch is rebased and pushed, `sdf sync` prints which downstream branch needs to sync next.

**Run `sdf sync` from the main repo** to get a readiness dashboard. No rebasing happens — it shows each branch's sync state:

```
Stack users-feature (worktree mode) — branch readiness:
  ✓ users-feature/db-schema — up to date
  ⚠ users-feature/repository — needs sync (users-feature/db-schema advanced)
  ✓ users-feature/controller — up to date (uncommitted work present)

Run `sdf sync` inside a branch's worktree to integrate it.
```

### Viewing stack state

`sdf status` shows each branch's worktree path and dirty state alongside the normal topology information.

`sdf ls` tags worktree stacks with `[worktree]`:

```
  users-feature         3 PRs   active  [worktree]
```

### Lifecycle: merge and prune

When `sdf merge` merges the head PR, the merged branch's worktree is removed automatically. The downstream branch syncs on its own next run.

`sdf prune --apply` removes worktrees for any branches that no longer exist in git.

`sdf doctor` includes a worktree integrity check — it reports missing worktree directories and branches not registered with git.

### Configuration

The worktree base directory defaults to `../{repo}.worktrees` (a sibling of the repo), where `{repo}` is the repo directory name. Each branch's worktree lives at `<base_path>/<branch>` with `/` replaced by `-`.

Override the base path per repo:

```sh
sdf config set worktree.base_path /absolute/path/to/worktrees
```

Or use a relative path with the `{repo}` placeholder:

```sh
sdf config set worktree.base_path /workareas/{repo}
```

## Configuration

`sdf` uses a two-tier configuration system:

- **Global**: `~/.config/sdf/config.json` — user-level defaults that apply to all repos
- **Repo**: `.sdf/config.json` — per-repo overrides, stored locally per machine

Repo-level values override global values on a field-by-field basis. Missing files are fine — `sdf` works without any config file using sensible defaults.

### Branch prefix enforcement

By default, `sdf branch` automatically prefixes branch names with the stack ID and a separator. For example, in a stack called `users-feature`:

```sh
sdf branch db-schema
# creates: users-feature/db-schema
```

This keeps branches organized and namespaced to their stack. The behavior is configurable:

| Key | Default | Description |
|-----|---------|-------------|
| `branch_prefix.enabled` | `true` | Whether to auto-prefix branch names |
| `branch_prefix.scope` | (stack ID) | Scope used as branch prefix and conventional commit scope (empty = use stack ID) |
| `branch_prefix.separator` | `/` | Character between prefix and branch name |
| `worktree.base_path` | `../{repo}.worktrees` | Base directory for worktrees; `{repo}` is replaced with the repo directory name |

```sh
# Disable prefix enforcement for this repo
sdf config set branch_prefix.enabled false

# Use a custom prefix instead of stack ID
sdf config set branch_prefix.scope feat

# Change separator (e.g. feat-db-schema instead of feat/db-schema)
sdf config set branch_prefix.separator -

# Set a global default
sdf config set --global branch_prefix.separator -

# Skip prefix for a single branch
sdf branch --no-prefix my-branch
```

Use `sdf config show` to see the effective (merged) configuration and the file paths being used.

## Repository layout

```
.sdf/
  config.json               # repo-level configuration
  stacks/
    users-feature.json      # stack topology, PR numbers, sync state
    auth-feature.json       # a second independent stack
  local.json                # ephemeral state
```

The entire `.sdf/` directory is **local-only** — it is listed in `.gitignore` and is never pushed to the remote. Each developer's machine maintains its own copy of the stack metadata. PR descriptions on GitHub serve as the shared source of truth for stack structure, and `sdf fetch` can reconstruct local state from the PR graph at any time.

## License

MIT
