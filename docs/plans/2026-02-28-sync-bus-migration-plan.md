# Sync Command Bus Migration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Route all sync command output through a single `render.Bus`, eliminating all direct `fmt.Print*` calls to stdout/stderr in `cmd/sync.go` and `cmd/prnav.go`.

**Architecture:** Create one Bus in `runSyncCmd`, thread it as an explicit `bus *render.Bus` parameter to every function that produces output. Interactive prompts wrapped with `bus.Pause()`/`bus.Resume()`. `updatePRContent` and `updateStackNavForAllPRs` stop creating their own Bus and use the parent bus with `SetLabel` + `AddTask` + `RunBatch`.

**Tech Stack:** Go, `internal/render` (Bus, Events), `internal/ui` (huh widgets)

---

### Task 1: Add SetLabel to Bus and plumb bus parameter through all function signatures

Pure plumbing — add the bus parameter to every function that will need it, update all call sites to pass it. No output changes yet. Code must compile and all tests must pass.

**Files:**
- Modify: `internal/render/bus.go` (add SetLabel method)
- Modify: `cmd/sync.go` (11 function signatures + all call sites)
- Modify: `cmd/prnav.go` (1 function signature)
- Modify: `cmd/sync_test.go` (1 test that calls `printSyncPlan` directly)

**Step 1: Add SetLabel to Bus**

In `internal/render/bus.go`, add after the `Resume` method (after line 132):

```go
// SetLabel changes the label used for the next RunBatch spinner line.
func (b *Bus) SetLabel(label string) {
	b.label = label
}
```

**Step 2: Create bus in runSyncCmd and add bus param to entry points**

In `cmd/sync.go`, modify `runSyncCmd` (line 61–82):

```go
func runSyncCmd(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	cont, _ := cmd.Flags().GetBool("continue")
	stackFlag, _ := cmd.Flags().GetString("stack")
	withContent, _ := cmd.Flags().GetBool("with-content")

	stackName := stackFlag
	if stackName == "" && len(args) > 0 {
		stackName = args[0]
	}

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{})
	defer func() { _ = bus.Finish() }()

	if cont {
		return runSyncContinue(root, bus)
	}

	return runSyncFull(root, stackName, yes, withContent, bus)
}
```

**Step 3: Add bus param to all function signatures and update call sites**

Change each signature and every call site. Only the signature and call site change — function bodies stay the same for now.

| Function | New signature |
|---|---|
| `runSyncContinue` | `func runSyncContinue(root string, bus *render.Bus) error` |
| `runSyncFull` | `func runSyncFull(root, stackName string, skipConfirm, flagWithContent bool, bus *render.Bus) error` |
| `runSyncFrom` | `func runSyncFrom(root string, s *stack.Stack, startIndex int, opts *syncOptions, bus *render.Bus) error` |
| `reconcileSyncPRStates` | `func reconcileSyncPRStates(s *stack.Stack, bus *render.Bus)` |
| `printSyncPlan` | `func printSyncPlan(plan []syncAction, bus *render.Bus)` |
| `promptOnConflict` | `func promptOnConflict(root string, s *stack.Stack, branch, originalBranch string, nodeIndex int, rebaseErr error, bus *render.Bus) (conflictAction, error)` |
| `tryClaude` | `func tryClaude(root string, s *stack.Stack, branch, originalBranch string, nodeIndex int, rebaseErr error, conflicted []string, bus *render.Bus) (conflictAction, error)` |
| `pauseForManualResolution` | `func pauseForManualResolution(root string, s *stack.Stack, branch, originalBranch string, nodeIndex int, bus *render.Bus) (conflictAction, error)` |
| `promptCreateMissingPRs` | `func promptCreateMissingPRs(root string, s *stack.Stack, opts *syncOptions, bus *render.Bus)` |
| `updatePRContent` | `func updatePRContent(_ string, s *stack.Stack, opts *syncOptions, bus *render.Bus)` |
| `updateStackNavForAllPRs` (prnav.go) | `func updateStackNavForAllPRs(root string, s *stack.Stack, bus *render.Bus) error` |

Call sites to update (search for each function name and add `bus` as the last argument):

