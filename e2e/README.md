# E2E Tests

End-to-end tests that exercise sdf against a real GitHub repository,
creating real branches, PRs, and performing real merge/sync operations.

## Prerequisites

1. **Sandbox repository**: A dedicated GitHub repo for E2E tests.
   Create one (private is fine):
   ```
   gh repo create pavelpascari/sdf-test-sandbox --private --clone
   cd sdf-test-sandbox
   echo "# sdf E2E sandbox" > README.md
   git add README.md && git commit -m "init" && git push
   ```

2. **GitHub PAT** with `repo` scope.
   Create one at https://github.com/settings/tokens and store it:
   - Locally: `export GH_TOKEN=ghp_...`
   - In CI: Add as repository secret `SDF_E2E_TOKEN`

3. **sdf binary** built:
   ```
   make build
   ```

## Running locally

```bash
# Clone the sandbox repo
gh repo clone pavelpascari/sdf-test-sandbox /tmp/e2e-sandbox
cd /tmp/e2e-sandbox
git config user.email "test@test.com"
git config user.name "Test"

# Run E2E tests
export SDF_E2E_REPO=/tmp/e2e-sandbox
export SDF_BIN=$(pwd)/../sdf/bin/sdf
go test -tags e2e -v -count=1 ./e2e/...

# To also run Claude-dependent tests (requires claude CLI):
go test -tags e2e -v -count=1 ./e2e/... -args -with-claude
```

## Running in CI

E2E tests run via `.github/workflows/e2e.yml`:

- **Manual dispatch**: Actions → E2E Tests → Run workflow
- **Weekly schedule**: Monday 6am UTC
- **PR label**: Add `run-e2e` label to a PR

The workflow requires the `SDF_E2E_TOKEN` secret to be set.

## Test structure

| Test | What it verifies |
|------|-----------------|
| `TestE2E_FullStackLifecycle` | init → branch → pr → sync round-trip |
| `TestE2E_MergeRetargetOrdering` | Downstream PR survives head merge (retarget-before-delete) |
| `TestE2E_MergeThenSync` | Post-merge sync rebases remaining branches correctly |

## Cleanup

Tests clean up after themselves (branches + PRs). If a test fails mid-run,
stale branches may remain. Clean them up with:

```bash
cd /tmp/e2e-sandbox
git fetch --prune
# Delete any leftover e2e branches
git branch -r | grep 'origin/e2e-' | sed 's|origin/||' | xargs -I{} git push origin --delete {}
# Close any leftover PRs
gh pr list --state open | grep 'e2e-' | awk '{print $1}' | xargs -I{} gh pr close {}
```
