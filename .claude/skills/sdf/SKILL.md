---
name: sdf-stacked-diffs
description: Use when creating branches, rebasing, creating PRs, or merging in any repository that uses SDF stacked diffs — replaces raw git/gh commands with sdf equivalents to maintain stack integrity
---

# SDF — Stacked Diffs Flow

SDF manages stacked PRs — chains of dependent pull requests where each PR builds on the previous one. It handles branch topology, cascade rebasing when PRs merge, and keeps PR bases correct on GitHub.

## Rules

For any branch that is part of an SDF stack:

- **`sdf branch <name>`** instead of `git checkout -b` — registers the branch in the stack and sets the correct base
- **`sdf sync`** instead of `git rebase` — cascade-rebases the entire stack and updates PR bases
- **`sdf pr`** instead of `gh pr create` — sets the correct PR base branch and adds navigation links
- **`sdf merge`** instead of `gh pr merge` — retargets the next PR and syncs the remaining stack
- After amending an earlier branch, run **`sdf sync`** to cascade changes forward

## Quick Reference

| Command | Purpose |
|---------|---------|
| `sdf branch <name>` | Add a new branch to the current stack |
| `sdf config` | Manage sdf configuration |
| `sdf doctor` | Check that dependencies are available |
| `sdf fetch` | Discover and sync PR stacks from GitHub |
| `sdf init [stack-name]` | Create a new stack and its first branch |
| `sdf merge` | Merge head PR and sync remaining branches |
| `sdf move <commit>...` | Move commits from current branch to parent |
| `sdf pr` | Create a GitHub PR for the current branch |
| `sdf register` | Discover and sync PR stacks from GitHub |
| `sdf status [stack-name]` | Show stack topology and sync state |
| `sdf switch [branch]` | Switch to a branch and report its stack |
| `sdf sync [stack-name]` | Detect merged PRs and cascade-rebase downstream branches |

## When to Run What

- **Starting a session:** `sdf ls` to see all tracked stacks, then `sdf status` on the relevant stack for details.
- **Before creating a branch:** `sdf status` to confirm you're on the right branch (new branch inserts after current position).
- **After pushing changes to an earlier branch:** `sdf sync` to cascade.
- **After a PR is merged on GitHub:** `sdf sync` to rebase remaining branches onto the new base.
