# Worktree Mode — Review Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Eliminate the concurrency and correctness defects found in the PR #229 review — make every stack-JSON mutation lock-safe (load+mutate+save under one lock), fix worktree-mode gaps in sync/merge/move/restack/split, and remove the path-collision and stale-worktree footguns.

**Architecture:** Introduce one keystone abstraction — `stack.WithLock(root, stackID, fn)` that acquires the lock, loads the stack FRESH, runs `fn` to mutate, saves atomically, releases — and route every mutation site through it (no caller trusts a pre-lock snapshot). Make `Stack.Save` atomic (temp+rename) so lock-free readers never see a torn file. Make worktree commands operate in the branch's worktree dir via `-C`-scoped git ops instead of `checkout` in the process CWD.

**Tech Stack:** Go 1.24.7, cobra, shells to `git`/`gh`. Tests use real `git` in `t.TempDir()`.

## Global Constraints
- Module `github.com/pavelpascari/sdf`, Go 1.24.7.
- `.sdf/` is gitignored, one shared copy in the main repo. `local.json` is in that shared `.sdf/` too (so `SyncProgress` is shared across worktrees — that is the bug behind per-branch keying).
- **Locking invariant (the point of this phase):** every read-modify-write of a stack's JSON (and of that stack's progress in `local.json`) must occur while holding `stack.AcquireLock(root, stackID)` — the LOAD included. Use `stack.WithLock` everywhere.
- Non-worktree behavior (monolithic sync/merge/move/restack/split, single `LocalState.SyncProgress`) must stay byte-for-byte unchanged.
- Verify each task with focused tests. KNOWN pre-existing `cmd/` failures (TestStatus*/TestReconcile*/TestComputeSyncPlan*/TestProperty*/TestDetectBaseDrift — helpers `git add .sdf` vs gitignored `.sdf/`) are environmental and unrelated; do not try to fix them, and do not let them mask a NEW failure.
- Production git wrapper records via `recordRun` only; any new exec path calls it.
- Commit after each task, Conventional Commits, body ending:
  `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`

### Canonical signatures (consumed across tasks)
```go
// internal/stack/lock.go
const LockTimeout = 10 * time.Second
// internal/stack/withlock.go
func WithLock(root, stackID string, fn func(*Stack) error) error // acquire→load fresh→fn→Save (only if fn nil)→release

// internal/stack/stack.go — LocalState gains per-branch worktree progress:
type LocalState struct {
    SyncProgress     *SyncProgress              `json:"sync_progress,omitempty"`     // monolithic path, unchanged
    SplitSessions    map[string]string          `json:"split_sessions,omitempty"`
    RestackProgress  *RestackProgress           `json:"restack_progress,omitempty"`
    WorktreeProgress map[string]*SyncProgress   `json:"worktree_progress,omitempty"` // branch → progress (worktree mode)
}

// internal/git/worktree.go — new -C variants for move/restack/split:
func CherryPickAt(dir string, commits ...string) error
func CherryPickAbortAt(dir string) error
func ResetHardAt(dir, ref string) error
func AddAt(dir string, paths ...string) error
func CheckoutAt(dir, branch string) error   // used only where a worktree must change refs; normally unused in WT mode

// internal/config/worktree.go — nested paths (collision-free):
func SanitizeBranchForPath(branch string) string  // returns filepath.FromSlash-safe nested form (does NOT flatten '/')

// cmd/worktree_helpers.go
func branchWorktreeDir(s *stack.Stack, branch string) string // node.WorktreePath or "" (non-worktree)
```

---

## Task 1: Keystone — `stack.WithLock`, `LockTimeout`, atomic `Save`, concurrency regression test

**Files:**
- Create: `internal/stack/withlock.go`, `internal/stack/withlock_test.go`
- Modify: `internal/stack/lock.go` (export `LockTimeout`), `internal/stack/stack.go` (`Save` → atomic temp+rename)

**Interfaces:**
- Produces: `stack.WithLock`, `stack.LockTimeout`; atomic `Save`.

- [ ] **Step 1: Write the failing test (concurrency + helper)**

