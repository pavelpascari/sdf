# sdf ↔ flow Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the gaps between flow's integration contract and sdf v0.4.0 — idempotent `new`/`branch`/`pr`, PR draft/ready, additive `--json` fields (`worktree_path`, `pr`, `draft`, `created`, `error_code`), a `conflicted` sync_state, a structured conflict-≠-error contract for worktree `sync`, and a distinguishable lock-timeout error.

**Architecture:** Mostly additive JSON fields + new flags on existing commands, plus one behavior change (idempotency) and the worktree-sync status contract. All idempotency/mutation continues to run under the existing `stack.WithLock`. No new top-level JSON objects — the worktree sync result extends the existing `SyncResult.Branches[]`.

**Tech Stack:** Go 1.26.x, cobra, shells to `git`/`gh`. Tests use real `git` in `t.TempDir()`; `gh` interactions use the spyrecord build tag / `ghpkg.Available()` guards.

## Global Constraints
- Module `github.com/pavelpascari/sdf`. Spec: `docs/superpowers/specs/2026-06-17-flow-integration-design.md`.
- **All JSON changes are additive** — never rename or remove an existing field (`number`, `title`, `action`, etc. stay). flow's parser keys on stable names.
- **Idempotent by default:** re-running `new`/`branch`/`pr` for an existing resource returns its current state, exit 0, with `"created": false` (vs `true` when freshly created). Existence checks run inside `stack.WithLock`.
- **Conflict ≠ error:** worktree `sync`/`--continue` report a rebase conflict as `branches[0].status:"conflicted"` with `conflicts:[paths]`, exit 0, and an EMPTY top-level `error`. `error`/`error_code` are for real failures only.
- **Headless:** flow-invoked commands never prompt.
- **Lock-timeout distinguishable:** `--json` emits `error_code:"lock_timeout"`; non-json exits `75`.
- `sync_state` enum: `in_sync | needs_sync | conflicted` (empty for base). `status` enum: `open | merged | closed`.
- Verify with `go build ./... && go test ./... -count=1` (note: pre-existing `cmd/` failures from test helpers running `git add .sdf` against gitignored `.sdf/` are environmental — verify via focused `-run` tests).
- Commit per task; Conventional Commits; body ends with:
  `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`
- Work on branch `flow-integration` (already created off `main`).

### Canonical signatures (consumed across tasks)
```go
// internal/stack/lock.go
var ErrLockTimeout = errors.New("stack lock acquire timed out")   // returned by AcquireLock on timeout

// internal/gh/gh.go
func PRCreate(title, body, base, head string, draft bool) (string, error)   // signature CHANGED: +draft
func PRReady(prNumber int) error                                            // NEW: `gh pr ready <n>`

// cmd result structs (additive fields)
type NewResult struct { Stack, Base, Branch string; WorktreePath string `json:"worktree_path,omitempty"`; Pushed, Created bool; ErrorCode string `json:"error_code,omitempty"` }
type NewBranchResult struct { Branch, Stack, Parent string; WorktreePath string `json:"worktree_path,omitempty"`; Created bool; ErrorCode string `json:"error_code,omitempty"` }
type PRResult struct { Number, Pr int; URL, Title string; Draft, Created bool; ErrorCode string `json:"error_code,omitempty"` }
// BranchResult gains: Status string `json:"status,omitempty"` (clean|noop|conflicted); Conflicts []string `json:"conflicts,omitempty"`
// SyncResult gains: ErrorCode string `json:"error_code,omitempty"`
```

---

## Task 1: `stack.ErrLockTimeout` sentinel

**Files:** Modify `internal/stack/lock.go` (`AcquireLock` timeout return). Test: `internal/stack/lock_test.go`.

**Interfaces:** Produces `stack.ErrLockTimeout` (wrapped/returned by `AcquireLock` on timeout).

- [ ] **Step 1: Write the failing test**
```go
// add to internal/stack/lock_test.go
func TestAcquireLockTimeoutReturnsSentinel(t *testing.T) {
	root := sdfRepo(t)
	l, err := AcquireLock(root, "feat", time.Second)
	if err != nil { t.Fatal(err) }
	defer l.Release()
	_, err = AcquireLock(root, "feat", 100*time.Millisecond)
	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("expected ErrLockTimeout, got %v", err)
	}
}
```
(Add `"errors"` import to the test if missing.)

