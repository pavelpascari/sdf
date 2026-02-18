# sdf — Stacked Diffs Flow

A lightweight CLI that manages stacked diffs (chains of dependent pull requests) in Git repositories. It orchestrates `git`, `gh`, and `claude` to handle stack topology, cascade rebasing, and semantic context continuity across branches.

## Why

Large features rarely fit in a single PR. Splitting work into a chain of dependent PRs keeps reviews focused, but the maintenance burden is real: rebasing cascades when upstream changes, PR bases drift after merges, and context about *why* each layer exists lives only in someone's head. `sdf` eliminates that overhead.

## What it does

- **Stack topology** — tracks branch ordering and PR metadata in `.sdf/stack.json`, committed alongside code
- **Cascade rebase** — when a head PR merges or an earlier branch is amended, `sdf sync` rebases every downstream branch, force-pushes, and updates PR bases in GitHub
- **Context documents** — each branch carries a Markdown doc (`.sdf/context/<branch>.md`) describing intent, upstream constraints, decisions, and open questions
- **AI conflict resolution** — when rebase conflicts occur, Claude receives the full assembled stack context plus the conflicted files and resolves them in-place
- **PR creation** — `sdf pr` creates a GitHub PR with the context doc as the body, so reviewers see intent alongside the diff

## Prerequisites

- [Go](https://go.dev/) 1.24+ (build only)
- [git](https://git-scm.com/)
- [gh](https://cli.github.com/) — GitHub CLI (required for PR operations)
- [claude](https://docs.anthropic.com/en/docs/claude-code) — Claude CLI (optional, for conflict resolution and context updates)

Run `sdf doctor` to verify all dependencies are available.

## Install

### From source

```sh
git clone https://github.com/pavelpascari/sdf.git
cd sdf
make build        # → bin/sdf
```

Move the binary somewhere on your `$PATH`:

```sh
cp bin/sdf /usr/local/bin/
```

Or install directly to `$GOPATH/bin`:

```sh
make install
```

### Cross-compile

Build for all supported platforms (linux, macOS, Windows — amd64 and arm64):

```sh
make dist         # → dist/sdf-<os>-<arch>
```

Build for a specific platform:

```sh
make build-for OS=darwin ARCH=arm64
```

## Quick start

```sh
# Start from main
git checkout main

# Initialize a stack
sdf init --stack users-feature

# Create the first branch
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

## Commands

```
sdf init [--stack] <name>     Initialize a new stack in the current repo
sdf branch <name>             Create a new branch in the stack
sdf status                    Show stack topology and sync state
sdf sync                      Detect merged PRs, cascade rebase, push
sdf pr                        Create a GitHub PR for the current branch

sdf context show              Print assembled context for current branch
sdf context edit              Open context doc in $EDITOR
sdf context update            Ask Claude to rewrite context doc

sdf doctor                    Check that dependencies are available
sdf version                   Print version
sdf help                      Show help
```

## How sync works

1. Polls GitHub for PR state via `gh`
2. Walks merged nodes from the bottom of the stack upward
3. Rebases each unmerged branch onto its new base
4. If conflicts arise, invokes Claude with assembled stack context for resolution
5. Force-pushes updated branches and runs `gh pr edit --base` to fix PR diffs
6. Updates `.sdf/stack.json`

## License

MIT
