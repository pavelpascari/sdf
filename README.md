# sdf — Stacked Diffs Flow

A lightweight CLI that manages stacked diffs (chains of dependent pull requests) in Git repositories. It orchestrates `git`, `gh`, and `claude` to handle stack topology, cascade rebasing, and semantic context continuity across branches.

## Why

Large features rarely fit in a single PR. Splitting work into a chain of dependent PRs keeps reviews focused, but the maintenance burden is real: rebasing cascades when upstream changes, PR bases drift after merges, and context about *why* each layer exists lives only in someone's head. `sdf` eliminates that overhead.

## What it does

- **Stack topology** — tracks branch ordering and PR metadata in `.sdf/stacks/<name>.json`, committed alongside code
- **Multiple stacks** — a single repo can have several independent stacks, each with its own base branch
- **Cascade rebase** — when a head PR merges or an earlier branch is amended, `sdf sync` rebases every downstream branch, force-pushes, and updates PR bases in GitHub
- **Context documents** — each branch carries a Markdown doc (`.sdf/context/<branch>.md`) describing intent, upstream constraints, decisions, and open questions
- **AI conflict resolution** — when rebase conflicts occur, Claude receives the full assembled stack context plus the conflicted files and resolves them in-place
- **PR creation** — `sdf pr` creates a GitHub PR with the context doc as the body, so reviewers see intent alongside the diff

## Prerequisites

- [git](https://git-scm.com/)
- [gh](https://cli.github.com/) — GitHub CLI (required for PR operations)
- [claude](https://docs.anthropic.com/en/docs/claude-code) — Claude CLI (optional, for conflict resolution and context updates)

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
# Initialize a stack (auto-detects base branch from origin HEAD)
sdf init users-feature

# Create the first branch (chains from the base branch)
sdf branch users/db-schema
# ... write code ...
sdf context edit          # document intent, decisions, constraints
sdf pr                    # create PR with context doc as body

# Stack another branch on top
sdf branch users/repository
# ... write code ...
sdf context edit
sdf pr

# Add a third layer
sdf branch users/controller
# ... write code ...
sdf context edit
sdf pr
```

You now have three PRs chained as `main <- db-schema <- repository <- controller`.

When the first PR merges:

```sh
sdf sync
```

This rebases the remaining branches onto `main`, pushes them, and updates their PR bases in GitHub — no manual rebase required.

### Working with multiple stacks

You can have multiple independent stacks in the same repo:

```sh
sdf init auth-feature
sdf branch auth/db-schema
# ... work on auth ...

sdf init billing-feature
sdf branch billing/models
# ... work on billing ...

# Sync a specific stack
sdf sync auth-feature

# Check status of a specific stack
sdf status billing-feature

# Switch between branches (shows stack context)
sdf switch auth/db-schema
# or just:
sdf auth/db-schema
```

When you're on a branch that belongs to a stack, commands like `sdf sync`, `sdf status`, and `sdf branch` auto-detect which stack to use. If you have multiple stacks and your current branch isn't in any of them, pass `--stack <name>` or use the positional argument.

## Commands

```
Stack commands:
  init [--base <branch>] <name>      Initialize a new stack (base auto-detected)
  register                           Discover and register existing PR stacks
  branch [--stack <name>] <name>     Create a new branch in the stack
  status [--stack <name>]            Show stack topology and sync state
  sync [<stack>] [--stack <name>]    Detect merged PRs, cascade rebase, push
  move <commit>...                   Move commits from current branch to parent
  pr                                 Create a GitHub PR for the current branch

Navigation:
  switch [<branch>]                  Switch to a branch (shows stack context)
  <branch>                           Shorthand for switch <branch>

Context commands:
  context show              Print assembled context for current branch
  context edit              Open context doc in $EDITOR
  context update            Ask Claude to rewrite context doc

Other:
  doctor                    Check that dependencies are available
  version                   Print version
  help                      Show this help
```

## How `sdf init` works

`sdf init <name>` creates a stack rooted at a **base branch**. The base branch is where PRs ultimately merge into — typically `main` or `master`.

- **If `--base` is provided**, that branch is used as the base
- **Otherwise**, the base is auto-detected from `origin/HEAD` (falling back to `main` or `master` if they exist)
- **The base branch is validated** — init fails if the branch doesn't exist
- **Your current branch doesn't matter** — the stack is always rooted at the base branch, not wherever you happen to be checked out. All branches created with `sdf branch` chain from the base (or from the last branch in the stack)

This means `sdf init my-feature` does the same thing whether you run it from `main`, `develop`, or any other branch.

## How sync works

1. Polls GitHub for PR state via `gh`
2. Walks merged nodes from the bottom of the stack upward
3. Rebases each unmerged branch onto its new base
4. If conflicts arise, invokes Claude with assembled stack context for resolution
5. Force-pushes updated branches and runs `gh pr edit --base` to fix PR diffs
6. Updates `.sdf/stacks/<name>.json`

## Repository layout

```
.sdf/
  stacks/
    users-feature.json      # stack topology, PR numbers, sync state
    auth-feature.json       # a second independent stack
  context/
    users/db-schema.md
    users/repository.md
    auth/db-schema.md
  local.json                # ephemeral state (gitignored)
```

Both stack files and context docs are committed alongside code. This means:

- Stack topology is available on any clone and in CI
- Context docs appear in PR reviews — reviewers see the intent alongside the diff
- `git log` on a context doc shows how intent evolved over the life of the PR

## License

MIT
