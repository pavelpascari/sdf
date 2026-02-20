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

## Splitting a Branch into a New Branch

When you want to extract commits into a **new** branch (not the existing parent), combine `sdf branch` with `sdf move`:

### Pattern: Extract work into a new child branch

```bash
# Current stack: main ← feature/auth [a1, a2, a3, a4, a5]
# You realize a4 and a5 are a separate concern

# 1. Create a new branch (appended to stack)
sdf branch api-layer

# 2. Switch back to the source branch
sdf switch feature/auth

# 3. Move the newer commits to the new branch
#    (move works parent→child direction, so we need a different approach)
```

This pattern doesn't work directly because `sdf move` only moves commits **down** (to the parent). To move commits **up** (to a child), use git directly:

```bash
# 1. Create the new branch at the current tip
sdf branch api-layer
# api-layer now has commits a1-a5 (same as feature/auth)

# 2. Reset feature/auth to drop the commits you want to move
git checkout feature/auth
git reset --hard HEAD~2  # drops a4, a5

# 3. Force-push the trimmed branch
git push --force-with-lease

# 4. Sync to update the stack state
sdf sync
```

### Pattern: Split current work into parent PR

This is the primary `sdf move` use case — you've been working on a branch and realize the first few commits should be their own PR:

```bash
# Stack: main ← feature/auth [setup, schema, handlers, tests]
# You want "setup" and "schema" to be a separate PR

# 1. Create a branch before feature/auth for the foundation work
#    (requires stack-resilience/insert-branch — not yet implemented)
#    For now, use manual approach:

# Option A: Move commits to the existing parent
sdf move setup schema
# Moves "setup" and "schema" to main (or whatever the parent is)
# feature/auth now only has [handlers, tests]

# Option B: Create a new branch, reorder the stack manually
# (Advanced — involves editing .sdf/stacks/*.json)
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