```go
// internal/stack/withlock_test.go
package stack

import (
	"sync"
	"testing"
)

func TestWithLockSerializesConcurrentAppends(t *testing.T) {
	root := sdfRepo(t) // helper from lock_test.go: makes .sdf/stacks/
	if err := Save(root, &Stack{StackID: "feat", Base: "main", Nodes: nil}); err != nil {
		t.Fatal(err)
	}
	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = WithLock(root, "feat", func(s *Stack) error {
				s.Nodes = append(s.Nodes, Node{Branch: branchName(i), Status: "open"})
				return nil
			})
		}(i)
	}
	wg.Wait()
	s, err := LoadStack(root, "feat")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Nodes) != n {
		t.Fatalf("lost updates: got %d nodes, want %d", len(s.Nodes), n)
	}
}

func TestWithLockSkipsSaveOnError(t *testing.T) {
	root := sdfRepo(t)
	_ = Save(root, &Stack{StackID: "feat", Base: "main", Nodes: []Node{{Branch: "a", Status: "open"}}})
	want := errString("boom")
	err := WithLock(root, "feat", func(s *Stack) error {
		s.Nodes = append(s.Nodes, Node{Branch: "b"})
		return want
	})
	if err == nil {
		t.Fatal("expected error from fn")
	}
	s, _ := LoadStack(root, "feat")
	if len(s.Nodes) != 1 {
		t.Errorf("save must be skipped when fn errors; got %d nodes", len(s.Nodes))
	}
}

func branchName(i int) string { return "b" + string(rune('a'+i%26)) + string(rune('a'+i/26)) }

type errString string
func (e errString) Error() string { return string(e) }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/stack/ -run TestWithLock -count=1`
Expected: FAIL — undefined `WithLock`.

- [ ] **Step 3: Implement**

In `internal/stack/lock.go`, replace the unexported staleness/timeout literal with an exported constant and use it. Add near the top:
```go
// LockTimeout bounds how long an sdf process waits to acquire a stack lock.
const LockTimeout = 10 * time.Second
```
(Leave `AcquireLock`'s signature as-is; callers may pass `LockTimeout`.)

Create `internal/stack/withlock.go`:
```go
package stack

// WithLock runs fn against a freshly-loaded copy of the named stack while
// holding the stack's advisory lock, then saves the stack. The lock is held
// across load+mutate+save so concurrent sdf processes cannot lose updates.
// The stack is saved only when fn returns nil.
func WithLock(root, stackID string, fn func(*Stack) error) error {
	lock, err := AcquireLock(root, stackID, LockTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	s, err := LoadStack(root, stackID)
	if err != nil {
		return err
	}
	if err := fn(s); err != nil {
		return err
	}
	return Save(root, s)
}
```

Make `Save` atomic in `internal/stack/stack.go` — write to a temp file in the same dir, then `os.Rename`:
```go
func Save(root string, s *Stack) error {
	stacksDir := filepath.Join(root, SDFDir, StacksDir)
	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		return fmt.Errorf("cannot create %s: %w", stacksDir, err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal stack: %w", err)
	}
	data = append(data, '\n')
	path := StackPath(root, s.StackID)
	tmp, err := os.CreateTemp(stacksDir, "."+s.StackID+".*.tmp")
	if err != nil {
		return fmt.Errorf("cannot create temp stack file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("cannot write temp stack file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("cannot rename temp stack file: %w", err)
	}
	return nil
}
```
Apply the same temp+rename treatment to `SaveLocal`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/stack/ -count=1`
Expected: PASS (new concurrency test + all existing stack tests). Run with `-race`: `go test ./internal/stack/ -run TestWithLock -race -count=1` — PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/stack/withlock.go internal/stack/withlock_test.go internal/stack/lock.go internal/stack/stack.go
git commit -m "feat(stack): add WithLock and atomic Save for safe concurrent mutation

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Route `branch` and `new` through `WithLock`

**Files:** Modify `cmd/branch.go`, `cmd/new.go`. Test: `cmd/branch_worktree_test.go` (add a concurrency test).

**Interfaces:** Consumes `stack.WithLock`.

Both commands currently load (`resolveStack`/`LoadStack`) then `stack.Save` with no lock — concurrent `sdf branch` calls drop nodes (#1).

- [ ] **Step 1: Write the failing test**

```go
// add to cmd/branch_worktree_test.go
func TestConcurrentBranchKeepsAllNodes(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatal(err)
	}
	// Two concurrent appends to the same stack must both survive.
	var wg sync.WaitGroup
	for _, name := range []string{"feat/b", "feat/c"} {
		wg.Add(1)
		go func(n string) { defer wg.Done(); _ = RunBranch([]string{n, "--no-prefix"}) }(name)
	}
	wg.Wait()
	s, _ := stack.LoadStack(root, "feat")
	for _, b := range []string{"feat/a", "feat/b", "feat/c"} {
		if s.FindNode(b) == nil {
			t.Errorf("node %s lost to a concurrent branch race", b)
		}
	}
}
```
(Add `"sync"` import.) NOTE: `RunBranch` drives the shared `rootCmd`; this test is inherently about the JSON race, not cobra flags — both goroutines pass the same args, so flag state is identical. If flaky on cobra state, serialize the `rootCmd.Execute()` behind a mutex inside the test while still exercising `WithLock` (the race under test is the stack-file write, which `WithLock` serializes regardless).

- [ ] **Step 2: Run to verify it fails** — `go test ./cmd/ -run TestConcurrentBranchKeepsAllNodes -count=1` → FAIL (a node is lost).

- [ ] **Step 3: Implement.** In `runBranch` (cmd/branch.go), after resolving `cfg`, `branchName`, `parent`, `insertAfterIdx`, and creating the worktree/branch, move the node-insertion + downstream-PR-base bookkeeping into `WithLock`. Replace the `s.Nodes = slices.Insert(...)` + `stack.Save(root, s)` region with:
```go
	var newNode stack.Node // built before/inside as today
	err = stack.WithLock(root, s.StackID, func(ls *stack.Stack) error {
		// Re-find insertion point in the FRESH stack (positions may have shifted).
		insertAt := ls.NodeIndex(parent) + 1
		if ls.NodeIndex(parent) < 0 {
			insertAt = len(ls.Nodes)
		}
		ls.Nodes = slices.Insert(ls.Nodes, insertAt, newNode)
		return nil
	})
	if err != nil {
		return err
	}
