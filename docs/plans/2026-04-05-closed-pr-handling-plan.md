# Closed PR Handling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a mid-chain PR is closed on GitHub, `sdf sync` skips the closed node and rebases downstream branches onto the nearest open ancestor — same as merged PRs, but with a conflict warning.

**Architecture:** Three existing code paths that check `"merged"` gain parallel `"closed"` handling. A new `"skip-closed"` sync action mirrors `"skip-merged"`. A warning is emitted when downstream branches will rebase past a closed node.

**Tech Stack:** Go 1.24, existing test infrastructure (`syncTestRepo`, `filterActions`)

---

## File Map

| File | Change | Responsibility |
|------|--------|---------------|
| `internal/stack/stack.go:283-295` | Modify | `ParentBranch()` — skip closed nodes |
| `cmd/sync.go:62` | Modify | Add `"skip-closed"` to action kind comment |
| `cmd/sync.go:397-417` | Modify | `hasRealWork` loop — skip closed nodes |
| `cmd/sync.go:419-437` | Modify | Sync execution loop — skip closed nodes |
| `cmd/sync.go:1130-1210` | Modify | `computeSyncPlan()` — skip closed, emit warning |
| `cmd/sync.go:1234-1244` | Modify | `printSyncPlan()` — render skip-closed + warning |
| `cmd/sync_test.go` | Modify | New `computeSyncPlan` tests for closed scenarios |
| `cmd/prnav.go:645` | Modify | `promptCreateMissingPRs` — skip closed nodes |

---

### Task 1: ParentBranch skips closed nodes

**Files:**
- Modify: `internal/stack/stack.go:283-295`
- Test: `internal/stack/stack_test.go` (new test functions)

The `ParentBranch` method currently skips `"merged"` nodes. It needs to also
skip `"closed"` nodes. The existing tests for ParentBranch live alongside the
Stack struct. There are no existing ParentBranch tests — the behavior is
currently tested indirectly through `computeSyncPlan` tests.

- [ ] **Step 1: Write failing test — closed mid-chain node skipped**

Add to `internal/stack/stack_test.go` (create if needed):

```go
func TestParentBranch_SkipsClosedNode(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "a", Status: "open"},
			{Branch: "b", Status: "closed"},
			{Branch: "c", Status: "open"},
		},
	}
	parent := s.ParentBranch("c")
	if parent != "a" {
		t.Errorf("expected parent of c to be a (skipping closed b), got %s", parent)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/stack/ -run TestParentBranch_SkipsClosedNode -v -count=1`
Expected: FAIL — `expected parent of c to be a (skipping closed b), got b`

- [ ] **Step 3: Write failing test — consecutive closed nodes skipped**

```go
func TestParentBranch_SkipsConsecutiveClosedNodes(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "a", Status: "open"},
			{Branch: "b", Status: "closed"},
			{Branch: "c", Status: "closed"},
			{Branch: "d", Status: "open"},
		},
	}
	parent := s.ParentBranch("d")
	if parent != "a" {
		t.Errorf("expected parent of d to be a (skipping closed b,c), got %s", parent)
	}
}
```

- [ ] **Step 4: Write failing test — first node closed falls back to base**

```go
func TestParentBranch_FirstNodeClosedReturnsBase(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "a", Status: "closed"},
			{Branch: "b", Status: "open"},
		},
	}
	parent := s.ParentBranch("b")
	if parent != "main" {
		t.Errorf("expected parent of b to be main (skipping closed a), got %s", parent)
	}
}
```

- [ ] **Step 5: Write failing test — mixed merged and closed**

```go
func TestParentBranch_SkipsMergedAndClosed(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "a", Status: "open"},
			{Branch: "b", Status: "merged"},
			{Branch: "c", Status: "closed"},
			{Branch: "d", Status: "open"},
		},
	}
	parent := s.ParentBranch("d")
	if parent != "a" {
		t.Errorf("expected parent of d to be a (skipping merged b, closed c), got %s", parent)
	}
}
```

- [ ] **Step 6: Run all new tests to verify they fail**

Run: `go test ./internal/stack/ -run 'TestParentBranch_Skips|TestParentBranch_First' -v -count=1`
Expected: All 4 tests FAIL

- [ ] **Step 7: Implement — modify ParentBranch to skip closed**

