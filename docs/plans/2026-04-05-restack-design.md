# sdf restack — Reorder Branches Within a Stack

**Issue:** #199
**Date:** 2026-04-05

## Problem

When a branch needs to be moved to a different position in the stack, there is
no built-in command. Users must manually `git rebase --onto`, cascade-rebase
downstream branches, edit the stack JSON, and force-push — error-prone and
tedious.

## Command

```
sdf restack <branch> --after <target-branch>
sdf restack --continue
```

Moves `<branch>` to the position immediately after `<target-branch>` in the
stack. `<target-branch>` can be any branch in the same stack, or the stack's
base branch (e.g. `main`) to make it first.

`--continue` resumes after conflict resolution (same pattern as `sdf sync`).

## Algorithm

Given `main ← A ← B ← C ← D`, running `sdf restack C --after A`:

1. **Fetch and verify sync**: fetch from origin, reconcile PR states, and
   verify the stack is fully synced (no pending rebases, no local/remote
   divergence). If the stack is not in sync, offer to run `sdf sync` first
   (single y/n prompt). If the user declines, abort. This prevents restacking
   from a stale state where history rewriting could conflict with unpushed
   or unfetched changes.
2. **Validate**: both branches in same stack, target is not source, working
   tree clean, move changes position (not a no-op).
3. **Reorder**: rearrange Nodes array `[A, B, C, D]` → `[A, C, B, D]`.
4. **Diff parents**: compare old vs new parent for each node. Only branches
   whose effective parent changed need rebasing:
   - C: parent B → A (changed)
   - B: parent A → C (changed)
   - D: parent C → B (changed, cascade)
5. **Rebase in new array order**: for each affected branch, rebase onto its
   new parent. On conflict: offer Claude resolution, then manual pause
   (`sdf restack --continue`), skip, or abort.
6. **Push** all rebased branches (force-push, history rewritten).
7. **Update PR bases** on GitHub for branches whose parent changed and have a PR.
8. **Update PR navigation** for all PRs in the stack.
9. **Save** stack JSON with updated `BaseTip` values.

## Edge Cases

- Stack not in sync with remote → offer to run sync first, abort if declined.
- Moving to same position or after immediate predecessor → no-op, print message.
- Moving after self → error.
- Branch not in stack → error.
- Merged/closed nodes between old and new positions → stay in array, skipped
  by `ParentBranch` as usual.
- `--after` is the stack base branch → branch becomes first node in stack.

## Conflict Handling

Same pattern as `sdf move`: pause at conflict, offer Claude resolution attempt,
fall back to manual resolution. Progress saved to `.sdf/local.json` so
`sdf restack --continue` can resume from the failed branch.

## Output

```
Restacking C after A in stack my-stack...

Restack plan:
  → rebase C onto A
  → rebase B onto C
  → rebase D onto B (cascade)

  rebasing C onto A...
  ✓ C rebased and pushed
  rebasing B onto C...
  ✓ B rebased and pushed
  rebasing D onto B...
  ✓ D rebased and pushed

Restack complete.

  PR #3 base updated → A
  PR #2 base updated → C
  PR #4 base updated → B

Updated 3 PR description(s).
```

## Files

- New: `cmd/restack.go` — command implementation
- New: `cmd/restack_test.go` — tests
- Modify: `main.go` — register the command

## Testing

- Validate: error cases (not in sync, not in stack, same position, dirty tree, after self)
- Reorder logic: array manipulation produces correct new order
- Parent diff: correctly identifies which branches need rebasing
- Rebase: branches end up on correct parents with correct commits
- Push: all affected branches pushed
- PR base update: GitHub PR bases match new parents
- Cascade: downstream branches beyond the moved region are rebased
- Conflict: pause and resume via --continue