```
Keep the worktree/branch creation (side effects on git) BEFORE the lock as today, but compute `newNode` (incl. WorktreePath) before the lock. Reload `s` after WithLock if later code needs the updated node list (re-`LoadStack` for the downstream-PR-base update, or do that update inside the `fn`). Keep `--json`/human output using `newNode`.
In `cmd/new.go` `runNewCore`, wrap the two mutation points (set `Worktree=true`; append the first node) so each is a `WithLock(root, stackName, fn)` (load fresh, mutate, save) instead of bare `LoadStack`+mutate+`Save`. The branch/worktree git creation stays outside the lock; the node append (with BaseTip + WorktreePath) goes inside.

- [ ] **Step 4: Run to verify it passes** — `go test ./cmd/ -run 'TestConcurrentBranch|TestBranch|TestNew' -count=1` → PASS.

- [ ] **Step 5: Commit** — `fix(branch,new): mutate stack JSON under WithLock`.

---

## Task 3: Route `runWorktreeSyncStep` through `WithLock`; add base fetch/fast-forward and nav refresh

**Files:** Modify `cmd/sync_worktree.go`, `cmd/sync.go` (routing). Test: `cmd/sync_worktree_test.go`.

Fixes #3 (no fetch/ff before parent-tip read), #5 (load-before-lock), #9 (no nav refresh).

- [ ] **Step 1: Write the failing test** — an external merge advances `origin/main`; running `sdf sync` in a downstream worktree must integrate it.
```go
func TestWorktreeSyncFetchesMovedBaseFromOrigin(t *testing.T) {
	root := bareRepoWithClone(t)
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil { t.Fatal(err) }
	s, _ := stack.LoadStack(root, "feat")
	wtA := s.FindNode("feat/a").WorktreePath
	// Advance origin/main from a separate clone (simulates a sibling PR merge).
	other := t.TempDir()
	mustRun(t, "", "git", "clone", originOf(t, root), other)        // helpers below
	commitAndPush(t, other, "main", "ext.txt", "ext\n", "external")
	// feat/a's BaseTip still equals the OLD local main; running sync in wtA must
	// fetch, fast-forward main, and rebase feat/a onto the new tip.
	chdir(t, wtA)
	if err := RunSync(nil); err != nil { t.Fatalf("sync: %v", err) }
	localMain, _ := gitpkg.RevParse("main")
	if !gitpkg.IsAncestor(localMain, "feat/a") {
		t.Errorf("feat/a did not integrate the moved base")
	}
}
```
Provide small helpers `originOf`, `commitAndPush`, `mustRun` in the test file (push to origin via a second clone). Reset sync flags at the top.

- [ ] **Step 2: Run to verify it fails** — sync reads the stale local `main`, prints "up to date", rebases nothing → assertion fails.

- [ ] **Step 3: Implement.** Change routing in `cmd/sync.go` `runSyncFull` to pass identifiers, not the pre-loaded `s`:
```go
	if s.Worktree {
		cur, _ := gitpkg.CurrentBranch()
		if currentWorktreeNode(s, cur) != nil {
			return runWorktreeSyncStep(root, s.StackID, cur, bus)
		}
		return runWorktreeDashboard(root, s, bus)
	}