In `internal/stack/stack.go`, change the loop condition in `ParentBranch`:

```go
// ParentBranch returns the branch that the given branch is based on.
// For the first node, this is the stack base (e.g. "main").
func (s *Stack) ParentBranch(branch string) string {
	idx := s.NodeIndex(branch)
	if idx <= 0 {
		return s.Base
	}
	// Skip over merged and closed nodes — they are no longer active in the chain
	for j := idx - 1; j >= 0; j-- {
		if s.Nodes[j].Status != "merged" && s.Nodes[j].Status != "closed" {
			return s.Nodes[j].Branch
		}
	}
	return s.Base
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/stack/ -run 'TestParentBranch_Skips|TestParentBranch_First' -v -count=1`
Expected: All 4 tests PASS

- [ ] **Step 9: Run full stack package tests**

Run: `go test ./internal/stack/ -count=1`
Expected: All PASS

- [ ] **Step 10: Commit**

```bash
git add internal/stack/stack.go internal/stack/stack_test.go
git commit -m "feat(stack): ParentBranch skips closed nodes (#211)"
```

---

### Task 2: computeSyncPlan skips closed nodes

**Files:**
- Modify: `cmd/sync.go:62,1130-1210`
- Test: `cmd/sync_test.go`

Add `"skip-closed"` action kind and make `computeSyncPlan` skip closed nodes
the same way it skips merged ones. Also skip closed nodes in the content-update
append loop.

- [ ] **Step 1: Write failing test — closed mid-chain produces skip-closed**

Add to `cmd/sync_test.go`:

```go
func TestComputeSyncPlan_ClosedMiddle(t *testing.T) {
	syncTestRepo(t)

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Mark branchB (middle) as closed → branchC rebases onto branchA
	s.Nodes[1].Status = "closed"

	plan := computeSyncPlan(s, nil)

	skipsClosed := filterActions(plan, "skip-closed")
	updateTips := filterActions(plan, "update-tip")

	if len(skipsClosed) != 1 || skipsClosed[0].branch != "branchB" {
		t.Errorf("expected 1 skip-closed for branchB, got %v", skipsClosed)
	}

	if len(updateTips) != 1 || updateTips[0].branch != "branchC" {
		t.Errorf("expected update-tip for branchC, got %v", actionBranches(updateTips))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestComputeSyncPlan_ClosedMiddle -v -count=1`
Expected: FAIL — no `skip-closed` actions (branchB treated as active)

- [ ] **Step 3: Write failing test — closed head node**

```go
func TestComputeSyncPlan_ClosedHead(t *testing.T) {
	syncTestRepo(t)

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Mark branchA (head) as closed
	s.Nodes[0].Status = "closed"

	plan := computeSyncPlan(s, nil)

	skipsClosed := filterActions(plan, "skip-closed")
	if len(skipsClosed) != 1 || skipsClosed[0].branch != "branchA" {
		t.Errorf("expected skip-closed for branchA, got %v", skipsClosed)
	}
}
```

- [ ] **Step 4: Write failing test — closed node skipped in content updates**

```go
func TestComputeSyncPlan_SkipsClosedForUpdates(t *testing.T) {
	syncTestRepo(t)

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Assign PRs to all nodes, mark branchB as closed
	s.Nodes[0].PR = 10
	s.Nodes[1].PR = 20
	s.Nodes[1].Status = "closed"
	s.Nodes[2].PR = 30

	opts := &syncOptions{withContent: true}
	plan := computeSyncPlan(s, opts)

	contentUpdates := filterActions(plan, "update-content")
	for _, a := range contentUpdates {
		if a.branch == "branchB" {
			t.Error("closed branchB should not get content update")
		}
	}
}
```

- [ ] **Step 5: Run all new tests to verify they fail**

Run: `go test ./cmd/ -run 'TestComputeSyncPlan_Closed' -v -count=1`
Expected: All FAIL

- [ ] **Step 6: Implement — add skip-closed to computeSyncPlan**

In `cmd/sync.go`:

1. Update the action kind comment (line 62):
```go
kind   string // "skip-merged", "skip-closed", "update-tip", "rebase", "push", "update-pr-base", "update-content"
```

