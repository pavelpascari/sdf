# Moving Commits Between Branches

`sdf move` relocates commits from the current branch to its parent in the stack. This is useful when you realize work belongs in a different PR, or when you want to split a large branch into smaller, reviewable pieces.

## Basic Usage: Move to Parent

Move commits from the current branch down to its parent:

```bash
# You're on feature/api with commits [a1, a2, a3]
# Stack: main ← feature/auth ← feature/api

sdf move a1 a2
```

This:
1. Cherry-picks `a1` and `a2` onto `feature/auth`
2. Rebases `feature/api` to strip the moved commits
3. Cascade-rebases any downstream branches
4. Updates the stack state

**Constraints:**
- Commits must be a contiguous prefix (oldest-first) of the branch
- At least one commit must remain on the source branch
- Working tree must be clean

## Splitting a Branch: Move to Parent

This is the primary `sdf move` use case — you've been working on a branch and realize the first few commits should be their own PR:

```bash
# Stack: main ← feature/auth [setup, schema, handlers, tests]
# You want "setup" and "schema" in the parent branch

sdf move setup schema
# Moves "setup" and "schema" to the parent branch
# feature/auth now only has [handlers, tests]
```

## Caveat: Moving Commits Up (to a Child)

`sdf move` currently only moves commits **down** to the parent branch. Moving commits **up** to a child branch is not yet supported natively.

> **Tracking issue:** [#149](https://github.com/pavelpascari/sdf/issues/149) — follow for updates on upward move support.

For now, you can work around this with git directly:

```bash
# 1. Create the new branch at the current tip
sdf branch api-layer
# api-layer now has the same commits as the source branch

# 2. Reset the source branch to drop the commits you want in the child
git checkout feature/auth
git reset --hard HEAD~2  # drops the last 2 commits

# 3. Force-push the trimmed branch
git push --force-with-lease

# 4. Sync to update the stack state
sdf sync
```

## How It Works Internally

```
Before:
  parent [p1, p2]  ←  current [c1, c2, c3]  ←  downstream [d1]

sdf move c1 c2:

Phase 1: Cherry-pick c1, c2 onto parent
  parent [p1, p2, c1, c2]

Phase 2: Rebase current to strip c1, c2
  parent [p1, p2, c1, c2]  ←  current [c3]

Phase 3: Cascade-rebase downstream
  parent [p1, p2, c1, c2]  ←  current [c3]  ←  downstream [d1]

Phase 4: Save stack state (updated BaseTips)
```

If conflicts occur during cherry-pick or rebase, Claude is invoked for auto-resolution. If that fails, the operation aborts cleanly and the user can resolve manually.

## Tips

- **Move early, move often.** It's easier to move 1-2 commits than to untangle 10.
- **Commits must be contiguous.** If you need to move non-adjacent commits, use `git rebase -i` to reorder them first, then `sdf move`.
- **Push after moving.** `sdf move` updates local branches but doesn't push. Run `sdf sync` afterward to push all changes and update PR bases.
- **Check with `sdf status`** after moving to verify the stack looks correct.
