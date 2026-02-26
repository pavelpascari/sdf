# E2E Scenario Plan

## Goal

Define and prioritize end-to-end scenarios for the stack and split workflows, with clear assertions and rollout order.

## Current Coverage (Keep as P0)

1. `TestE2E_FullStackLifecycle`
- Validates `init -> branch -> pr -> sync` lifecycle.
- Checks branch chain creation, PR creation, PR base chaining, and in-sync behavior.

2. `TestE2E_MergeRetargetOrdering`
- Validates retarget-before-merge ordering.
- Ensures downstream PR remains open after head merge.

3. `TestE2E_MergeThenSync`
- Validates merge followed by automatic sync behavior.
- Ensures remaining branches/PRs are coherent after merge.

4. `TestE2E_SyncAfterBaseAdvances`
- Validates cascade rebase after `main` advances.
- Ensures new base tip is reachable from all stack branches.

5. `TestE2E_InsertBranchMidStack`
- Validates insertion into middle of stack.
- Ensures PR base retargeting and downstream rebase chain correctness.

6. `TestE2E_RecordAndValidate`
- Validates spy recordings and fake-response structural compatibility.
- Ensures recorded GH responses remain parseable by production structs.

## Additional Scenarios

### P1 (Next)

1. E2E preflight: spy-enabled binary contract
- Fail fast when `SDF_BIN` is not spy-instrumented.
- Assertion: `gh_sdf.jsonl` is created immediately after first GH call via `sdf`.

2. Sync conflict path
- Create intentional conflict between updated base and stack branch.
- Assertions:
- `sdf sync` exits with a clear error.
- Stack metadata remains consistent.
- No silent partial-success corruption.

3. Partial remote failure path
- Simulate GH update failure during sync/PR edit.
- Assertions:
- Command fails with actionable message.
- Successful prior operations remain persisted.
- Rerun can recover without manual repair.

4. Idempotency and retry
- Rerun `sdf pr` on a branch with existing PR.
- Rerun `sdf sync` immediately after successful sync.
- Assertions:
- No duplicate PR creation.
- Second sync is a no-op.
- Stack state remains unchanged.

### P2 (After P1)

1. `sdf split` end-to-end happy path
- Analyze -> execute path produces valid stack and preserves tree identity.

2. `sdf split` refine flow
- Refine in Claude session -> re-extract -> execute.
- Assertions:
- Refined plan is persisted.
- Executed branch chain matches refined plan.

3. `sdf split` hunk assignment fallback
- Force hunk assignment failure after refine.
- Assertions:
- Plan falls back via shared-file deduplication.
- Structural refinements are preserved.
- Fallback plan is persisted.

4. `sdf split --no-push`
- Assertions:
- Local stack/branches created.
- No remote branches/PRs created.
- User-facing next-step guidance is correct.

## Implementation Order

1. Keep current P0 suite stable and mandatory.
2. Implement all P1 scenarios and make them mandatory.
3. Add P2 split scenarios as opt-in first (`with-claude` / dedicated gate).
4. Promote stable P2 scenarios to mandatory.

## Operational Notes

1. `make test-e2e` must always run with the just-built binary (`SDF_BIN` defaulted to `bin/sdf`).
2. Per-test recording artifacts (`full.jsonl`, tool-specific JSONL) are required for failure triage.
3. Test cleanup should remain best-effort and not hide primary failures.
