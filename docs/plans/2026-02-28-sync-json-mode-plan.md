# Sync --json Mode Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `--json` flag to `sdf sync` that emits structured JSON output, suppresses TTY output, and skips interactive prompts.

**Architecture:** A `SyncResult` struct is populated alongside existing `bus.Print/Warn` calls. When `--json`, the Bus uses `JSONRenderer` to suppress terminal output. After sync completes, the result is marshaled to stdout. Interactive prompts are skipped or abort in JSON mode.

**Tech Stack:** Go, encoding/json, render.Bus, render.JSONRenderer

---

### Task 1: Add result types and --json flag plumbing

**Files:**
- Modify: `cmd/sync.go:23-26` (syncOptions struct)
- Modify: `cmd/sync.go:36-59` (syncCmd, init, flag registration)
- Modify: `cmd/sync.go:61-85` (runSyncCmd)

**Step 1: Add result types to sync.go**

Add after the `syncOptions` struct:

```go
// SyncResult is the structured output of sdf sync when --json is used.
type SyncResult struct {
	Stack     string         `json:"stack"`
	Base      string         `json:"base"`
	Branches  []BranchResult `json:"branches"`
	PRUpdates []PRUpdate     `json:"pr_updates,omitempty"`
	Warnings  []string       `json:"warnings,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// BranchResult describes what happened to a single branch during sync.