2. In `computeSyncPlan` (after the `merged` check at line 1141), add:
```go
		if node.Status == "closed" {
			actions = append(actions, syncAction{kind: "skip-closed", branch: node.Branch, pr: node.PR})
			continue
		}
```

3. In the content-update loop (line 1200), add `"closed"` check:
```go
			if node.PR == 0 || node.Status == "merged" || node.Status == "closed" {
				continue
			}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./cmd/ -run 'TestComputeSyncPlan_Closed' -v -count=1`
Expected: All PASS

- [ ] **Step 8: Run existing computeSyncPlan tests**

Run: `go test ./cmd/ -run TestComputeSyncPlan -v -count=1`
Expected: All PASS (no regressions)

- [ ] **Step 9: Commit**

```bash
git add cmd/sync.go cmd/sync_test.go
git commit -m "feat(sync): computeSyncPlan skips closed nodes (#211)"
```

---

### Task 3: Sync execution loop skips closed nodes

**Files:**
- Modify: `cmd/sync.go:397-437`

The main sync execution loop (the `for i := 0; i < len(s.Nodes)` block that
actually runs rebases) checks `node.Status == "merged"` to skip nodes. It
needs to also skip `"closed"`. The `hasRealWork` pre-check loop also needs
the same treatment.

This is not separately testable from the full sync integration tests (it shells
out to git), so we modify it alongside the plan changes and rely on the
`computeSyncPlan` tests for logic coverage.

- [ ] **Step 1: Update hasRealWork loop**

In `cmd/sync.go`, the `hasRealWork` loop (around line 400):

```go
		if s.Nodes[i].Status != "merged" && s.Nodes[i].Status != "closed" {
```

- [ ] **Step 2: Update sync execution skip block**

In the main execution loop (around line 422), after the existing `merged` block,
add a parallel block for closed:

```go
		if node.Status == "closed" {
			if hasRealWork {
				if node.PR > 0 {
					bus.Printf("  %s PR %s (%s) closed", ui.SymWarn, ui.PR(node.PR), ui.Branch(node.Branch))
				} else {
					bus.Printf("  %s %s closed", ui.SymWarn, ui.Branch(node.Branch))
				}
			}
			if result != nil {
				result.Branches = append(result.Branches, BranchResult{
					Branch: node.Branch, PR: node.PR, Action: "closed",
				})
			}
			continue
		}
```

- [ ] **Step 3: Update promptCreateMissingPRs to skip closed**

In `cmd/sync.go`, the `promptCreateMissingPRs` function (around line 645):

```go
		if node.Status != "merged" && node.Status != "closed" && node.PR == 0 {
```

(Currently: `if node.Status != "merged" && node.PR == 0`)

- [ ] **Step 3a: Update checkout-after-sync to handle closed branch**

In `cmd/sync.go` (around line 592), the block that switches away from a merged
original branch should also switch away from a closed one. And the loop finding
the first open branch should skip closed nodes too:

```go
	if node := s.FindNode(originalBranch); node != nil && (node.Status == "merged" || node.Status == "closed") {
		checkoutTarget = s.Base
		for _, n := range s.Nodes {
			if n.Status != "merged" && n.Status != "closed" {
				checkoutTarget = n.Branch
				break
			}
		}
	}
```

- [ ] **Step 3b: Update updatePRContent to skip closed nodes**

In `cmd/sync.go` (around line 728):

```go
		if node.PR == 0 || node.Status == "merged" || node.Status == "closed" {
			continue
		}
```

- [ ] **Step 4: Build and verify**

Run: `go build ./...`
Expected: Compiles without errors

- [ ] **Step 5: Run all sync tests**

