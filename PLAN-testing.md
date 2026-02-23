# sdf Testing & Validation Plan

## The Core Problem

sdf orchestrates three external tools via `exec.Command`:

| Dependency | Current test coverage | Gap |
|---|---|---|
| **git** | Good — temp repos in `t.TempDir()` | No remote (origin) testing |
| **gh** (GitHub CLI) | None — tests check `ghpkg.Available()` and skip | All PR operations untested |
| **claude** (Claude CLI) | None | Conflict resolution & context generation untested |

All three packages (`internal/git`, `internal/gh`, `internal/claude`) call `exec.Command` directly in package-level functions — there are no interfaces to swap in test doubles.

## Do We Need a Public Repo?

**No.** A private repo works equally well for E2E testing. The only requirement is a GitHub PAT (Personal Access Token) with repo scope, stored as a GitHub Actions secret. A public repo would only be needed if you wanted external contributors to fork and run the full E2E suite — which isn't a current need.

However, a **dedicated test repo** (separate from `pavelpascari/sdf`) is strongly recommended for E2E tests, so they can freely create/delete branches and PRs without polluting the real repo.

## Proposed Testing Layers

### Layer 1: Unit Tests (existing — strengthen)

**What:** Pure logic tests with real git repos in temp dirs. No network, no GitHub, no Claude.

**Current state:** 151 tests, covering `computeSyncPlan`, init, branch, move, register, config, stack topology.

**Gaps to fill:**

1. **Introduce interfaces for `gh` and `claude` packages.** This is the single most impactful change. Replace direct `exec.Command` calls with interfaces that can be stubbed in tests:

```go
// internal/gh/gh.go
type Client interface {
    PRList(branches []string) ([]PRInfo, error)
    PRCreate(title, body, base, head string) (string, error)
    PREditBase(prNumber int, newBase string) error
    PRMerge(prNumber int, method string) error
    PRView(branch string) (*PRInfo, error)
    PRViewBody(prNumber int) (string, error)
    PREditBody(prNumber int, body string) error
    PREditTitle(prNumber int, title string) error
    Available() bool
    // ... etc
}

// CLIClient implements Client by shelling out to gh
type CLIClient struct{}

func (c *CLIClient) PRList(branches []string) ([]PRInfo, error) { ... }
```

Similarly for `claude`:

```go
// internal/claude/claude.go
type Client interface {
    RunPrompt(sessionName, prompt string) (string, error)
    RunPromptStreaming(name, prompt string, display io.Writer) (string, error)
    Available() bool
}
```

And for `git` (selectively — many git operations are fine to run against temp repos, but operations like `Push`, `PushNew`, `FetchAll`, `FetchBranch`, `LSRemoteRef` need a remote):

```go
// internal/git/remote.go
type Remote interface {
    Push(branch string) error
    PushNew(branch string) error
    FetchAll() error
    FetchBranch(branch string) error
    LSRemoteRef(ref string) (string, error)
    FastForward(branch string) error
}
```

2. **Test `executeSyncPlan` (not just `computeSyncPlan`).** The plan computation is well tested, but plan execution — which actually calls rebase, push, PR edit — is untested. With interfaces, we can test the full execute path with mock gh/claude.

3. **Test conflict resolution flow.** With a mock claude client, verify that:
   - Conflicted files are detected
   - The right prompt is constructed (with PR description and upstream diff)
   - Resolved files are staged and rebase continues
   - Failed resolution triggers abort

4. **Test `sdf pr` command.** With a mock gh client, verify PR creation with correct title, body, base, head.

5. **Test `sdf merge` command.** Verify correct merge method, branch cleanup, stack pruning.

### Layer 2: Integration Tests with Fake Binaries (new)

**What:** Replace `gh` and `claude` with small shell scripts on `$PATH` that record invocations and return canned JSON responses. Tests exercise the real code paths including `exec.Command` — no interface changes needed.

**Why both this AND interfaces?** This layer tests that the actual CLI argument construction and JSON parsing work correctly — interfaces mock at a higher level and can miss serialization bugs.

**Implementation:**

