# The Rebase That Broke the AI: Why Stacked Diffs Need Topology Awareness

A real conversation with Claude Code exposed a fundamental problem with manual rebasing of stacked branches. Here's the story, the test that captures it, and why `git rebase --onto` knowledge shouldn't live in your head.

## The Incident

A developer had two stacked branches for a signed-IDs feature:

```
main ← distributed-signed-ids-engine ← distributed-signed-ids-wiring
```

The engine branch got review feedback and was rebased onto an updated main. Time to rebase the wiring branch too. Claude Code's first attempt:

```bash
git rebase distributed-signed-ids-engine
```

It blew up immediately:

```
warning: skipped previously applied commit cf13d896b8da
warning: skipped previously applied commit f8395d46bcf0
CONFLICT (content): Merge conflict in Gemfile
CONFLICT (content): Merge conflict in Gemfile.lock
CONFLICT (content): Merge conflict in CODEOWNERS
...
```

**28 lines of conflicts.** Not because the wiring code conflicted with anything -- because git tried to replay the shared engine commits that were already in the new base. The wiring branch's history included commits from the engine branch (they were stacked), and after the engine was rewritten, those shared commits collided with their own rewritten versions.

## The Manual Recovery

Claude Code had good instincts. It aborted, then reverse-engineered the topology:

```bash
# What commits are wiring-only?
git log --oneline distributed-signed-ids-wiring \
  --not distributed-signed-ids-engine
```

```
afc5a7f fix(TPM-1283): update wiring branch after engine PR review changes
0f9c1e9 feat(TPM-1283): wire distributed signed IDs into SignedIdGenerator and SignedIdParser
8f94b00 fix(TPM-1283): remove version constraint from doctolib_signed-ids gem
f8395d4 fix(TPM-1283): address review comments on engine
cf13d89 feat(TPM-1283): engine core implementation
3a2b1c0 refactor(TPM-1283): extract signing logic
```

Bottom 4 commits: shared with engine. Top 2: wiring-specific. The fix:

```bash
git rebase --onto distributed-signed-ids-engine \
  8f94b007d3e6 \
  distributed-signed-ids-wiring
```

This replays only the 2 wiring-specific commits on top of the rewritten engine. Clean. No conflicts.

**Total time: 3 minutes 8 seconds** of an AI reasoning through git topology, making a mistake, diagnosing it, and recovering.

## What SDF Does Instead

```bash
sdf sync
```

That's it. SDF tracks each branch's `BaseTip` -- the SHA of the parent branch at the time the child was last synced. When it detects that the parent has moved, it computes the `--onto` rebase automatically:

```go
git rebase --onto <parent> <node.BaseTip> <branch>
```

The `BaseTip` is the old base. The current parent is the new base. Only commits between `BaseTip` and the branch tip are replayed. No shared-commit conflicts. No manual `git log --not`. No counting commits.

## The E2E Test

We wrote `TestE2E_SyncAfterUpstreamBranchRebased` to codify this exact scenario. Here's the structure:

### Phase 1: Build the stack

```
main ← engine [2 commits] ← wiring [2 commits]
```

Both branches get PRs via `sdf pr`.

### Phase 2: Rewrite the upstream branch

The engine branch gets new commits (simulating review feedback). This is the trigger -- the wiring branch's `BaseTip` now points at the old engine tip, but the engine has moved forward.

### Phase 3: Sync

```bash
sdf sync -y
```

SDF detects the stale `BaseTip`, computes the `--onto` rebase, and cascades.

### Phase 4: Verify correctness (7 assertions)

This is where the test earns its keep. Each assertion targets a specific failure mode:

| # | Assertion | What it catches |
|---|-----------|----------------|
| 4a | Both PRs still OPEN | Sync accidentally closed a PR |
| 4b | New engine tip is ancestor of wiring | Wiring wasn't rebased onto updated engine |
| 4c | Old engine tip is NOT ancestor of wiring | Wiring still sitting on stale history |
| 4d | Wiring-specific files exist | Wiring commits were lost during rebase |
| 4e | Engine review files exist in wiring | Rebase didn't pick up engine changes |
| 4f | Stack topology unchanged | SDF's internal state got corrupted |
| 4g | Exactly 2 commits ahead | Shared commits were duplicated |

Assertion **4g** is the one that catches the original failure mode. If SDF had naively run `git rebase` instead of `git rebase --onto`, the wiring branch would show 6 commits ahead of engine (4 duplicated shared commits + 2 wiring commits) instead of 2.

Assertion **4c** is the sneakiest. After a rewrite, the old engine tip should NOT be in the wiring branch's ancestry. If it is, the wiring branch wasn't actually rebased -- it's still sitting on the old history. This catches a subtle bug where `sdf sync` might detect the branch as "already up to date" by checking ancestry in the wrong direction.

## Why This Matters

The manual recovery took 3 minutes of an experienced AI assistant reasoning through git internals. For a human, this is a 10-15 minute detour that requires:

1. Knowing that `git rebase` replays all commits, including shared ones
2. Knowing the `--not` syntax for `git log` to find branch-specific commits
3. Identifying the correct boundary commit for `--onto`
4. Remembering `--force-with-lease` for the push

Each of those is a decision point where things can go wrong. The `--onto` boundary commit is especially dangerous -- pick the wrong one and you either lose commits or duplicate them.

SDF encodes this knowledge in the `BaseTip` tracking. Every branch remembers where its parent was when it was last synced. The rebase math is always:

```
git rebase --onto <current-parent-tip> <recorded-BaseTip> <branch>
```

No manual commit archaeology. No counting. No mistakes.

## Running the Test

```bash
# Prerequisites
export SDF_E2E_REPO=/path/to/cloned/sandbox-repo
export GH_TOKEN=ghp_...

# Build and test
go build -o sdf .
go test -tags e2e -v -count=1 -run TestE2E_SyncAfterUpstreamBranchRebased ./e2e/...
```

The test creates real branches and PRs against a GitHub repository, verifies the cascade rebase, and cleans up after itself. Recordings are saved to `e2e/testdata/recordings/` for debugging.

## The Takeaway

Stacked branches create topology relationships that plain git doesn't track. When an upstream branch is rewritten, every downstream branch needs a topology-aware rebase -- not a naive one. The difference is `git rebase` (which replays shared history and explodes) vs `git rebase --onto` (which replays only branch-specific commits).

SDF's `BaseTip` tracking turns a manual git archaeology exercise into a single command. The e2e test ensures it stays that way.