- [ ] **Step 2: Run to verify it fails** — `go test ./internal/stack/ -run TestAcquireLockTimeoutReturnsSentinel -count=1` → FAIL (undefined `ErrLockTimeout`).

- [ ] **Step 3: Implement.** In `internal/stack/lock.go` add (with `"errors"` imported):
```go
// ErrLockTimeout is returned by AcquireLock when the lock cannot be acquired
// within the timeout (another sdf process holds it). Callers map it to a
// distinguishable error_code / exit code so orchestrators retry rather than escalate.
var ErrLockTimeout = errors.New("stack lock acquire timed out")
```
In `AcquireLock`, change the timeout return from the ad-hoc `fmt.Errorf("timed out ...")` to wrap the sentinel:
```go
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: %s (another sdf process may be running)", ErrLockTimeout, path)
		}
```

- [ ] **Step 4: Run to verify it passes** — `go test ./internal/stack/ -count=1` → PASS.
- [ ] **Step 5: Commit** — `feat(stack): add ErrLockTimeout sentinel for lock-acquire timeout`.

---

## Task 2: gh wrappers — `PRCreate(..., draft)` + `PRReady`

**Files:** Modify `internal/gh/gh.go` (`PRCreate` signature, new `PRReady`); update the OTHER `PRCreate` caller in `cmd/sync.go` (`promptCreateMissingPRs`). Test: `internal/gh/gh_test.go` (or the spy-based test file used for gh).

**Interfaces:**
- Produces: `PRCreate(title, body, base, head string, draft bool) (string, error)`, `PRReady(prNumber int) error`.
- Consumes: existing `run(...)` gh wrapper.