- `runSyncContinue`: called at L78 → `runSyncContinue(root, bus)`
- `runSyncFull`: called at L81, L117 → add `bus` as last arg
- `runSyncFrom`: called at L155, L254 → add `bus` as last arg
- `reconcileSyncPRStates`: called at L203 → `reconcileSyncPRStates(s, bus)`
- `printSyncPlan`: called at L243 → `printSyncPlan(plan, bus)`
- `promptOnConflict`: called at L317 → add `bus` as last arg
- `tryClaude`: called at L950 → add `bus` as last arg
- `pauseForManualResolution`: called at L952, L1012 → add `bus` as last arg
- `promptCreateMissingPRs`: called at L398 → add `bus` as last arg
- `updatePRContent`: called at L401 → add `bus` as last arg
- `updateStackNavForAllPRs`: called at L237, L405 → add `bus` as last arg

**Step 4: Update the printSyncPlan test**

The test at `cmd/sync_test.go:462` (`TestPrintSyncPlan_Output`) calls `printSyncPlan(plan)` directly. Update it to create a bus and pass it:

```go
func TestPrintSyncPlan_Output(t *testing.T) {
	plan := []syncAction{
		{kind: "skip-merged", branch: "feat/auth", pr: 10},
		{kind: "rebase", branch: "feat/api", onto: "main"},
		{kind: "push", branch: "feat/api"},
		{kind: "update-pr-base", branch: "feat/api", pr: 42, onto: "main"},
		{kind: "update-content", branch: "feat/api", pr: 42},
	}

	var buf bytes.Buffer
	bus := render.NewBus(&buf, io.Discard, render.Options{})

	printSyncPlan(plan, bus)

	_ = bus.Finish()

	output := stripANSI(buf.String())

	// Verify each action type appears in the output
	checks := []struct {
		label    string
		contains string
	}{
		{"header", "Sync plan:"},
		{"merged", "PR #10 (feat/auth) merged"},
		{"rebase+push", "rebase feat/api onto main + push"},
		{"pr-base", "update PR #42 base"},
		{"content", "update PR #42 content"},
	}

	for _, c := range checks {
		if !strings.Contains(output, c.contains) {
			t.Errorf("printSyncPlan output missing %s: expected to contain %q\ngot:\n%s",
				c.label, c.contains, output)
		}
	}

	// Verify rebase+push are combined (no separate "push feat/api" line)
	if strings.Contains(output, "push feat/api\n") {
		t.Error("printSyncPlan should combine rebase+push, but found separate push line")
	}
}
```

Add `"io"` and `"github.com/pavelpascari/sdf/internal/render"` to `sync_test.go` imports.

**Step 5: Verify**

Run: `go build ./... && go test ./cmd/... -count=1 -run 'TestComputeSyncPlan|TestPrintSyncPlan|TestBuildDescription'`

Expected: All tests pass. Code compiles.

**Step 6: Commit**

```bash
git add internal/render/bus.go cmd/sync.go cmd/prnav.go cmd/sync_test.go
git commit -m "refactor(sync): plumb render.Bus through all sync functions

Add bus parameter to all sync functions and create the bus in
runSyncCmd. No output changes yet — this is pure plumbing to
prepare for the fmt.Print* → bus.Print migration."
```

---

### Task 2: Convert runSyncContinue output to use bus

Replace all `fmt.Print*` calls in `runSyncContinue` with bus methods.

**Files:**
- Modify: `cmd/sync.go:91–156`

**Step 1: Convert output calls**

Replace each `fmt.Print*` call in `runSyncContinue`:

| Line | Before | After |
|---|---|---|
| 104 | `fmt.Printf("  rebasing %s (continuing)...\n", ui.Branch(progress.PausedAt))` | `bus.Printf("  rebasing %s (continuing)...", ui.Branch(progress.PausedAt))` |
| 111 | `fmt.Printf("  %s %s rebased (completed outside sdf)\n", ui.SymOK, ui.Branch(progress.PausedAt))` | `bus.Printf("  %s %s rebased (completed outside sdf)", ui.SymOK, ui.Branch(progress.PausedAt))` |
| 114 | `fmt.Printf("Rebase of %s was aborted. Starting a fresh sync.\n", ui.Branch(progress.PausedAt))` | `bus.Printf("Rebase of %s was aborted. Starting a fresh sync.", ui.Branch(progress.PausedAt))` |
| 130 | `fmt.Fprintf(os.Stderr, "  %s push failed for %s: %v\n", ui.SymFail, ui.Branch(node.Branch), err)` | `bus.Warnf("  %s push failed for %s: %v", ui.SymFail, ui.Branch(node.Branch), err)` |
| 132 | `fmt.Printf("  %s %s rebased and pushed\n", ui.SymOK, ui.Branch(node.Branch))` | `bus.Printf("  %s %s rebased and pushed", ui.SymOK, ui.Branch(node.Branch))` |
| 138 | `fmt.Fprintf(os.Stderr, "  %s could not update PR %s base: %v\n", ui.SymWarn, ui.PR(node.PR), err)` | `bus.Warnf("  %s could not update PR %s base: %v", ui.SymWarn, ui.PR(node.PR), err)` |
| 140 | `fmt.Printf("  %s PR %s base updated to %s\n", ui.SymOK, ui.PR(node.PR), ui.Branch(parent))` | `bus.Printf("  %s PR %s base updated to %s", ui.SymOK, ui.PR(node.PR), ui.Branch(parent))` |
| 154 | `fmt.Println("\nResuming sync for remaining branches...")` | `bus.Print("\nResuming sync for remaining branches...")` |