```
Rewrite `runWorktreeSyncStep(root, stackID, branch string, bus *render.Bus) error`:
1. `gitpkg.FetchAll()` then `gitpkg.FastForward(<base>)` — get the base from a quick `LoadStack` (read-only) or inside the lock before resolving parent. (Fetch/ff are network/local-ref ops; do them BEFORE taking the lock to avoid holding it across the network fetch. Re-resolve parent tip INSIDE the lock after ff.)
2. `stack.WithLock(root, stackID, func(s){ ... })`: find node by branch; resolve `parent := s.ParentBranch(branch)`; `parentTip := RevParse(parent)`; if `parentTip == node.BaseTip` → set a flag "noop" and return nil; if worktree dirty (`IsCleanAt(node.WorktreePath)`) → return a dirty error; else `RebaseOntoAt(wt, parent, oldBase, branch)` (on conflict: write `local.WorktreeProgress[branch]` via a locked-local helper and return a conflict error); `PushAt(wt, branch)`; set `node.BaseTip = parentTip`; PR-base update rule unchanged. }
3. After WithLock returns nil and work happened: `updateStackNavForAllPRs(root, freshStack, nil, bus)` (reload the stack read-only for nav), and print the downstream-needs-sync message.
The conflict-progress write touches `local.json`; do it while still holding the stack lock (inside `fn`) so the `WorktreeProgress` map isn't clobbered — read-modify-write `local.json` inside `fn`.

- [ ] **Step 4: Run to verify it passes** — `go test ./cmd/ -run 'TestWorktreeSync|TestDashboard' -count=1` → PASS (incl. existing rebase/dirty/continue tests).

- [ ] **Step 5: Commit** — `fix(sync): worktree sync fetches base, locks load+mutate, refreshes nav`.

---

## Task 4: Per-branch worktree `SyncProgress`; route `continueWorktreeSync` through `WithLock`

**Files:** Modify `internal/stack/stack.go` (`LocalState.WorktreeProgress`), `cmd/sync_worktree.go`, `cmd/sync.go` (`runSyncContinue` routing), `cmd/prune.go` (clean stale entries). Test: `cmd/sync_continue_worktree_test.go`.

Fixes #6 (single global slot → wrong-branch `--continue`/push) and the continue load-before-lock half of #5.

- [ ] **Step 1: Write the failing test** — two worktrees conflict; `--continue` in worktree A must resume A, not B.
```go
func TestWorktreeContinueResumesOwnBranch(t *testing.T) {
	// Build feat/a, feat/b, feat/c worktrees. Cause a conflict on BOTH feat/b and feat/c
	// (advance feat/a, then make conflicting edits on b and c vs a's change).
	// Run sync in wtB (conflict → progress[feat/b]); run sync in wtC (conflict → progress[feat/c]).
	// Resolve + `sdf sync --continue` in wtB; assert feat/b advanced and feat/c's progress is untouched.
	// ... (full setup mirrors TestWorktreeSyncContinueAfterManualResolve, doubled) ...
}
```
Write it concretely against `bareRepoWithClone` + `commitInWorktree`; assert `WorktreeProgress["feat/c"]` still present after continuing `feat/b`, and `feat/b`'s tip (not `feat/c`'s) moved.

- [ ] **Step 2: Run to verify it fails** — with the single slot, B's progress is overwritten by C; continuing in B resumes C.

- [ ] **Step 3: Implement.** Add `WorktreeProgress map[string]*SyncProgress json:"worktree_progress,omitempty"` to `LocalState`. In `runWorktreeSyncStep`'s conflict path, write `local.WorktreeProgress[branch] = &SyncProgress{PausedAt: branch, ParentTip: parentTip, WorktreePath: wt}` (allocate the map if nil). In `runSyncContinue` (cmd/sync.go), BEFORE the monolithic logic, detect worktree context:
```go
	if cur, _ := gitpkg.CurrentBranch(); true {
		if s, e := stack.LoadByBranch(root, cur); e == nil && s.Worktree {
			local, _ := stack.LoadLocal(root)
			if p := local.WorktreeProgress[cur]; p != nil {
				return continueWorktreeSync(root, s.StackID, cur, bus)
			}
			return fmt.Errorf("no paused worktree sync for %s", cur)
		}
	}
	// ... existing monolithic continue unchanged ...
