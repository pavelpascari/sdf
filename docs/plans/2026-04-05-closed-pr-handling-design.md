# Handle Closed Mid-Chain PRs During Sync

**Issue:** #211
**Date:** 2026-04-05

## Problem

When a PR in the middle of a stack is closed on GitHub, `sdf sync` does not
skip the closed node. The local stack metadata becomes stale — downstream
branches still treat the closed branch as their parent, and sync attempts to
rebase onto it.

## Design

Treat `"closed"` identically to `"merged"` in the sync pipeline. A closed node
is skipped during rebase planning, and downstream branches are re-parented to
the nearest open ancestor.

### What changes

Three code paths currently check for `"merged"` and need to also check `"closed"`:

1. **`stack.ParentBranch()`** (`internal/stack/stack.go`) — skip closed nodes
   when walking backwards to find the effective parent. Currently:
   `if s.Nodes[j].Status != "merged"`. Change to also skip `"closed"`.

2. **`computeSyncPlan()`** (`cmd/sync.go`) — skip closed nodes in the sync
   plan, emitting a `"skip-closed"` action (parallel to `"skip-merged"`).
   Currently only checks `node.Status == "merged"`.

3. **Sync output / status display** (`cmd/sync.go`) — render closed nodes
   distinctly from merged. Merged shows `"✓ PR #N merged"`, closed should
   show something like `"⊘ PR #N closed"` or `"- PR #N closed"`.

### What stays the same

- **Reconciliation**: `ReconcileFromPRs` and `ghStateToNodeStatus` already
  detect `CLOSED` and apply the status change. No changes needed.
- **Node retention**: Closed nodes stay in the stack JSON (like merged nodes).
  They are not removed.
- **Reopening**: If a closed PR is reopened on GitHub, the next `sdf sync`
  detects `CLOSED → OPEN`, sets status back to `"open"`, and the node
  re-enters the rebase chain automatically. No special handling needed.

### Behavioral examples

**Mid-chain close:**
```
Before: main ← A(open) ← B(closed) ← C(open)
Sync:   ParentBranch("C") skips B(closed), returns A
        C rebases onto A, PR base retargeted A
After:  main ← A(open) ← C(open, base:A)
        B remains in stack JSON as "closed"
```

**Consecutive closed nodes:**
```
Before: main ← A(open) ← B(closed) ← C(closed) ← D(open)
Sync:   ParentBranch("D") skips C, skips B, returns A
        D rebases onto A
After:  main ← A(open) ← D(open, base:A)
```

**First node closed:**
```
Before: main ← A(closed) ← B(open) ← C(open)
Sync:   ParentBranch("B") skips A(closed), returns main
        B rebases onto main
After:  main ← B(open, base:main) ← C(open, base:B)
```

**Accidental close + reopen:**
```
1. sync → B detected closed → B skipped, C rebased onto A
2. User reopens B on GitHub
3. sync → B detected open → B rebases onto A, C rebases onto B
```

### Conflict warning

Unlike merged (where the closed branch's commits are already in the base),
closing a PR means those commits are **not** in the ancestor. Downstream
branches that depended on the closed branch's changes will likely see rebase
conflicts. This is correct — the user chose to abandon that work.

To minimize surprise, sync emits a warning when skipping a closed node that
has open descendants. This is a fourth change point:

4. **Conflict warning in sync plan output** (`cmd/sync.go`) — when a closed
   node is skipped and the next active node will rebase past it, warn:
   ```
   ⊘ PR #5 (validation) closed — skipping
   ⚠ api-routes will rebase onto auth (skipping closed validation) — conflicts possible
   ```
   The warning names the downstream branch, the new target, and the skipped
   branch so the user understands what's about to happen. They can Ctrl-C
   to intervene (e.g. cherry-pick needed commits) or proceed and resolve
   conflicts via `sdf sync --continue` if they arise.

   The warning is emitted during plan display, before any rebase executes.

## Testing

- `ParentBranch` unit tests: closed nodes skipped (single, consecutive, first-in-stack)
- `computeSyncPlan` unit tests: closed nodes produce `skip-closed` action
- `computeSyncPlan` unit tests: downstream rebase after closed node produces conflict warning
- Integration: stack with closed mid-chain node syncs correctly, downstream
  branches rebase onto nearest open ancestor
- Reopening: closed → open transition restores node to rebase chain