type BranchResult struct {
	Branch      string `json:"branch"`
	PR          int    `json:"pr,omitempty"`
	Action      string `json:"action"`
	Pushed      bool   `json:"pushed,omitempty"`
	BaseUpdated bool   `json:"base_updated,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// PRUpdate describes a PR field update during sync.
type PRUpdate struct {
	PR     int    `json:"pr"`
	Field  string `json:"field"`
	Status string `json:"status"`
}
```

**Step 2: Add jsonMode to syncOptions**

```go
type syncOptions struct {
	withContent bool
	jsonMode    bool
	cfg         cfgpkg.Config
}
```

**Step 3: Register --json flag in init()**

Add to init(): `syncCmd.Flags().Bool("json", false, "output result as JSON")`

**Step 4: Update runSyncCmd to handle --json**

```go
func runSyncCmd(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	cont, _ := cmd.Flags().GetBool("continue")
	stackFlag, _ := cmd.Flags().GetString("stack")
	withContent, _ := cmd.Flags().GetBool("with-content")
	jsonFlag, _ := cmd.Flags().GetBool("json")

	stackName := stackFlag
	if stackName == "" && len(args) > 0 {
		stackName = args[0]
	}

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	var renderer render.Renderer
	var jsonRenderer *render.JSONRenderer
	if jsonFlag {
		jsonRenderer = &render.JSONRenderer{}
		renderer = jsonRenderer
	}
	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{Renderer: renderer})
	defer func() { _ = bus.Finish() }()

	var result *SyncResult
	if jsonFlag {
		result = &SyncResult{}
	}

	if cont {
		err = runSyncContinue(root, result, bus)
	} else {
		err = runSyncFull(root, stackName, yes || jsonFlag, withContent, jsonFlag, result, bus)
	}

	if jsonFlag {
		_ = bus.Finish() // flush before JSON output
		if err != nil {
			result.Error = err.Error()
		}
		if jsonRenderer != nil {
			result.Warnings = append(result.Warnings, jsonRenderer.Warnings()...)
		}
		data, marshalErr := json.MarshalIndent(result, "", "  ")
		if marshalErr != nil {
			return fmt.Errorf("cannot marshal result: %w", marshalErr)
		}
		fmt.Println(string(data))
		return nil // error is in the JSON output
	}

	return err
}
```

Add `"encoding/json"` to the imports.

**Step 5: Build and verify**

Run: `go build ./...`
Expected: compilation errors because `runSyncFull` and `runSyncContinue` signatures changed — that's expected, fixed in Task 2.

**Step 6: Commit**

```bash
git add cmd/sync.go
git commit -m "feat(sync): add --json flag plumbing and result types"
```

---

### Task 2: Thread result through runSyncContinue

**Files:**
- Modify: `cmd/sync.go:94-159` (runSyncContinue)

**Step 1: Add `result *SyncResult` parameter**

Change signature to: `func runSyncContinue(root string, result *SyncResult, bus *render.Bus) error`

**Step 2: Populate result when non-nil**

After resolving the stack, set `result.Stack` and `result.Base`. When a branch is rebased/pushed, append to `result.Branches`. Example:

```go
if result != nil {
	result.Stack = s.StackID
	result.Base = s.Base
}
```

After push:
```go
if result != nil {
	result.Branches = append(result.Branches, BranchResult{
		Branch: node.Branch,
		PR:     node.PR,
		Action: "rebased",
		Pushed: true,
	})
}
```

**Step 3: Pass result to runSyncFrom**

Change `runSyncFrom(root, s, progress.ResumeIndex+1, nil, bus)` to include result.

**Step 4: Build and verify**

Run: `go build ./...`
Expected: compilation errors because `runSyncFrom` signature changed — fixed in Task 3.

**Step 5: Commit**

```bash
git add cmd/sync.go
git commit -m "feat(sync): thread result through runSyncContinue"
```

---

### Task 3: Thread result through runSyncFull

**Files:**
- Modify: `cmd/sync.go:162-261` (runSyncFull)

**Step 1: Add `jsonMode bool` and `result *SyncResult` parameters**

Change signature to: `func runSyncFull(root, stackName string, skipConfirm, flagWithContent, jsonMode bool, result *SyncResult, bus *render.Bus) error`

**Step 2: Populate result with stack info**

After resolving the stack:
```go
if result != nil {
	result.Stack = s.StackID
	result.Base = s.Base
}
```

**Step 3: Add jsonMode to syncOptions**

```go
opts := syncOptions{
	withContent: cfg.WithContentEnabled() || flagWithContent,
	jsonMode:    jsonMode,
	cfg:         cfg,
}
```

**Step 4: Pass result through to downstream calls**

- `runSyncFrom(root, s, 0, &opts, result, bus)`
- `promptCreateMissingPRs(root, s, opts, result, bus)` — skip when json
- `updatePRContent(root, s, opts, result, bus)` — collect PRUpdates
- `updateStackNavForAllPRs(root, s, result, bus)` — collect PRUpdates

**Step 5: Update merge.go call site**

The `runSyncFull` call in `cmd/merge.go` needs the new parameters. Pass `false` for jsonMode and `nil` for result.

**Step 6: Build and verify**

Run: `go build ./...`
Expected: may still have errors if downstream signatures haven't changed yet.

**Step 7: Commit**

```bash
git add cmd/sync.go cmd/merge.go
git commit -m "feat(sync): thread result through runSyncFull"
```

---

### Task 4: Thread result through runSyncFrom

**Files:**
- Modify: `cmd/sync.go:265-416` (runSyncFrom)

**Step 1: Add `result *SyncResult` parameter**

Change signature to: `func runSyncFrom(root string, s *stack.Stack, startIndex int, opts *syncOptions, result *SyncResult, bus *render.Bus) error`

**Step 2: Populate BranchResult for each branch**

In the rebase loop, after each branch action, append to result.Branches:

For merged branches:
```go
if result != nil {
	result.Branches = append(result.Branches, BranchResult{
		Branch: node.Branch, PR: node.PR, Action: "merged",
	})
}
```

For rebased + pushed:
```go
if result != nil {
	result.Branches = append(result.Branches, BranchResult{
		Branch: node.Branch, PR: node.PR, Action: "rebased",
		Pushed: pushOK, BaseUpdated: baseUpdated,
	})
}
```

For blocked:
```go
if result != nil {
	result.Branches = append(result.Branches, BranchResult{
		Branch: node.Branch, PR: node.PR, Action: "blocked",
		Reason: "depends on a branch that failed",
	})
}
```

For conflicts (when jsonMode):
```go
if opts != nil && opts.jsonMode {
	if result != nil {
		result.Branches = append(result.Branches, BranchResult{
			Branch: node.Branch, PR: node.PR, Action: "failed",
			Reason: "conflict",
		})
	}
	gitpkg.RebaseAbort()
	return fmt.Errorf("conflict in %s — cannot resolve in --json mode", node.Branch)
}
```

**Step 3: Pass result to promptOnConflict**

When NOT in json mode, pass result to `promptOnConflict` so it can record failures.

**Step 4: Pass result to downstream PR update functions**

- `updatePRContent(root, s, opts, result, bus)`
- `updateStackNavForAllPRs(root, s, result, bus)`

**Step 5: Build and verify**

Run: `go build ./...`

**Step 6: Commit**

```bash
git add cmd/sync.go
git commit -m "feat(sync): populate branch results in runSyncFrom"
```

---

### Task 5: Skip prompts in JSON mode

**Files:**
- Modify: `cmd/sync.go` — `promptCreateMissingPRs`, `promptOnConflict`

**Step 1: Skip promptCreateMissingPRs when jsonMode**

Add `result *SyncResult` param to signature. At the top:
```go
if opts != nil && opts.jsonMode {
	return
}
```

**Step 2: Handle conflict abort in JSON mode**

In `promptOnConflict`, the JSON path is already handled in Task 4 (runSyncFrom aborts before calling promptOnConflict). No change needed here.

**Step 3: Build and verify**

Run: `go build ./...`
Expected: PASS

**Step 4: Run tests**

Run: `go test ./cmd/... -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/sync.go
git commit -m "feat(sync): skip interactive prompts in json mode"
```

---

### Task 6: Collect PRUpdate results

**Files:**
- Modify: `cmd/sync.go` — `updatePRContent`
- Modify: `cmd/prnav.go` — `updateStackNavForAllPRs`

**Step 1: Add result param to updatePRContent**

Change signature to include `result *SyncResult`. In each task Fn, after a successful update:
```go
if result != nil {
	result.PRUpdates = append(result.PRUpdates, PRUpdate{
		PR: j.node.PR, Field: "title", Status: "updated",
	})
}
```

Use a mutex since tasks run in parallel. Or collect from task results after RunBatch.

Actually, simpler: use the existing `updated` counter pattern but also collect structured updates. Since the task closures capture `result`, and `result.PRUpdates` append needs sync, use a mutex:

```go
var mu sync.Mutex
// In task Fn:
mu.Lock()
result.PRUpdates = append(result.PRUpdates, PRUpdate{...})
mu.Unlock()
```

**Step 2: Add result param to updateStackNavForAllPRs**

Change signature to include `result *SyncResult`. Same pattern — after successful nav update:
```go
if result != nil {
	mu.Lock()
	result.PRUpdates = append(result.PRUpdates, PRUpdate{
		PR: node.PR, Field: "nav", Status: "updated",
	})
	mu.Unlock()
}
```

The existing `mu sync.Mutex` in updateStackNavForAllPRs can be reused.

**Step 3: Update all call sites**

- `cmd/sync.go`: pass result to both functions
- `cmd/pr.go`: pass nil for result to `updateStackNavForAllPRs`
- `cmd/split.go`: pass nil for result to `updateStackNavForAllPRs`

**Step 4: Build and verify**

Run: `go build ./...`

**Step 5: Run tests**

Run: `go test ./... -count=1`

**Step 6: Commit**

```bash
git add cmd/sync.go cmd/prnav.go cmd/pr.go cmd/split.go
git commit -m "feat(sync): collect PR update results for json output"
```

---

### Task 7: Add tests for JSON output

**Files:**
- Modify: `cmd/sync_test.go`

**Step 1: Test SyncResult serialization**

```go
func TestSyncResultJSON(t *testing.T) {
	result := SyncResult{
		Stack: "my-feature",
		Base:  "main",
		Branches: []BranchResult{
			{Branch: "feat-a", PR: 42, Action: "merged"},
			{Branch: "feat-b", PR: 43, Action: "rebased", Pushed: true, BaseUpdated: true},
		},
		PRUpdates: []PRUpdate{
			{PR: 43, Field: "nav", Status: "updated"},
		},
		Warnings: []string{"fetch failed"},
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var roundtrip SyncResult
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if roundtrip.Stack != "my-feature" {
		t.Errorf("stack: got %q, want %q", roundtrip.Stack, "my-feature")
	}
	if len(roundtrip.Branches) != 2 {
		t.Fatalf("branches: got %d, want 2", len(roundtrip.Branches))
	}
	if roundtrip.Branches[1].Action != "rebased" {
		t.Errorf("branches[1].action: got %q, want %q", roundtrip.Branches[1].Action, "rebased")
	}
	if !roundtrip.Branches[1].Pushed {
		t.Error("branches[1].pushed: got false, want true")
	}
}
```

**Step 2: Test empty result omits optional fields**

```go
func TestSyncResultJSON_EmptyOmitsOptional(t *testing.T) {
	result := SyncResult{Stack: "test", Base: "main"}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	s := string(data)
	if strings.Contains(s, "pr_updates") {
		t.Error("empty pr_updates should be omitted")
	}
	if strings.Contains(s, "warnings") {
		t.Error("empty warnings should be omitted")
	}
	if strings.Contains(s, "error") {
		t.Error("empty error should be omitted")
	}
}
```

**Step 3: Run tests**

Run: `go test ./cmd/... -count=1 -run TestSyncResult`
Expected: PASS

**Step 4: Run full test suite**

Run: `go test ./... -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/sync_test.go
git commit -m "test(sync): add JSON result serialization tests"
```

---

### Task 8: Verify complete integration

**Step 1: Check no fmt.Print* to stdout/stderr in sync.go**

Run: `grep -n 'fmt\.Print\|fmt\.Fprint' cmd/sync.go | grep -v 'fmt\.Errorf\|fmt\.Fprintf(&b\|fmt\.Sprintf'`
Expected: zero matches (or only error formatting)

**Step 2: Run full test suite**

Run: `go test ./... -count=1`
Expected: all pass

**Step 3: Build and install**

Run: `go install ./...`
Expected: success

**Step 4: Commit (if any fixes)**

Only if Task 8 found issues to fix.