```
Rewrite `continueWorktreeSync(root, stackID, branch string, bus)` to do the rebase-state detection in the worktree, then `stack.WithLock(root, stackID, fn)` for the BaseTip update, and clear `local.WorktreeProgress[branch]` (locked-local read-modify-write inside `fn` or right after under the same lock). Push via `PushAt`. In `cmd/prune.go` `pruneLocalState`, also delete `WorktreeProgress[b]` entries whose branch no longer exists.
Keep `LocalState.SyncProgress` and the monolithic continue path UNTOUCHED.

- [ ] **Step 4: Run to verify** — `go test ./cmd/ -run 'TestWorktreeContinue|TestWorktreeSyncContinue|TestWorktreeSync' -count=1` and `go test ./internal/stack/ -count=1` → PASS.

- [ ] **Step 5: Commit** — `fix(sync): per-branch worktree resume state, locked continue`.

---

## Task 5: Route `merge` worktree mutations through `WithLock` (incl. the early reconcile-save)

**Files:** Modify `cmd/merge.go`. Test: `cmd/merge_worktree_test.go`.

Fixes #2 — `reconcileSyncPRStates` + `stack.Save` (lines 122-123) run unlocked before the worktree lock; and the manual lock added in `a044f06` should become `WithLock` reloading fresh.

- [ ] **Step 1: Write the failing test** — for a worktree stack, the early reconcile-save must not clobber a concurrent downstream BaseTip update. (Unit-test the locked reconcile path: simulate a concurrent `WithLock` BaseTip bump landing between merge's load and its reconcile-save; assert the bump survives.) Concretely: build a worktree stack; start merge's reconcile under the new locked path; from another goroutine `stack.WithLock(... set downstream BaseTip ...)`; assert the final JSON has BOTH the merge's status changes and the downstream BaseTip.

- [ ] **Step 2: Run to verify it fails** (against the current unlocked reconcile-save).

- [ ] **Step 3: Implement.** For worktree stacks only, wrap the reconcile mutation: replace `reconcileSyncPRStates(s, bus); stack.Save(root, s)` with — when `s.Worktree` — `stack.WithLock(root, s.StackID, func(ls){ reconcileSyncPRStates(ls, bus); return nil })` then reload `s` for the read-only planning (findHeadPR/confirm) from the locked result. Keep the non-worktree path as the original `reconcile + Save` (no lock). Replace the post-merge `if s.Worktree { lock... node.Status=merged; Save; cleanup; Save }` block (from a044f06) with a single `stack.WithLock(root, s.StackID, func(ls){ n := ls.FindNode(node.Branch); n.Status="merged"; cleanupMergedWorktree(root, ls, n, false, bus); return nil })` — reloading fresh inside the lock so it can't clobber a concurrent sync. Do NOT hold the lock across `ui.Confirm` or `ghpkg.PRMergeWithOptions` (network/human) — each `WithLock` section is short. Non-worktree merge flow (incl. `runSyncFull` cascade) unchanged.

- [ ] **Step 4: Run to verify** — `go test ./cmd/ -run 'TestPostMergeWorktreeCleanup|TestMerge' -count=1` → PASS.

- [ ] **Step 5: Commit** — `fix(merge): all worktree-stack saves under WithLock`.

---

## Task 6: Untracked-aware cleanliness for worktree removal

**Files:** Modify `internal/git/worktree.go` (add untracked-aware check), `cmd/sync_worktree.go` (`cleanupMergedWorktree`), `cmd/prune.go` if it relies on the same check. Test: `internal/git/worktree_test.go` + `cmd/merge_worktree_test.go`.

Fixes #8 — `IsCleanAt` passes `--untracked-files=no`, so a worktree with untracked files reports clean, then `git worktree remove` (no `--force`) fails exit 128 and the WorktreePath is left stale.

- [ ] **Step 1: Write the failing test**
```go
// internal/git/worktree_test.go
func TestIsCleanAtCountsUntracked(t *testing.T) {
	repo := initTestRepo(t); chdir(t, repo)
	wt := filepath.Join(t.TempDir(), "w")
	if err := WorktreeAdd(wt, "w", "main"); err != nil { t.Fatal(err) }
	// untracked file present
	os.WriteFile(filepath.Join(wt, "scratch.txt"), []byte("x"), 0644)
	clean, _ := IsCleanAt(wt)
	if clean { t.Errorf("IsCleanAt should ignore untracked (documented), got clean") }
	full, _ := IsWorktreeRemovable(wt)
	if full { t.Errorf("IsWorktreeRemovable must count untracked files") }
}
```

- [ ] **Step 2: Run to verify it fails** — undefined `IsWorktreeRemovable`.

- [ ] **Step 3: Implement.** Add to `internal/git/worktree.go`:
```go
// IsWorktreeRemovable reports whether the worktree at dir has no uncommitted
// changes AND no untracked files — i.e. `git worktree remove` will succeed
// without --force. Unlike IsCleanAt, this counts untracked files.
func IsWorktreeRemovable(dir string) (bool, error) {
	out, err := runAt(dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out == "", nil
}
```
In `cleanupMergedWorktree` (cmd/sync_worktree.go), replace the `IsCleanAt` guard with `IsWorktreeRemovable`. When not removable and `!force`: warn AND **retain** the WorktreePath intentionally (it already does) — but now the check matches what `git worktree remove` will actually accept, so the success path never hits exit 128. When removable (or force), `removeWorktreeForNode` succeeds and clears the path. Use `IsWorktreeRemovable` anywhere prune decides to auto-remove too.

- [ ] **Step 4: Run to verify** — `go test ./internal/git/ -run TestIsCleanAtCountsUntracked -count=1` and `go test ./cmd/ -run TestPostMergeWorktreeCleanup -count=1` → PASS.

- [ ] **Step 5: Commit** — `fix(worktree): count untracked files before auto-removing a worktree`.

---

## Task 7: Nested (collision-free) worktree paths

**Files:** Modify `internal/config/worktree.go`, `internal/config/worktree_test.go`, `cmd/worktree_helpers.go` (ensure `MkdirAll` of nested parent — already present).

Fixes #10 — flattening `/`→`-` collides (`feat/login` vs `feat-login`).

- [ ] **Step 1: Update the failing test**
```go
func TestWorktreePathForNested(t *testing.T) {
	cfg := Defaults()
	root := "/home/u/proj/myrepo"
	if got := cfg.WorktreePathFor(root, "feat/login"); got != filepath.Clean("/home/u/proj/myrepo.worktrees/feat/login") {
		t.Errorf("nested path = %q", got)
	}
	// distinct branches must map to distinct dirs
	a := cfg.WorktreePathFor(root, "feat/login")
	b := cfg.WorktreePathFor(root, "feat-login")
	if a == b { t.Errorf("feat/login and feat-login collided: %q", a) }
}
```
Update `TestSanitizeBranchForPath`/`TestWorktreePathForDefault` to expect nested output (`feat/a` → `<base>/feat/a`).

- [ ] **Step 2: Run to verify it fails.**

- [ ] **Step 3: Implement.** Change `SanitizeBranchForPath` to NOT flatten — return the branch as a slash-path safe for the OS:
```go
// SanitizeBranchForPath maps a branch name to a relative worktree path. It keeps
// '/' as a directory separator (nested layout), which is collision-free because
// git's ref D/F rule forbids a branch and a sub-path of it coexisting
// (e.g. you cannot have both `feat` and `feat/login`).
func SanitizeBranchForPath(branch string) string {
	return filepath.FromSlash(branch)
}
```
`WorktreePathFor` already does `filepath.Join(base, SanitizeBranchForPath(branch))` → nested. `addWorktreeForNode` already `os.MkdirAll(filepath.Dir(path), …)` so nested parents are created. Confirm `git worktree add` handles nested target dirs (it does).

- [ ] **Step 4: Run to verify** — `go test ./internal/config/ -count=1` and `go test ./cmd/ -run 'TestNewWorktree|TestBranchWorktree|TestWorktreeEnable' -count=1` → PASS (worktrees now created at nested paths; existing assertions that compute `WorktreePathFor` stay valid).

- [ ] **Step 5: Commit** — `fix(config): nested collision-free worktree paths`.

---

## Task 8: `worktree enable` — persist as you go (no orphans on mid-loop failure)

**Files:** Modify `cmd/worktree.go`. Test: `cmd/worktree_enable_test.go`.

Fixes #7 — if a stack branch is checked out in some other worktree, `WorktreeAdd` fails mid-loop and the partial state (worktrees created earlier, `Worktree=true`) is never saved.

- [ ] **Step 1: Write the failing test** — pre-create a worktree for one stack branch elsewhere, then `worktree enable`; assert that (a) it returns an informative error, AND (b) the stack JSON records `Worktree=true` and the WorktreePaths for the branches that DID materialize (no orphans), so a re-run is idempotent and `doctor` sees a consistent state.

- [ ] **Step 2: Run to verify it fails** (current code returns before any save).

- [ ] **Step 3: Implement.** Restructure `runWorktreeEnable` to mutate+save under `stack.WithLock`, persisting after each successful `addWorktreeForNode`: inside the lock, set `s.Worktree = true` and save once up front (so the flag survives), then loop nodes; on each successful add, update the node's WorktreePath and `Save` (or accumulate and save in a `defer`/on-error path). On a failing add, save what succeeded and return a wrapped error naming the conflicting branch and suggesting `sdf doctor`. Net: every created worktree has a recorded WorktreePath even on partial failure.

- [ ] **Step 4: Run to verify** — `go test ./cmd/ -run TestWorktreeEnable -count=1` → PASS.

- [ ] **Step 5: Commit** — `fix(worktree): persist enable progress to avoid orphaned worktrees`.

---

## Task 9: git `-C` variants for move/restack/split + `branchWorktreeDir` helper

**Files:** Modify `internal/git/worktree.go`, `internal/git/worktree_test.go`, `cmd/worktree_helpers.go`.

Prereq for Tasks 10-12. Adds the worktree-scoped git ops those commands need and a CWD-resolver helper.

- [ ] **Step 1: Write the failing test** for the new git ops:
```go
func TestCherryPickAtAndResetHardAt(t *testing.T) {
	repo := initTestRepo(t); chdir(t, repo)
	// make a commit on a side branch, cherry-pick it into a worktree, reset it out
	// ... assert CherryPickAt applies in the worktree dir and ResetHardAt moves its HEAD ...
}
```

- [ ] **Step 2: Run to verify it fails.**

- [ ] **Step 3: Implement** in `internal/git/worktree.go` (all via `runAt`, which records):
```go
func CherryPickAt(dir string, commits ...string) error { _, err := runAt(dir, append([]string{"cherry-pick"}, commits...)...); return err }
func CherryPickAbortAt(dir string) error { _, err := runAt(dir, "cherry-pick", "--abort"); return err }
func ResetHardAt(dir, ref string) error { _, err := runAt(dir, "reset", "--hard", ref); return err }
func AddAt(dir string, paths ...string) error { _, err := runAt(dir, append([]string{"add"}, paths...)...); return err }
func CheckoutAt(dir, branch string) error { _, err := runAt(dir, "checkout", branch); return err }
```
Add `cmd/worktree_helpers.go`:
```go
// branchWorktreeDir returns the directory git ops for a branch must run in:
// the branch's worktree in worktree mode, or "" (the process CWD) otherwise.
func branchWorktreeDir(s *stack.Stack, branch string) string {
	if n := s.FindNode(branch); n != nil && n.WorktreePath != "" {
		return n.WorktreePath
	}
	return ""
}
```

- [ ] **Step 4: Run to verify** — `go test ./internal/git/ -count=1` → PASS.
- [ ] **Step 5: Commit** — `feat(git): -C variants for cherry-pick/reset/add/checkout`.

---

## Task 10: `sdf move` worktree support

**Files:** Modify `cmd/move.go`. Test: `cmd/move_worktree_test.go`.

Fixes #4 for move — it cherry-picks onto `parent` and rebases `branch`/downstream via `Checkout` in CWD (move.go:170,174,177,189,212,216,226). In worktree mode each of those branches lives in its own worktree.

- [ ] **Step 1: Write the failing test** — build a 3-branch worktree stack; `cd` into branchB's worktree; `RunMove` a commit from B to its parent A; assert the commit now lives on A (in wtA), B no longer has it, both worktrees are intact and on their own branches, downstream rebased. Reset move flags for isolation.

- [ ] **Step 2: Run to verify it fails** (Checkout in CWD errors / corrupts).

- [ ] **Step 3: Implement.** In `runMoveLogic`, after `resolveStack`, compute per-branch dirs via `branchWorktreeDir(s, <branch>)`. When `s.Worktree`: operate with the `At` variants in each branch's worktree and DO NOT `Checkout` (the branch is already checked out in its worktree) — cherry-pick onto parent runs in the PARENT's worktree (`CherryPickAt(parentDir, …)`); the `RebaseOnto`/downstream rebases run in each target branch's worktree (`RebaseOntoAt(branchDir, …)`); replace the cleanup `Checkout(branch)` calls (no-ops in WT mode); the conflict handler uses `…At(dir)` variants. Mutate the stack via `WithLock`. Non-worktree path unchanged (guard with `if s.Worktree`). Cherry-pick the parent in `parentDir`, then move B's branch ref forward by rebasing B in `branchDir` dropping the moved commits (mirror existing logic, dir-scoped). Validate the cross-worktree result with the test.

- [ ] **Step 4: Run to verify** — `go test ./cmd/ -run 'TestMove' -count=1` (new + existing move tests) → PASS.
- [ ] **Step 5: Commit** — `fix(move): operate in branch worktrees in worktree mode`.

---

## Task 11: `sdf restack` worktree support

**Files:** Modify `cmd/restack.go`. Test: `cmd/restack_worktree_test.go`.

Fixes #4 for restack — `Checkout(originalBranch)` and per-branch rebases (restack.go:240,249,293,335,416,421,468) in CWD.

- [ ] **Step 1: Write the failing test** — worktree stack with 3 branches; reorder via `RunRestack`; assert branches rebased onto new parents in their own worktrees, all worktrees intact, BaseTips updated. Cover `--continue`/`--abort` restore paths for worktree mode.

- [ ] **Step 2: Run to verify it fails.**

- [ ] **Step 3: Implement.** In `runRestackLogic`/`runRestackContinue`/`runRestackAbort`: when `s.Worktree`, run each `RebaseOnto`→`RebaseOntoAt(branchWorktreeDir(s,a.Branch), …)`, `ResetHard`→`ResetHardAt(dir,…)`, and drop `Checkout(originalBranch)` (no-op in WT mode). Mutate stack/local under `WithLock`. Push via `PushAt(dir, branch)`. Non-worktree path unchanged.

- [ ] **Step 4: Run to verify** — `go test ./cmd/ -run TestRestack -count=1` → PASS.
- [ ] **Step 5: Commit** — `fix(restack): worktree-aware rebases and restore`.

---

## Task 12: `sdf split` worktree support

**Files:** Modify `cmd/split.go`. Test: `cmd/split_worktree_test.go`.

Fixes #4 for split — `Checkout` at split.go:303,348 in CWD.

- [ ] **Step 1: Write the failing test** — worktree stack; split a branch; assert the resulting branches each get a worktree (via `addWorktreeForNode`) and the operation runs in the right dirs.
- [ ] **Step 2: Run to verify it fails.**
- [ ] **Step 3: Implement.** Read `cmd/split.go`; where it checks out and rebases, use `…At(dir)` for existing-branch worktrees and `addWorktreeForNode` for any newly-created branch; mutate stack under `WithLock`; guard with `if s.Worktree`; non-worktree path unchanged.
- [ ] **Step 4: Run to verify** — `go test ./cmd/ -run TestSplit -count=1` → PASS.
- [ ] **Step 5: Commit** — `fix(split): worktree-aware split`.

---

## Task 13: register flag preservation, doctor symlink false-positive, lock-test fidelity, dedups

**Files:** Modify `cmd/register.go` (or `internal/stack/reconcile.go`), `cmd/doctor.go`, `internal/stack/lock.go`, `internal/git/worktree.go` (dedup). Tests as noted.

- [ ] **Step 1: Write/adjust failing tests:**
  - register: a reconcile of a `Worktree:true` stack must keep `Worktree` true and node WorktreePaths (add `TestReconcilePreservesWorktreeMode`).
  - doctor: `checkWorktrees` must not false-positive when `WorktreeList` returns a symlink-resolved path while `WorktreePathFor` does not — compare with `filepath.EvalSymlinks` on both sides (add `TestCheckWorktreesSymlinkPath`).
  - lock: make `AcquireLock` call `writeLockFile` (so the test exercises the production write path); assert the lock file content matches `writeLockFile`'s format.

- [ ] **Step 2: Run to verify they fail.**

- [ ] **Step 3: Implement.**
  - In the reconcile/register path, carry `Worktree` and each node's `WorktreePath` from the existing stack onto the reconciled result (don't drop them).
  - In `checkWorktrees`, normalize both the recorded path and git's listed paths via `filepath.EvalSymlinks` (fall back to the raw path on error) before the membership/stat checks.
  - In `AcquireLock`, replace the inline marshal+write with `writeLockFile(path, lockData{PID: os.Getpid(), Stamp: time.Now().Unix()})` after the O_EXCL create (write via the same fd path or write the file then it already exists — simplest: keep O_EXCL create to claim atomically, then `writeLockFile` to fill content). Ensure no TOCTOU regression.
  - Dedup: have `run` and `runAt` share one implementation (e.g. `run(args...)` = `runIn("", args...)`; `runAt(dir,…)` = `runIn(dir,…)`), and make `RebaseContinue`/`RebaseContinueAt` share one helper with an optional dir + the `GIT_EDITOR=true` env (and record the same args incl. a marker so spy fidelity is consistent). Replace `doctor.go`'s inline worktree-path set with the shared `existingWorktreePaths` helper.

- [ ] **Step 4: Run to verify** — `go test ./internal/... -count=1` and `go test ./cmd/ -run 'TestReconcile|TestCheckWorktrees|TestRegister' -count=1` → PASS (note: some TestReconcile* are pre-existing-failing for the `.sdf` reason; run the NEW test by exact name to confirm).
- [ ] **Step 5: Commit** — `refactor(worktree): register flag preservation, doctor symlink fix, lock/test fidelity, dedups`.

---

## Task 14: Full verification + PR update

- [ ] **Step 1:** `go build ./... && go vet ./cmd/ && golangci-lint run` (0 issues).
- [ ] **Step 2:** `go test ./internal/... -count=1 -race` → all PASS (race-clean, incl. the WithLock concurrency test).
- [ ] **Step 3:** Worktree cmd tests together:
  `go test ./cmd/ -run 'TestWorktree|TestDashboard|TestBranch|TestNew|TestSwitch|TestLS|TestCheckWorktrees|TestPostMergeWorktreeCleanup|TestPrune|TestMove|TestRestack|TestSplit|TestConcurrentBranch|TestRegister' -count=1` → all PASS. Confirm the remaining `cmd/` failures are exactly the known pre-existing set.
- [ ] **Step 4:** Update README if any documented behavior changed (nested paths; move/restack/split now work in worktree mode). Note the resolved review findings in the PR.
- [ ] **Step 5:** Commit docs; the branch is already pushed (PR #229) — the new commits update it.

---

## Notes for the implementer
- **The lock must wrap the LOAD.** Never load a stack outside the lock and then mutate it inside — always `stack.WithLock`, which loads fresh. Any place still doing `LoadStack`/`resolveStack` → mutate → `Save` is a bug.
- **Don't hold the lock across network or human interaction** (`gh` calls, `ui.Confirm`, `FetchAll`/`push` of unrelated work). Take the lock only around the read-modify-write of the stack JSON (and that stack's `local.json` progress). Rebase+push of the CURRENT branch under the lock is acceptable and intended.
- **`local.json` progress is shared** — read-modify-write of `WorktreeProgress` must also be under the stack lock.
- Worktree-mode git ops run in the branch's worktree (`branchWorktreeDir`), never via `Checkout` in the process CWD.