Note: `bus.Printf` and `bus.Print` append `\n` automatically (TTYRenderer adds it), so remove the trailing `\n` from format strings.

**Step 2: Verify**

Run: `go build ./... && go test ./cmd/... -count=1`

Expected: All tests pass.

**Step 3: Commit**

```bash
git add cmd/sync.go
git commit -m "refactor(sync): convert runSyncContinue output to bus"
```

---

### Task 3: Convert runSyncFull output to use bus

Replace all `fmt.Print*` calls in `runSyncFull` and add Pause/Resume around `confirmSync()`.

**Files:**
- Modify: `cmd/sync.go:159–255`

**Step 1: Convert output calls**

| Line | Before | After |
|---|---|---|
| 179 | `fmt.Printf("No branches in stack %q. Nothing to sync.\n", s.StackID)` | `bus.Printf("No branches in stack %q. Nothing to sync.", s.StackID)` |
| 191 | `fmt.Printf("Syncing stack %s...\n", ui.Bold.Render(s.StackID))` | `bus.Printf("Syncing stack %s...", ui.Bold.Render(s.StackID))` |
| 192 | `fmt.Println("Fetching from origin...")` | `bus.Print("Fetching from origin...")` |
| 194 | `fmt.Fprintf(os.Stderr, "warning: fetch failed: %v\n", err)` | `bus.Warnf("warning: fetch failed: %v", err)` |
| 199 | `fmt.Fprintf(os.Stderr, "warning: could not fast-forward %s: %v\n", s.Base, err)` | `bus.Warnf("warning: could not fast-forward %s: %v", s.Base, err)` |
| 209 | `fmt.Fprintf(os.Stderr, "warning: could not load config: %v\n", err)` | `bus.Warnf("warning: could not load config: %v", err)` |
| 231 | `fmt.Println("\nEverything is in sync.")` | `bus.Print("\nEverything is in sync.")` |
| 234 | `fmt.Fprintf(os.Stderr, "warning: could not save stack: %v\n", err)` | `bus.Warnf("warning: could not save stack: %v", err)` |
| 238 | `fmt.Fprintf(os.Stderr, "warning: could not update PR descriptions: %v\n", err)` | `bus.Warnf("warning: could not update PR descriptions: %v", err)` |
| 247 | `fmt.Println("Aborted.")` | `bus.Print("Aborted.")` |
| 252 | `fmt.Println()` | `bus.Print("")` |

**Step 2: Wrap confirmSync with Pause/Resume**

Replace lines 245–250:

```go
	if !skipConfirm {
		bus.Pause()
		ok := confirmSync()
		bus.Resume()
		if !ok {
			bus.Print("Aborted.")
			return nil
		}
	}
```

**Step 3: Verify**

Run: `go build ./... && go test ./cmd/... -count=1`

**Step 4: Commit**

```bash
git add cmd/sync.go
git commit -m "refactor(sync): convert runSyncFull output to bus"
```

---

### Task 4: Convert runSyncFrom output to use bus

Replace all `fmt.Print*` calls in `runSyncFrom`.

**Files:**
- Modify: `cmd/sync.go:259–410`

**Step 1: Convert output calls**