- [ ] **Step 1: Write the failing test** (use the repo's existing gh-wrapper test pattern; if gh tests shell to a fake `gh` via `Binary`, mirror it). Minimal contract test:
```go
// internal/gh/gh_test.go
func TestPRCreateDraftFlag(t *testing.T) {
	got := prCreateArgs("t", "b", "main", "feat/x", true)
	if !containsArg(got, "--draft") { t.Errorf("draft create must pass --draft: %v", got) }
	got = prCreateArgs("t", "b", "main", "feat/x", false)
	if containsArg(got, "--draft") { t.Errorf("non-draft must not pass --draft: %v", got) }
}
func TestPRReadyArgs(t *testing.T) {
	if a := prReadyArgs(42); a[0] != "pr" || a[1] != "ready" || a[2] != "42" {
		t.Errorf("pr ready args = %v", a)
	}
}
func containsArg(a []string, s string) bool { for _, x := range a { if x == s { return true } }; return false }
```
(This requires extracting the arg-building into small pure helpers `prCreateArgs`/`prReadyArgs` so they're unit-testable without invoking `gh`.)

- [ ] **Step 2: Run to verify it fails** — `go test ./internal/gh/ -run 'TestPRCreateDraftFlag|TestPRReadyArgs' -count=1` → FAIL (undefined).

- [ ] **Step 3: Implement.** In `internal/gh/gh.go`, refactor `PRCreate` to build args via a helper and accept `draft`, and add `PRReady`:
```go
func prCreateArgs(title, body, base, head string, draft bool) []string {
	args := []string{"pr", "create", "--title", title, "--body", body, "--head", head}
	if base != "" {
		args = append(args, "--base", base)
	}
	if draft {
		args = append(args, "--draft")
	}
	return args
}

// PRCreate opens a PR for head against base. When draft is true it is opened as a draft.
func PRCreate(title, body, base, head string, draft bool) (string, error) {
	return run(prCreateArgs(title, body, base, head, draft)...)
}

func prReadyArgs(prNumber int) []string {
	return []string{"pr", "ready", fmt.Sprintf("%d", prNumber)}
}

// PRReady marks a draft PR as ready for review. Idempotent: marking an
// already-ready PR is a no-op success on GitHub's side.
func PRReady(prNumber int) error {
	_, err := run(prReadyArgs(prNumber)...)
	return err
}
```
Update the OTHER caller — `cmd/sync.go` `promptCreateMissingPRs` calls `ghpkg.PRCreate(prTitle, body, base, node.Branch)`; change to `ghpkg.PRCreate(prTitle, body, base, node.Branch, false)`.

- [ ] **Step 4: Run to verify it passes** — `go test ./internal/gh/ -count=1 && go build ./...` → PASS (build confirms the sync.go caller is fixed).
- [ ] **Step 5: Commit** — `feat(gh): PRCreate --draft support and PRReady wrapper`.

---

## Task 3: `sdf new` — idempotent + `worktree_path` + `created` (J1)

**Files:** Modify `cmd/new.go` (`NewResult`, `runNewCore`). Test: `cmd/new_worktree_test.go`.

**Interfaces:** Consumes `stack.WithLock`. Produces `NewResult{..., WorktreePath, Created, ErrorCode}`.

- [ ] **Step 1: Write the failing tests**
```go
// add to cmd/new_worktree_test.go
func TestNewJSONIncludesWorktreePath(t *testing.T) {
	root := bareRepoWithClone(t)
	out, err := RunNewWithOutput([]string{"feat", "--branch", "feat/a", "--worktrees", "--json"})
	if err != nil { t.Fatal(err) }
	var r NewResult
	if e := json.Unmarshal([]byte(out), &r); e != nil { t.Fatal(e) }
	if r.WorktreePath == "" { t.Errorf("worktree_path must be set for --worktrees") }
	if !r.Created { t.Errorf("created must be true on first create") }
	_ = root
}
func TestNewIsIdempotent(t *testing.T) {
	bareRepoWithClone(t)
	if _, err := RunNewWithOutput([]string{"feat", "--branch", "feat/a", "--worktrees", "--json"}); err != nil { t.Fatal(err) }
	out, err := RunNewWithOutput([]string{"feat", "--branch", "feat/a", "--worktrees", "--json"})
	if err != nil { t.Fatalf("re-run must not error: %v", err) }
	var r NewResult
	json.Unmarshal([]byte(out), &r)
	if r.Created { t.Errorf("created must be false on re-run") }
	if r.Stack != "feat" || r.Branch != "feat/a" { t.Errorf("re-run must return existing state: %+v", r) }
}
```
(Add `"encoding/json"` import if missing.)

- [ ] **Step 2: Run to verify it fails** — `go test ./cmd/ -run 'TestNewJSONIncludesWorktreePath|TestNewIsIdempotent' -count=1` → FAIL (no `WorktreePath`/`Created`; re-run errors "already exists").

- [ ] **Step 3: Implement.** Extend `NewResult`:
```go
type NewResult struct {
	Stack        string `json:"stack"`
	Base         string `json:"base"`
	Branch       string `json:"branch"`
	WorktreePath string `json:"worktree_path,omitempty"`
	Pushed       bool   `json:"pushed"`
	Created      bool   `json:"created"`
	ErrorCode    string `json:"error_code,omitempty"`
}
```
In `runNewCore`, replace the early `if _, err := stack.LoadStack(root, stackName); err == nil { return "", fmt.Errorf("stack %q already exists ...") }` with an idempotent return: load the existing stack, find its first node, build a `NewResult{Created:false, WorktreePath: firstNode.WorktreePath, ...}` and return it (marshal+print when `jsonFlag`, else print an "already exists — returning current state" note to stderr). On the create path set `Created: true` and populate `WorktreePath` from the first node when `worktree` mode. (Populate `WorktreePath` from `node.WorktreePath` after `addWorktreeForNode`.)

- [ ] **Step 4: Run to verify it passes** — `go test ./cmd/ -run 'TestNew' -count=1 && go build ./...` → PASS.
- [ ] **Step 5: Commit** — `feat(new): idempotent re-run, worktree_path + created in JSON`.

---

## Task 4: `sdf branch` — idempotent + `created` (J2)

**Files:** Modify `cmd/branch.go` (`NewBranchResult`, `runBranch`). Test: `cmd/branch_worktree_test.go`.

**Interfaces:** Produces `NewBranchResult{..., Created, ErrorCode}` (already has `WorktreePath`).

- [ ] **Step 1: Write the failing test**
```go
func TestBranchIsIdempotent(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil { t.Fatal(err) }
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil { t.Fatal(err) }
	// Re-running the same branch must not error and must not duplicate the node.
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatalf("re-add must be idempotent, got %v", err)
	}
	s, _ := stack.LoadStack(root, "feat")
	count := 0
	for _, n := range s.Nodes { if n.Branch == "feat/b" { count++ } }
	if count != 1 { t.Errorf("feat/b appears %d times, want 1", count) }
}
```

- [ ] **Step 2: Run to verify it fails** — `go test ./cmd/ -run TestBranchIsIdempotent -count=1` → FAIL (errors "already exists").

- [ ] **Step 3: Implement.** Extend `NewBranchResult`:
```go
type NewBranchResult struct {
	Branch       string `json:"branch"`
	Stack        string `json:"stack"`
	Parent       string `json:"parent"`
	WorktreePath string `json:"worktree_path,omitempty"`
	Created      bool   `json:"created"`
	ErrorCode    string `json:"error_code,omitempty"`
}
```
In `runBranch`, replace `if s.FindNode(branchName) != nil { return fmt.Errorf("branch %q already exists ...") }` with an idempotent return: if the node exists, build `NewBranchResult{Created:false, Branch, Stack, Parent: s.ParentBranch(branchName), WorktreePath: node.WorktreePath}` and return it (JSON or human note), without recreating the branch/worktree. On the create path set `Created:true`.

- [ ] **Step 4: Run to verify it passes** — `go test ./cmd/ -run 'TestBranch' -count=1 && go build ./...` → PASS.
- [ ] **Step 5: Commit** — `feat(branch): idempotent re-add, created in JSON`.

---

## Task 5: `sdf pr` — `--draft`, fields, idempotent (J3)

**Files:** Modify `cmd/pr.go` (flag, `PRResult`, `runPR`). Test: `cmd/pr_test.go` (create if absent).

**Interfaces:** Consumes `ghpkg.PRCreate(...,draft)`, `ghpkg.PRView`. Produces `PRResult{Number, Pr, URL, Title, Draft, Created, ErrorCode}`.

- [ ] **Step 1: Write the failing test** (needs `ghpkg.Available()`; if the test env has no gh, guard with `t.Skip` when `!ghpkg.Available()` — mirror existing pr/gh tests. Assert the result struct shape + idempotency logic that does NOT require gh: the "already has PR" idempotent branch can be unit-tested by pre-seeding `node.PR`):
```go
func TestPRIdempotentWhenPRExists(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil { t.Fatal(err) }
	s, _ := stack.LoadStack(root, "feat")
	s.Nodes[0].PR = 7   // pretend a PR already exists
	stack.Save(root, s)
	// Re-running pr for a branch that already has a PR must NOT error.
	// (In worktree mode, run from the branch's worktree.)
	chdir(t, s.Nodes[0].WorktreePath)
	err := RunPR([]string{"--json"})
	if err != nil { t.Fatalf("pr must be idempotent when a PR exists, got %v", err) }
}
```
(If `RunPR` needs gh to fetch PR details for the idempotent return and gh is unavailable, the idempotent branch must degrade gracefully: return `{number:7, pr:7, created:false}` with whatever details are obtainable, never error solely because the PR already exists. Implement so the no-gh path still returns the known `node.PR`.)

- [ ] **Step 2: Run to verify it fails** — `go test ./cmd/ -run TestPRIdempotentWhenPRExists -count=1` → FAIL (errors "already has PR #7").

- [ ] **Step 3: Implement.** Add the flag in `pr.go` `init`: `prCmd.Flags().Bool("draft", false, "open the PR as a draft")`. Extend `PRResult`:
```go
type PRResult struct {
	Number    int    `json:"number"`
	Pr        int    `json:"pr"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	Draft     bool   `json:"draft"`
	Created   bool   `json:"created"`
	ErrorCode string `json:"error_code,omitempty"`
}
```
In `runPR`: read `draft, _ := cmd.Flags().GetBool("draft")`. Replace `if node.PR > 0 { return fmt.Errorf("branch %q already has PR #%d", ...) }` with an idempotent return — fetch current details via `ghpkg.PRView(branch)` when available (`url`, `IsDraft`), build `PRResult{Number: node.PR, Pr: node.PR, URL, Title, Draft, Created:false}` and return (JSON/human); if gh unavailable, return `{Number/Pr: node.PR, Created:false}`. On the create path: pass `draft` to `ghpkg.PRCreate(prTitle, body, base, branch, draft)`, and populate the result with `Pr: node.PR`, `Draft: draft`, `Created: true`.

- [ ] **Step 4: Run to verify it passes** — `go test ./cmd/ -run 'TestPR' -count=1 && go build ./...` → PASS.
- [ ] **Step 5: Commit** — `feat(pr): --draft, idempotent re-run, pr/draft/created JSON fields`.

---

## Task 6: `sdf pr --ready` (J4)

**Files:** Modify `cmd/pr.go` (flag, ready branch in `runPR`, `--branch` flag). Test: `cmd/pr_test.go`.

**Interfaces:** Consumes `ghpkg.PRReady`, `ghpkg.PRView`.

- [ ] **Step 1: Write the failing test** (logic-level; guard gh calls):
```go
func TestPRReadyRequiresExistingPR(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil { t.Fatal(err) }
	s, _ := stack.LoadStack(root, "feat")
	chdir(t, s.Nodes[0].WorktreePath)
	err := RunPR([]string{"--ready", "--json"})
	if err == nil { t.Fatalf("pr --ready with no PR must error clearly") }
	if !strings.Contains(err.Error(), "no PR") { t.Errorf("error should mention no PR: %v", err) }
}
```

- [ ] **Step 2: Run to verify it fails** — undefined `--ready` flag (cobra errors on unknown flag) or wrong behavior.

- [ ] **Step 3: Implement.** In `pr.go` `init`: `prCmd.Flags().Bool("ready", false, "mark the branch's draft PR as ready for review")` and `prCmd.Flags().String("branch", "", "target branch (default: current)")`. At the top of `runPR`, after resolving `branch` (honoring `--branch` when set) and `node`: if `ready` flag set, branch into a ready path:
```go
	if ready {
		if node.PR == 0 {
			return fmt.Errorf("no PR for branch %q — run `sdf pr` first", branch)
		}
		if ghpkg.Available() {
			if err := ghpkg.PRReady(node.PR); err != nil {
				return fmt.Errorf("cannot mark PR #%d ready: %w", node.PR, err)
			}
		}
		res := PRResult{Number: node.PR, Pr: node.PR, Draft: false, Created: false}
		if pv, e := ghpkg.PRView(branch); e == nil { res.URL = pv.URL }
		return emitPRResult(res, jsonFlag)   // helper that marshals (json) or prints (human)
	}
```
Extract the JSON-or-human emission into a small `emitPRResult(PRResult, json bool) error` helper reused by the create/idempotent/ready paths. `--ready` is idempotent (GitHub `pr ready` on an already-ready PR is a no-op).

- [ ] **Step 4: Run to verify it passes** — `go test ./cmd/ -run 'TestPR' -count=1 && go build ./...` → PASS.
- [ ] **Step 5: Commit** — `feat(pr): --ready to un-draft a PR`.

---

## Task 7: `status` — `conflicted` sync_state (J5)

**Files:** Modify `cmd/status.go` (sync_state computation). Test: `cmd/status_worktree_test.go`.

**Interfaces:** Consumes `gitpkg.IsRebaseInProgressAt`.

- [ ] **Step 1: Write the failing test**
```go
func TestStatusReportsConflictedSyncState(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil { t.Fatal(err) }
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil { t.Fatal(err) }
	s, _ := stack.LoadStack(root, "feat")
	wtA, wtB := s.FindNode("feat/a").WorktreePath, s.FindNode("feat/b").WorktreePath
	// Create a conflict and pause a rebase in feat/b's worktree.
	commitInWorktree(t, wtA, "shared.txt", "A\n", "a")
	commitInWorktree(t, wtB, "shared.txt", "B\n", "b")
	chdir(t, wtB)
	_ = RunSync(nil) // conflicts → paused rebase in wtB
	// status from main repo must report feat/b conflicted.
	chdir(t, root)
	resetStatusFlags()
	old := os.Stdout; r, w, _ := os.Pipe(); os.Stdout = w
	_ = RunStatus([]string{"--json"})
	w.Close(); os.Stdout = old
	out, _ := io.ReadAll(r)
	var sr StatusResult
	json.Unmarshal(out, &sr)
	var got string
	for _, n := range sr.Nodes { if n.Branch == "feat/b" { got = n.SyncState } }
	if got != "conflicted" { t.Errorf("feat/b sync_state = %q, want conflicted", got) }
}
```
(Add imports `io`, `os`, `strings`/`encoding/json` as needed; `resetStatusFlags` exists from prior work.)

- [ ] **Step 2: Run to verify it fails** — `go test ./cmd/ -run TestStatusReportsConflictedSyncState -count=1` → FAIL (status reports `needs_sync`, not `conflicted`).

- [ ] **Step 3: Implement.** In `status.go`, where `nr.SyncState` is computed (the ~line 208-224 block), add a conflicted check that takes precedence: for a worktree node, if `gitpkg.IsRebaseInProgressAt(node.WorktreePath)` is true, set `nr.SyncState = "conflicted"` and skip the in_sync/needs_sync computation for that node. Guard on `s.Worktree && node.WorktreePath != ""`.

- [ ] **Step 4: Run to verify it passes** — `go test ./cmd/ -run 'TestStatus' -count=1 && go build ./...` → PASS.
- [ ] **Step 5: Commit** — `feat(status): conflicted sync_state for paused worktree rebases`.

---

## Task 8: Worktree `sync`/`--continue` structured status (J6/J7)

**Files:** Modify `cmd/sync.go` (`BranchResult` fields), `cmd/sync_worktree.go` (`runWorktreeSyncStep`, `continueWorktreeSync` populate `result`). Test: `cmd/sync_worktree_test.go`.

**Interfaces:** Produces `BranchResult.Status` (`clean|noop|conflicted`), `BranchResult.Conflicts []string`. The worktree sync step writes exactly one `branches[]` entry and does NOT set top-level `error` on conflict.

This requires threading the `*SyncResult` into the worktree sync functions (today they take `bus` only and ignore `result`). `runSyncCmd` already builds `result` in `--json` mode and routes through `runSyncFull` → `runWorktreeSyncStep`. Pass `result` down.

- [ ] **Step 1: Write the failing tests**
```go
func TestWorktreeSyncJSONCleanAndNoop(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil { t.Fatal(err) }
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil { t.Fatal(err) }
	s, _ := stack.LoadStack(root, "feat")
	commitInWorktree(t, s.FindNode("feat/a").WorktreePath, "a.txt", "x\n", "a")
	chdir(t, s.FindNode("feat/b").WorktreePath)
	// First sync: clean rebase.
	resetSyncFlags()
	br := captureSyncJSON(t)
	if br.Status != "clean" || !br.Pushed { t.Errorf("want clean+pushed, got %+v", br) }
	// Second sync: noop.
	resetSyncFlags()
	br = captureSyncJSON(t)
	if br.Status != "noop" { t.Errorf("want noop, got %+v", br) }
}
func TestWorktreeSyncJSONConflictIsNotError(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil { t.Fatal(err) }
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil { t.Fatal(err) }
	s, _ := stack.LoadStack(root, "feat")
	commitInWorktree(t, s.FindNode("feat/a").WorktreePath, "shared.txt", "A\n", "a")
	commitInWorktree(t, s.FindNode("feat/b").WorktreePath, "shared.txt", "B\n", "b")
	chdir(t, s.FindNode("feat/b").WorktreePath)
	resetSyncFlags()
	old := os.Stdout; r, w, _ := os.Pipe(); os.Stdout = w
	err := RunSync([]string{"--json"})
	w.Close(); os.Stdout = old
	if err != nil { t.Fatalf("conflict must not be a process error: %v", err) }
	out, _ := io.ReadAll(r)
	var sr SyncResult
	json.Unmarshal(out, &sr)
	if sr.Error != "" { t.Errorf("top-level error must be empty on conflict, got %q", sr.Error) }
	if len(sr.Branches) != 1 || sr.Branches[0].Status != "conflicted" { t.Errorf("want branches[0].status=conflicted, got %+v", sr.Branches) }
	if len(sr.Branches[0].Conflicts) == 0 { t.Errorf("conflicts must list paths") }
}
// captureSyncJSON runs `sdf sync --json`, parses SyncResult, returns branches[0].
func captureSyncJSON(t *testing.T) BranchResult {
	t.Helper()
	old := os.Stdout; r, w, _ := os.Pipe(); os.Stdout = w
	_ = RunSync([]string{"--json"})
	w.Close(); os.Stdout = old
	out, _ := io.ReadAll(r)
	var sr SyncResult
	if err := json.Unmarshal(out, &sr); err != nil { t.Fatalf("bad json: %v\n%s", err, out) }
	if len(sr.Branches) == 0 { t.Fatalf("no branches in result: %s", out) }
	return sr.Branches[0]
}
```

- [ ] **Step 2: Run to verify it fails** — `go test ./cmd/ -run 'TestWorktreeSyncJSON' -count=1` → FAIL (no `Status`/`Conflicts`; conflict leaks into `error`).

- [ ] **Step 3: Implement.**
Add to `BranchResult` (cmd/sync.go):
```go
	Status    string   `json:"status,omitempty"`    // worktree sync: clean | noop | conflicted
	Conflicts []string `json:"conflicts,omitempty"` // worktree sync: conflicted paths
```
Thread `result *SyncResult` into `runWorktreeSyncStep(root, stackID, branch string, result *SyncResult, bus *render.Bus)` and `continueWorktreeSync(..., result *SyncResult, ...)` (update the routing calls in `runSyncFull`/`runSyncContinue`).
- No-op path (parentTip == BaseTip): append `BranchResult{Branch: branch, Status: "noop"}`.
- Success path (rebased + pushed): append `BranchResult{Branch: branch, Status: "clean", Pushed: true, PR: node.PR}`.
- Conflict path: gather `conflicts, _ := gitpkg.ConflictedFilesAt(wt)`; append `BranchResult{Branch: branch, Status: "conflicted", Conflicts: conflicts}`; record `WorktreeProgress` as today; **return nil** (no Go error) so `runSyncCmd` does not set `result.Error`. Keep the human-mode warning print for non-json.
Note: the dirty-worktree rejection and real failures (push error, etc.) still return errors → `result.Error` (and `error_code` from Task 9). Only the *rebase conflict* becomes `status:"conflicted"`.

- [ ] **Step 4: Run to verify it passes** — `go test ./cmd/ -run 'TestWorktreeSync' -count=1 && go build ./...` → PASS (incl. existing worktree sync tests; the older `TestWorktreeSyncRejectsDirtyWorktree` still expects an error — confirm the dirty path still errors).
- [ ] **Step 5: Commit** — `feat(sync): structured status/conflicts for worktree sync; conflict is not an error`.

---

## Task 9: Lock-timeout `error_code` + exit 75 (concurrency req §F)

**Files:** Modify result structs (`SyncResult`, `NewResult`, `NewBranchResult`, `PRResult` already have `ErrorCode` from Tasks 3-5/8 — add to `SyncResult`), and the command wrappers' error handling in `cmd/sync.go`/`cmd/new.go`/`cmd/branch.go`/`cmd/pr.go`/`main.go` (exit code). Create a small helper. Test: `cmd/lock_timeout_test.go`.

**Interfaces:** Consumes `stack.ErrLockTimeout`. Produces `error_code:"lock_timeout"` in JSON; exit 75 in non-json.

- [ ] **Step 1: Write the failing test** (drive a real lock contention; the cleanest is a unit test on the mapping helper):
```go
func TestLockTimeoutMapsToErrorCode(t *testing.T) {
	if code := errorCodeFor(fmt.Errorf("wrap: %w", stack.ErrLockTimeout)); code != "lock_timeout" {
		t.Errorf("got %q, want lock_timeout", code)
	}
	if code := errorCodeFor(fmt.Errorf("other")); code != "" {
		t.Errorf("non-lock error must have empty code, got %q", code)
	}
	if exitCodeFor(fmt.Errorf("x: %w", stack.ErrLockTimeout)) != 75 {
		t.Errorf("lock timeout must exit 75")
	}
}
```

- [ ] **Step 2: Run to verify it fails** — undefined `errorCodeFor`/`exitCodeFor`.

- [ ] **Step 3: Implement.** Add `SyncResult.ErrorCode string json:"error_code,omitempty"` (others added in their tasks). Add a shared helper file `cmd/errorcode.go`:
```go
package cmd
import ( "errors"; "github.com/pavelpascari/sdf/internal/stack" )
// errorCodeFor returns a stable machine code for an error, or "" for ordinary errors.
func errorCodeFor(err error) string {
	if errors.Is(err, stack.ErrLockTimeout) { return "lock_timeout" }
	return ""
}
// exitCodeFor maps an error to a process exit code: 75 (EX_TEMPFAIL) for a
// lock-timeout (retryable), 1 otherwise.
func exitCodeFor(err error) int {
	if errorCodeFor(err) == "lock_timeout" { return 75 }
	return 1
}
```
In each command's `--json` error path, set `result.ErrorCode = errorCodeFor(err)` alongside `result.Error = err.Error()`. In `main.go`'s top-level error handling (where non-json errors exit), use `os.Exit(exitCodeFor(err))` for the lock-timeout case (read `main.go` to find the exit path; map there).

- [ ] **Step 4: Run to verify it passes** — `go test ./cmd/ -run TestLockTimeoutMapsToErrorCode -count=1 && go build ./...` → PASS.
- [ ] **Step 5: Commit** — `feat(cmd): distinguishable lock_timeout error_code + exit 75`.

---

## Task 10: External-merge propagation test (J8)

**Files:** Test: `cmd/status_worktree_test.go` (or a new `cmd/external_merge_test.go`). Code only if the test exposes a gap.

- [ ] **Step 1: Write the test.** Build a 2-branch worktree stack with PRs recorded (`node.PR` set). Simulate a remote merge of the head PR by: marking the head node `merged` in a fresh clone's stack state is not how flow sees it — instead simulate what `status` observes: the head branch's PR shows merged. Since driving real `gh` is unavailable, assert the propagation logic directly: set the head node `Status="merged"` in the stack, then assert `status`/`ParentBranch` cause the child's `sync_state` to be `needs_sync` once the base advanced. Concretely: advance `main`, mark head merged, run `status --json` from main repo, assert child node `sync_state:"needs_sync"`.
```go
func TestExternalMergePropagatesNeedsSync(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil { t.Fatal(err) }
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil { t.Fatal(err) }
	s, _ := stack.LoadStack(root, "feat")
	// Simulate feat/a merged on the remote: mark merged + advance main past feat/a's tip.
	s.FindNode("feat/a").Status = "merged"
	stack.Save(root, s)
	// advance main (the merge commit) so feat/b's effective parent (main) moved
	commitOnBranchViaWorktree(t, root, "main", "merged.txt", "m\n", "merge of feat/a")
	chdir(t, root); resetStatusFlags()
	out := captureStatusJSON(t)
	var sr StatusResult; json.Unmarshal(out, &sr)
	for _, n := range sr.Nodes {
		if n.Branch == "feat/a" && n.Status != "merged" { t.Errorf("feat/a status=%q want merged", n.Status) }
		if n.Branch == "feat/b" && n.SyncState != "needs_sync" { t.Errorf("feat/b sync_state=%q want needs_sync", n.SyncState) }
	}
}
```
Provide `captureStatusJSON` and `commitOnBranchViaWorktree` helpers (the latter commits on a branch using a throwaway worktree or `git -C`). If `status` does not already surface `needs_sync` for the child here, fix the sync_state computation (it should via `ParentBranch` skipping the merged node + base drift).

- [ ] **Step 2: Run to verify** — `go test ./cmd/ -run TestExternalMergePropagatesNeedsSync -count=1`. If it passes immediately, J8 is confirmed (no code change). If it fails, fix `status` sync_state to account for merged-parent skip, then pass.
- [ ] **Step 3: Commit** — `test(status): confirm external-merge propagates needs_sync to child` (or `fix(status): ...` if code changed).

---

## Task 11: Docs + full verification

**Files:** Modify `README.md` (sync_state enum + flow JSON field contract; `pr --draft`/`--ready`). Optionally `www/src/data/cli-reference.json` (auto-regenerated by pre-commit hook).

- [ ] **Step 1: Full verification.**
  - `go build ./... && go vet ./... && golangci-lint run ./...` (0 issues).
  - `go test ./internal/... -count=1` (all pass).
  - The flow-touching cmd tests together: `go test ./cmd/ -run 'TestNew|TestBranch|TestPR|TestStatus|TestWorktreeSync|TestLockTimeout|TestExternalMerge' -count=1` (all pass). Confirm the remaining `cmd/` failures are exactly the known pre-existing `.sdf`-gitignore set.
- [ ] **Step 2: Document.** Add to `README.md`: the `sync_state` enum (`in_sync|needs_sync|conflicted`), `sdf pr --draft`/`--ready`, the `created`/`error_code`/`worktree_path` JSON fields, and a short "JSON output contract" note pointing integrators at the stable field names (mirror the spec's Field-name contract section). Match README's existing tone.
- [ ] **Step 3: Commit** — `docs: document flow JSON contract, sync_state enum, pr draft/ready`.

---

## Notes for the implementer
- **Idempotency is success, not a warning-as-error.** Re-runs return the current resource with `created:false` and exit 0. Never `return fmt.Errorf` on "already exists" for `new`/`branch`/`pr`.
- **Conflict is `status:"conflicted"`, never a process error or a top-level `error`.** Only genuine failures (dirty worktree, push rejection, lock timeout, IO) set `error`/`error_code`.
- **Additive JSON only.** Keep `number`/`title`/`action`; add `pr`/`draft`/`created`/`worktree_path`/`status`/`conflicts`/`error_code`.
- **All mutations stay under `stack.WithLock`** (idempotency existence-checks included) — do not reintroduce a load-outside-lock.
- The `cmd/` package has known pre-existing test failures (helpers `git add .sdf` vs gitignored `.sdf/`); verify your work with focused `-run` filters.