Run: `go test ./cmd/ -run 'TestComputeSyncPlan|TestPrintSyncPlan' -count=1`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/sync.go
git commit -m "feat(sync): execution loop skips closed nodes (#211)"
```

---

### Task 4: printSyncPlan renders skip-closed and conflict warning

**Files:**
- Modify: `cmd/sync.go:1234-1260` (printSyncPlan function)
- Test: `cmd/sync_test.go`

Add rendering for `"skip-closed"` action and a conflict warning when the next
active node after a closed skip will rebase past it.

- [ ] **Step 1: Write failing test — printSyncPlan renders skip-closed**

Add to `cmd/sync_test.go`:

```go
func TestPrintSyncPlan_ClosedOutput(t *testing.T) {
	plan := []syncAction{
		{kind: "skip-closed", branch: "feat/auth", pr: 10},
		{kind: "rebase", branch: "feat/api", onto: "feat/base"},
		{kind: "push", branch: "feat/api"},
	}

	var buf bytes.Buffer
	bus := render.NewBus(&buf, io.Discard, render.Options{})

	printSyncPlan(plan, bus)

	_ = bus.Finish()
	output := stripANSI(buf.String())

	if !strings.Contains(output, "PR #10 (feat/auth) closed") {
		t.Errorf("expected closed PR output, got:\n%s", output)
	}
}
```

- [ ] **Step 2: Write failing test — conflict warning after skip-closed**

```go
func TestPrintSyncPlan_ClosedConflictWarning(t *testing.T) {
	plan := []syncAction{
		{kind: "skip-closed", branch: "feat/validation", pr: 5},
		{kind: "rebase", branch: "feat/api", onto: "feat/auth"},
		{kind: "push", branch: "feat/api"},
	}

	var buf bytes.Buffer
	bus := render.NewBus(&buf, io.Discard, render.Options{})

	printSyncPlan(plan, bus)

	_ = bus.Finish()
	output := stripANSI(buf.String())

	if !strings.Contains(output, "feat/api will rebase onto feat/auth") {
		t.Errorf("expected conflict warning for feat/api, got:\n%s", output)
	}
	if !strings.Contains(output, "skipping closed feat/validation") {
		t.Errorf("expected skipped branch name in warning, got:\n%s", output)
	}
}
```

- [ ] **Step 3: Write failing test — no warning when skip-closed has no downstream rebase**

```go
func TestPrintSyncPlan_ClosedTailNoWarning(t *testing.T) {
	// Closed node is last — no downstream to warn about
	plan := []syncAction{
		{kind: "rebase", branch: "feat/auth", onto: "main"},
		{kind: "push", branch: "feat/auth"},
		{kind: "skip-closed", branch: "feat/dead", pr: 99},
	}

	var buf bytes.Buffer
	bus := render.NewBus(&buf, io.Discard, render.Options{})

	printSyncPlan(plan, bus)

	_ = bus.Finish()
	output := stripANSI(buf.String())

	if strings.Contains(output, "will rebase") {
		t.Errorf("should not warn when closed node is at tail, got:\n%s", output)
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./cmd/ -run 'TestPrintSyncPlan_Closed' -v -count=1`
Expected: All FAIL

- [ ] **Step 5: Implement — add skip-closed rendering to printSyncPlan**

In `cmd/sync.go`, in the `printSyncPlan` function, add a new case after
`skip-merged`:

```go
		case "skip-closed":
			if a.pr > 0 {
				bus.Printf("  %s PR %s (%s) closed", ui.SymWarn, ui.PR(a.pr), ui.Branch(a.branch))
			} else {
				bus.Printf("  %s %s closed", ui.SymWarn, ui.Branch(a.branch))
			}
			// Warn if the next action is a rebase (downstream branch rebasing past closed node)
			if i+1 < len(plan) && plan[i+1].kind == "rebase" {
				bus.Printf("  %s %s will rebase onto %s (skipping closed %s) — conflicts possible",
					ui.SymWarn, ui.Branch(plan[i+1].branch), ui.Branch(plan[i+1].onto), ui.Branch(a.branch))
			}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./cmd/ -run 'TestPrintSyncPlan_Closed' -v -count=1`
Expected: All PASS

- [ ] **Step 7: Run all printSyncPlan tests**

Run: `go test ./cmd/ -run TestPrintSyncPlan -v -count=1`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add cmd/sync.go cmd/sync_test.go
git commit -m "feat(sync): render skip-closed with conflict warning in plan output (#211)"
```

---

### Task 5: Final verification

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -count=1 -timeout 180s`
Expected: All PASS (except pre-existing `TestDoctor_GitMissing` timeout)

- [ ] **Step 2: Build and install**

Run: `go install ./...`
Expected: Builds successfully

- [ ] **Step 3: Commit design doc**

```bash
git add docs/plans/2026-04-05-closed-pr-handling-design.md docs/plans/2026-04-05-closed-pr-handling-plan.md
git commit -m "docs: design and plan for closed PR handling (#211)"
```