| Line | Before | After |
|---|---|---|
| 289 | `fmt.Printf("  %s PR %s (%s) merged\n", ...)` | `bus.Printf("  %s PR %s (%s) merged", ...)` |
| 291 | `fmt.Printf("  %s %s merged\n", ...)` | `bus.Printf("  %s %s merged", ...)` |
| 302 | `fmt.Printf("  %s skipping %s — depends on a branch that failed\n", ...)` | `bus.Printf("  %s skipping %s — depends on a branch that failed", ...)` |
| 314 | `fmt.Printf("  rebasing %s onto %s...\n", ...)` | `bus.Printf("  rebasing %s onto %s...", ...)` |
| 335 | `fmt.Fprintf(os.Stderr, "  %s push failed for %s: %v\n", ...)` | `bus.Warnf("  %s push failed for %s: %v", ...)` |
| 337 | `fmt.Printf("  %s %s rebased and pushed\n", ...)` | `bus.Printf("  %s %s rebased and pushed", ...)` |
| 349 | `fmt.Fprintf(os.Stderr, "  %s could not update PR %s base: %v\n", ...)` | `bus.Warnf("  %s could not update PR %s base: %v", ...)` |
| 359 | `fmt.Printf("  %s %d PR base(s) updated on GitHub\n", ...)` | `bus.Printf("  %s %d PR base(s) updated on GitHub", ...)` |
| 383 | `fmt.Printf("\nSync partially complete. %d branch(es) failed:\n", ...)` | `bus.Printf("\nSync partially complete. %d branch(es) failed:", ...)` |
| 385 | `fmt.Printf("  %s %s: %v\n", ...)` | `bus.Printf("  %s %s: %v", ...)` |
| 387 | ``fmt.Println("\nRun `sdf sync` again to retry.")`` | ``bus.Print("\nRun `sdf sync` again to retry.")`` |
| 392 | `fmt.Println("\nSync complete. Stack updated.")` | `bus.Print("\nSync complete. Stack updated.")` |
| 394 | `fmt.Println("\nEverything is in sync.")` | `bus.Print("\nEverything is in sync.")` |
| 406 | `fmt.Fprintf(os.Stderr, "warning: could not update PR descriptions: %v\n", ...)` | `bus.Warnf("warning: could not update PR descriptions: %v", ...)` |

**Step 2: Verify**

Run: `go build ./... && go test ./cmd/... -count=1`

**Step 3: Commit**

```bash
git add cmd/sync.go
git commit -m "refactor(sync): convert runSyncFrom output to bus"
```

---

### Task 5: Convert helper function output to use bus

Convert all remaining `fmt.Print*` calls in `reconcileSyncPRStates`, `printSyncPlan`, `promptOnConflict`, `tryClaude`, `pauseForManualResolution`, and `promptCreateMissingPRs`. Add Pause/Resume around interactive prompts.

**Files:**
- Modify: `cmd/sync.go` (6 functions)

**Step 1: Convert reconcileSyncPRStates (line 763)**

| Line | Before | After |
|---|---|---|
| 771 | `fmt.Fprintf(os.Stderr, "warning: could not poll PR states: %v\n", err)` | `bus.Warnf("warning: could not poll PR states: %v", err)` |
| 800 | `fmt.Println()` | `bus.Print("")` |
| 803 | `fmt.Fprintf(os.Stderr, "  %s %s\n", ui.SymWarn, c.Detail)` | `bus.Warnf("  %s %s", ui.SymWarn, c.Detail)` |
| 807 | ``fmt.Fprintf(os.Stderr, "\n  Run `sdf fetch` to reconcile structural changes.\n\n")`` | ``bus.Warn("\n  Run `sdf fetch` to reconcile structural changes.\n")`` |

**Step 2: Convert printSyncPlan (line 877)**

| Line | Before | After |
|---|---|---|
| 878 | `fmt.Println("\nSync plan:")` | `bus.Print("\nSync plan:")` |
| 884 | `fmt.Printf("  %s PR %s (%s) merged\n", ...)` | `bus.Printf("  %s PR %s (%s) merged", ...)` |
| 886 | `fmt.Printf("  %s %s merged\n", ...)` | `bus.Printf("  %s %s merged", ...)` |
| 891 | `fmt.Printf("  %s rebase %s onto %s + push\n", ...)` | `bus.Printf("  %s rebase %s onto %s + push", ...)` |
| 894 | `fmt.Printf("  %s rebase %s onto %s\n", ...)` | `bus.Printf("  %s rebase %s onto %s", ...)` |
| 897 | `fmt.Printf("  %s push %s\n", ...)` | `bus.Printf("  %s push %s", ...)` |
| 899 | `fmt.Printf("  %s update PR %s base → %s\n", ...)` | `bus.Printf("  %s update PR %s base → %s", ...)` |
| 901 | `fmt.Printf("  %s update PR %s content\n", ...)` | `bus.Printf("  %s update PR %s content", ...)` |
| 904 | `fmt.Println()` | `bus.Print("")` |

