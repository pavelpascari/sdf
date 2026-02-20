# Case Study: Squash-Merge Conflict Cascade

**Date:** 2026-02-20
**Stack:** `pr-dx-cleanup` (5 branches, PRs #21-#26)
**Outcome:** All PRs merged, but required ~4 rounds of manual conflict resolution

## What Happened

The `pr-dx-cleanup` stack had 5 branches stacked linearly:

```
main ← PR#21 ← PR#22 ← PR#23 ← PR#25 ← PR#26
       cleanup  pr-create pr-update  research  terminal-ui
```

PRs #21, #22, and #23 were squash-merged on GitHub in quick succession (without running `sdf sync` between each). Then `sdf sync` was run to rebase the remaining branches (#25, #26) onto the updated main.

The rebase of `terminal-ui` (PR #26) hit massive conflicts: 7 conflict blocks in `cmd/sync.go`, plus conflicts in `cmd/sync_test.go` and `main.go`. Each commit being replayed conflicted because its context lines referenced the old incremental history that no longer existed in the squashed main.

After resolving and continuing, the rebase hit conflicts again on the next commit. This repeated ~4 times until all 5 commits were replayed.

## Root Cause

**Squash merge destroys incremental history.**

When PRs are squash-merged, each PR's incremental commits (a, b, c, d, ...) are compressed into a single commit per PR. Downstream branches were written on top of the incremental commits, so their context lines reference code states that don't exist in the squashed version.

```
Before merge:
  main ← [a,b,c] ← [d,e] ← [f,g] ← [h,i] ← [j,k]
                                       ↑
                          j,k were written with context
                          from a,b,c,d,e,f,g (incremental)

After squash-merging first 3 PRs:
  main [ABC, DE, FG]  ← [h,i] ← [j,k]
       ↑                    ↑
       completely different  still expects incremental
       SHAs and diffs        context from a,b,c,...
```

When git rebases `[j,k]` onto the new main, it can't match context lines. Every file touched by both the squashed PRs and the downstream commits conflicts.

**The severity scales with the number of PRs merged simultaneously.** Crossing one squash boundary produces small, auto-resolvable conflicts. Crossing three boundaries at once produces a cascade of semantic mismatches.

## The Fix: Merge One at a Time

```
1. Squash-merge PR#21  →  sdf sync  (rebases #22-#26 across ONE boundary)
2. Squash-merge PR#22  →  sdf sync  (rebases #23-#26 across ONE boundary)
3. Squash-merge PR#23  →  sdf sync  (rebases #25-#26 across ONE boundary)
4. Squash-merge PR#25  →  sdf sync  (rebases #26 across ONE boundary)
5. Squash-merge PR#26  →  done
```

Each rebase only crosses one squash boundary. Git can usually auto-resolve these because the semantic diff is small and localized.

## Proposed Solution: `sdf merge`

A new command that enforces the safe pattern:

```
sdf merge
```

- Merges the **head** (bottom-most open) PR in the stack via `gh pr merge --squash`
- Automatically runs `sdf sync` afterward to cascade-rebase the rest
- Only merges one PR at a time — the user runs `sdf merge` repeatedly to drain the stack
- No `--all` flag. Deliberate one-at-a-time to keep conflicts small.

This makes the safe pattern the default path, so users never accidentally batch-merge and hit the cascade.

## Lessons for sdf Design

1. **sdf is a thin wrapper around git** — it doesn't replace git, it guides the user toward safe patterns
2. **`sdf sync --continue` should be minimal** — when conflicts happen, the user resolves with git directly, then re-runs `sdf sync`
3. **Squash merge is the user's choice** — sdf supports it but must make the safe workflow (merge-one-sync-repeat) the path of least resistance
4. **Never `git add .` during conflict resolution** — only stage the specific conflicted files to avoid pulling in untracked files