```go
// internal/testutil/fakebin.go
package testutil

// InstallFakeBin creates a shell script at dir/name that logs
// its arguments to dir/name.log and writes a canned response to stdout.
func InstallFakeBin(t *testing.T, dir, name, stdout string) {
    script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %s/%s.log
cat << 'RESPONSE'
%s
RESPONSE
`, dir, name, stdout)
    path := filepath.Join(dir, name)
    os.WriteFile(path, []byte(script), 0755)
    t.Setenv("PATH", dir + ":" + os.Getenv("PATH"))
}
```

**Test scenarios:**

- `sdf sync` with fake `gh` returning PR JSON → verify correct `gh pr edit --base` calls
- `sdf sync` with fake `claude` returning resolved file contents → verify conflict flow
- `sdf pr` with fake `gh` → verify `gh pr create` arguments
- `sdf merge` with fake `gh` → verify `gh pr merge` arguments
- `sdf doctor` → verify version checks against fake binaries

**Advantages:**
- No network, no tokens, fully deterministic
- Tests real argument serialization and output parsing
- Works in CI without any secrets
- Catches regressions in CLI argument changes (e.g., if `gh` changes flags)

### Layer 3: End-to-End Tests with a Real GitHub Repo (new)

**What:** Full workflow tests that create real branches, real PRs, and perform real syncs against a dedicated GitHub test repository.

**Setup:**
- Create a dedicated repo: `pavelpascari/sdf-test-sandbox` (private is fine)
- Store a PAT as GitHub Actions secret `SDF_E2E_TOKEN`
- Run E2E tests in a separate CI job, gated behind a label or schedule

**Implementation:**

```yaml
# .github/workflows/e2e.yml
name: E2E Tests

on:
  # Run on demand, not on every PR (they're slow and consume API quota)
  workflow_dispatch:
  # Weekly schedule as a regression check
  schedule:
    - cron: '0 6 * * 1'  # Monday 6am UTC
  # Or: run on PRs with a specific label
  pull_request:
    types: [labeled]