**Step 3: Convert promptOnConflict (line 921) — add Pause/Resume**

| Line | Before | After |
|---|---|---|
| 928 | `fmt.Printf("  %s conflict in %s — %d file(s):\n", ...)` | `bus.Printf("  %s conflict in %s — %d file(s):", ...)` |
| 930 | `fmt.Printf("    %s\n", f)` | `bus.Printf("    %s", f)` |
| 931 | `fmt.Println()` | `bus.Print("")` |
| 955 | `fmt.Printf("  Skipped %s.\n", branch)` | `bus.Printf("  Skipped %s.", branch)` |

Wrap the `ui.Select` call (line 946) with Pause/Resume:

```go
	bus.Pause()
	choice := ui.Select("How would you like to resolve?", options)
	bus.Resume()
```

**Step 4: Convert tryClaude (line 965) — add Pause/Resume**

| Line | Before | After |
|---|---|---|
| 966 | `fmt.Println("  invoking Claude for conflict resolution...")` | `bus.Print("  invoking Claude for conflict resolution...")` |
| 994 | `fmt.Printf("  %s conflict resolved by Claude\n", ui.SymOK)` | `bus.Printf("  %s conflict resolved by Claude", ui.SymOK)` |
| 1001 | `fmt.Println("  Claude couldn't fully resolve the conflicts.")` | `bus.Print("  Claude couldn't fully resolve the conflicts.")` |
| 1002 | `fmt.Println()` | `bus.Print("")` |

Wrap the fallback `ui.Select` call (line 1004) with Pause/Resume:

```go
	bus.Pause()
	choice := ui.Select("What next?", []huh.Option[string]{
		huh.NewOption("I'll fix the rest myself (pauses sync)", "manual"),
		huh.NewOption("Skip this branch, continue with the rest", "skip"),
		huh.NewOption("Abort sync entirely", "abort"),
	})
	bus.Resume()
```

**Step 5: Convert pauseForManualResolution (line 1025)**

| Line | Before | After |
|---|---|---|
| 1040 | `fmt.Printf("\n  Sync paused. Resolve conflicts in %s, then:\n", ui.Branch(branch))` | `bus.Printf("\n  Sync paused. Resolve conflicts in %s, then:", ui.Branch(branch))` |
| 1041 | `fmt.Println()` | `bus.Print("")` |
| 1042 | `fmt.Println("    1. Edit the conflicted files")` | `bus.Print("    1. Edit the conflicted files")` |
| 1043 | `fmt.Println("    2. git add <resolved files>")` | `bus.Print("    2. git add <resolved files>")` |
| 1044 | `fmt.Println("    3. sdf sync --continue")` | `bus.Print("    3. sdf sync --continue")` |
| 1045 | `fmt.Println()` | `bus.Print("")` |

**Step 6: Convert promptCreateMissingPRs (line 414) — add Pause/Resume**

| Line | Before | After |
|---|---|---|
| 434 | `fmt.Println()` | `bus.Print("")` |
| 449 | `fmt.Fprintf(os.Stderr, "  %s could not push %s: %v\n", ...)` | `bus.Warnf("  %s could not push %s: %v", ...)` |
| 456 | `fmt.Fprintf(os.Stderr, "  %s could not create PR: %v\n", ...)` | `bus.Warnf("  %s could not create PR: %v", ...)` |
| 466 | `fmt.Printf("  %s PR created: %s\n", ...)` | `bus.Printf("  %s PR created: %s", ...)` |
| 470 | `fmt.Fprintf(os.Stderr, "  %s could not save stack: %v\n", ...)` | `bus.Warnf("  %s could not save stack: %v", ...)` |

Wrap the `ui.Confirm` call (line 441) with Pause/Resume:

```go
		bus.Pause()
		ok := ui.Confirm(fmt.Sprintf("%s has no PR. Create one?", ui.Branch(node.Branch)))
		bus.Resume()
		if !ok {
			continue
		}
```

**Step 7: Verify**

Run: `go build ./... && go test ./cmd/... -count=1`

**Step 8: Commit**

```bash
git add cmd/sync.go
git commit -m "refactor(sync): convert helper function output to bus

Convert reconcileSyncPRStates, printSyncPlan, promptOnConflict,
tryClaude, pauseForManualResolution, and promptCreateMissingPRs.
Wrap interactive prompts with bus.Pause/Resume."
```

---

### Task 6: Convert updatePRContent and updateStackNavForAllPRs to use parent bus

Remove internal bus creation from both functions. Use the parent bus's `SetLabel` + `AddTask` + `RunBatch` instead. Remove `Finish()` calls (the parent bus handles lifecycle).

**Files:**
- Modify: `cmd/sync.go:477–611` (`updatePRContent`)
- Modify: `cmd/prnav.go:103–208` (`updateStackNavForAllPRs`)

**Step 1: Convert updatePRContent**

Remove these lines (bus creation and finish):
- Line 536: `var updated atomic.Int32` — keep (still needed for counting)
- Line 536–537: `bus := render.NewBus(...)` and `bus.Print("")` — replace with:

```go
	bus.Print("")
	bus.SetLabel("Updating PR content")
```

Remove the `Finish` block at the end (lines 608–610):
```go
	// DELETE these lines:
	// if err := bus.Finish(); err != nil {
	// 	fmt.Fprintf(os.Stderr, "warning: could not flush render log: %v\n", err)
	// }
```

The rest of the function body (AddTask, RunBatch, Warnf, Printf, Print) already uses `bus` — they now go through the parent bus.

**Step 2: Convert updateStackNavForAllPRs**

In `cmd/prnav.go`, modify `updateStackNavForAllPRs`:

Remove the bus creation (lines 158–159):
```go
	// DELETE these lines:
	// bus := render.NewBus(os.Stdout, os.Stderr, render.Options{Label: "Updating PR navigation"})
	// bus.Print("")
```

Replace with:
```go
	bus.Print("")
	bus.SetLabel("Updating PR navigation")
```

Remove the `Finish` block at the end (lines 203–205):
```go
	// DELETE these lines:
	// if err := bus.Finish(); err != nil {
	// 	fmt.Fprintf(os.Stderr, "warning: could not flush render log: %v\n", err)
	// }
```

The rest (AddTask, RunBatch, Warnf, Printf) already uses `bus`.

**Step 3: Clean up prnav.go imports**

Remove `"os"` from the import block in `cmd/prnav.go` — it's no longer used (was only for `os.Stdout` and `os.Stderr` in bus creation).

**Step 4: Verify**

Run: `go build ./... && go test ./... -count=1`

Expected: All tests pass. Full test suite green.

**Step 5: Commit**

```bash
git add cmd/sync.go cmd/prnav.go
git commit -m "refactor(sync): use parent bus in updatePRContent and updateStackNavForAllPRs

Remove internal bus creation from both functions. They now use
the parent bus's SetLabel + AddTask + RunBatch, with lifecycle
managed by the entry point."
```

---

### Task 7: Final cleanup and verification

Verify zero direct `fmt.Print*` calls remain in sync.go (except `fmt.Errorf` and `fmt.Fprintf` to `strings.Builder`). Run full test suite.

**Files:**
- Possibly modify: `cmd/sync.go` (import cleanup)

**Step 1: Grep for remaining fmt.Print calls**

Run: `grep -n 'fmt\.Print\|fmt\.Fprint' cmd/sync.go | grep -v 'fmt\.Errorf' | grep -v '&b,'`

Expected output: Zero lines. Every `fmt.Print*` call should now be a `bus.Print*` call. The only `fmt` usage remaining should be:
- `fmt.Errorf(...)` — error values
- `fmt.Sprintf(...)` — string construction (task IDs, names)
- `fmt.Fprintf(&b, ...)` — writing to `strings.Builder`

**Step 2: Grep prnav.go similarly**

Run: `grep -n 'fmt\.Print\|fmt\.Fprint' cmd/prnav.go | grep -v 'fmt\.Errorf' | grep -v '&b,'`

Expected: Zero lines of direct output. Only `fmt.Fprintf(&b, ...)` for `buildStackNav`/`navHash` string construction.

**Step 3: Run full test suite**

Run: `go test ./... -count=1`

Expected: All tests pass.

**Step 4: Build and verify**

Run: `go build ./...`

Expected: Clean build, no errors.

**Step 5: Commit (if any cleanup was needed)**

```bash
git add cmd/sync.go cmd/prnav.go
git commit -m "refactor(sync): final cleanup after bus migration

Remove unused imports and verify zero direct fmt.Print* calls
to stdout/stderr in sync command."
```