jobs:
  e2e:
    if: >
      github.event_name != 'pull_request' ||
      contains(github.event.pull_request.labels.*.name, 'run-e2e')
    runs-on: ubuntu-latest
    env:
      GH_TOKEN: ${{ secrets.SDF_E2E_TOKEN }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: Build sdf
        run: make build

      - name: Clone test sandbox
        run: gh repo clone pavelpascari/sdf-test-sandbox /tmp/sandbox

      - name: Run E2E suite
        run: go test -tags e2e -v -count=1 ./e2e/...
        env:
          SDF_E2E_REPO: /tmp/sandbox
```

**Test scenarios (build tags: `//go:build e2e`):**

Each test uses a unique branch prefix (timestamp + random) to avoid collisions:

1. **Full stack lifecycle:**
   - `sdf init` → creates stack and first branch
   - `sdf branch` → adds 2 more branches with commits
   - `sdf pr` → creates 3 PRs with correct base chains
   - Verify PRs exist on GitHub with correct base branches
   - `sdf status` → shows correct topology

2. **Sync after merge:**
   - Merge head PR via `gh pr merge`
   - `sdf sync` → rebases downstream, updates PR bases
   - Verify PR bases updated on GitHub

3. **Conflict resolution (if claude is available):**
   - Create conflicting changes
   - `sdf sync` → detects conflict
   - Verify conflict resolution flow (or graceful abort if no claude)

4. **Cleanup:** Each test cleans up its branches and PRs in `t.Cleanup()`

### Layer 4: Property-Based / Invariant Tests (new)

**What:** Generate random sequences of stack operations and verify invariants always hold.

**Implementation using `testing/quick` or a library like `pgregory.net/rapid`:**

```go
func TestStackInvariants(t *testing.T) {
    rapid.Check(t, func(t *rapid.T) {
        // Generate random stack operations
        ops := rapid.SliceOf(rapid.SampledFrom([]string{
            "add-branch", "merge-head", "merge-middle",
            "add-commit-to-base", "add-commit-to-branch",
        })).Draw(t, "ops")

        s := createInitialStack(t)
        for _, op := range ops {
            applyOp(s, op)
            plan := computeSyncPlan(s, nil)

            // INVARIANTS that must always hold:
            // 1. No rebase action targets a merged branch
            // 2. Rebase order respects topology (parent before child)
            // 3. Every rebase has a corresponding push
            // 4. skip-merged only appears for merged nodes
            assertInvariants(t, s, plan)
        }
    })
}
```

**Key invariants to verify:**
- Stack JSON always round-trips cleanly (save → load → save = identical)
- `computeSyncPlan` never produces actions for merged branches
- Rebase ordering always respects parent-before-child
- After sync execution, all `BaseTip` values match actual git state
- `ParentBranch()` always skips merged nodes correctly
- Branch names always have the correct prefix

### Layer 5: Golden File / Snapshot Tests (new)

**What:** Capture the output of commands (`sdf status`, `sdf sync --dry-run`) and compare against checked-in golden files.

**Implementation:**

```go
func TestStatusOutput_ThreeBranches(t *testing.T) {
    dir := syncTestRepo(t)
    // ... setup stack state

    var buf bytes.Buffer
    runStatusTo(&buf, dir)
    actual := stripANSI(buf.String())

    golden := filepath.Join("testdata", "status_three_branches.golden")
    if *update {
        os.WriteFile(golden, []byte(actual), 0644)
        return
    }

    expected, _ := os.ReadFile(golden)
    if diff := cmp.Diff(string(expected), actual); diff != "" {
        t.Errorf("output mismatch (-want +got):\n%s", diff)
    }
}
```

Run with `-update` flag to regenerate golden files after intentional changes.

**Benefits:** Catches unintentional output regressions, documents expected behavior.

---

## Implementation Priority

| Priority | Layer | Effort | Impact | Needs secrets/network? |
|---|---|---|---|---|
| **P0** | Fake binaries (Layer 2) | Low | High — covers the biggest gap (gh/claude interactions) with zero infra | No |
| **P1** | Interfaces + unit tests (Layer 1) | Medium | High — enables fast, precise testing of command logic | No |
| **P2** | Golden file tests (Layer 5) | Low | Medium — catches output regressions | No |
| **P3** | E2E tests (Layer 3) | Medium | High — catches real integration issues | Yes (PAT + sandbox repo) |
| **P4** | Property tests (Layer 4) | Low | Medium — catches edge cases in sync logic | No |

## Recommended Implementation Order

### Phase 1: Fake Binaries + Integration Tests (no code changes to sdf itself)

1. Create `internal/testutil/fakebin.go` helper
2. Write integration tests for `sync`, `pr`, `merge`, `doctor` using fake `gh`/`claude`
3. Add to existing `make test` — runs in CI immediately

### Phase 2: Interface Extraction

1. Define `gh.Client` interface, make existing functions methods on `CLIClient`
2. Define `claude.Client` interface similarly
3. Thread clients through commands (dependency injection via command context or struct fields)
4. Write focused unit tests with mock clients for edge cases

### Phase 3: Output Stability

1. Add golden file infrastructure (`testdata/` dirs, `-update` flag)
2. Snapshot key outputs: `status`, `sync --dry-run`, `doctor`
3. Add to CI

### Phase 4: E2E Infrastructure

1. Create `pavelpascari/sdf-test-sandbox` repo (private)
2. Add `SDF_E2E_TOKEN` secret
3. Create `e2e/` package with build tag `e2e`
4. Write full lifecycle test
5. Add `e2e.yml` workflow (manual dispatch + weekly schedule)

### Phase 5: Property Tests

1. Add `rapid` or use `testing/quick`
2. Write invariant tests for `computeSyncPlan`
3. Write round-trip tests for stack JSON

---

## Summary

**You do not need a public repo.** The most impactful improvements (fake binaries, interfaces, golden files, property tests) need zero infrastructure. E2E tests need a dedicated sandbox repo (private is fine) and a PAT — but they should be the last thing you build, not the first.

The fake binary approach (Layer 2) gives you the biggest bang for the buck: it tests real `exec.Command` paths, real argument serialization, and real JSON parsing — without any network, tokens, or external dependencies. Start there.
