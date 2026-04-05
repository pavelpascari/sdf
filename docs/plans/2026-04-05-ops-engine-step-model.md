# Operation Engine: Step Model & Executor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `internal/ops/` — the step model, executor, validation, persistence, and recovery that all sdf commands will use.

**Architecture:** Steps are data (ID, Kind, Inputs with literal/ref values, Outputs, Status, Phase). The Executor validates the step graph, runs steps in order, resolves refs at execution time, persists progress for crash safety, and provides --continue/--abort/--quit recovery. Commands build `[]Step` and hand them to the executor.

**Tech Stack:** Go 1.24, no new dependencies. Uses existing `internal/git`, `internal/gh`, `internal/render` packages. Persists to `.sdf/local.json` via existing `stack.LoadLocal`/`SaveLocal`.

**Branch:** `ops-engine_step-model` (stack: `ops-engine`, base: `v0.5-dev`)

**Scope:** This plan covers only the `internal/ops/` package — the foundation. Migrating existing commands (restack, sync, move) to use this package is a separate plan per command, each on its own stack branch.

---

### Task 1: Step and Value types

**Files:**
- Create: `internal/ops/step.go`
- Test: `internal/ops/step_test.go`

- [ ] **Step 1: Write failing tests for Step and Value construction**

```go
// internal/ops/step_test.go
package ops

import "testing"

func TestLit(t *testing.T) {
	v := Lit("abc123")
	if v.Literal != "abc123" || v.Ref != "" {
		t.Fatalf("Lit() returned %+v", v)
	}
}

func TestRef(t *testing.T) {
	v := Ref("rebase-auth.new_sha")
	if v.Ref != "rebase-auth.new_sha" || v.Literal != "" {
		t.Fatalf("Ref() returned %+v", v)
	}
}

func TestStepDefaults(t *testing.T) {
	s := &Step{
		ID:    "rebase-auth",
		Kind:  KindGitRebase,
		Phase: PhaseMutation,
		Inputs: map[string]Value{
			"branch":   Lit("feat/auth"),
			"onto":     Ref("ff-main.tip"),
			"old_base": Lit("aaa111"),
		},
	}
	if s.Status != "" {
		t.Fatalf("expected empty status, got %q", s.Status)
	}
	if s.Outputs != nil {
		t.Fatalf("expected nil outputs, got %v", s.Outputs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -count=1 -run 'TestLit|TestRef|TestStepDefaults' ./internal/ops/`
Expected: FAIL — package does not exist

- [ ] **Step 3: Implement Step and Value types**

```go
// internal/ops/step.go
package ops

// Step kind constants.
const (
	KindGitFetchAll     = "git-fetch-all"
	KindGitFastForward  = "git-fast-forward"
	KindGitRevParse     = "git-rev-parse"
	KindGitIsAncestor   = "git-is-ancestor"
	KindGitCreateBranch = "git-create-branch"
	KindGitCheckout     = "git-checkout"
	KindGitRebase       = "git-rebase"
	KindGitCherryPick   = "git-cherry-pick"
	KindGitPush         = "git-push"
	KindGitPushNew      = "git-push-new"
	KindGitResetHard    = "git-reset-hard"

	KindGHPRList     = "gh-pr-list"
	KindGHPRCreate   = "gh-pr-create"
	KindGHPREditBase = "gh-pr-edit-base"
	KindGHPRMerge    = "gh-pr-merge"
	KindGHPRView     = "gh-pr-view"

	KindReconcilePRs  = "reconcile-prs"
	KindReorderNodes  = "reorder-nodes"
	KindUpdateStackNav = "update-stack-nav"
	KindRenderStatus  = "render-status"
)

// Step phase constants.
const (
	PhasePreMutation = "pre-mutation"
	PhaseMutation    = "mutation"
	PhaseCommit      = "commit"
	PhasePostCommit  = "post-commit"
)

// Step status constants.
const (
	StatusPending    = "pending"
	StatusInProgress = "in-progress"
	StatusDone       = "done"
	StatusConflict   = "conflict"
	StatusSkipped    = "skipped"
	StatusFailed     = "failed"
)

// Step represents a single operation in an sdf command pipeline.
type Step struct {
	ID      string            `json:"id"`
	Kind    string            `json:"kind"`
	Phase   string            `json:"phase"`
	Inputs  map[string]Value  `json:"inputs"`
	Outputs map[string]string `json:"outputs,omitempty"`
	Status  string            `json:"status"`
	Error   string            `json:"error,omitempty"`
}

// Value is either a literal string or a reference to an upstream step's output.
type Value struct {
	Literal string `json:"literal,omitempty"`
	Ref     string `json:"ref,omitempty"`
}

// Lit creates a Value with a literal string.
func Lit(s string) Value {
	return Value{Literal: s}
}

// Ref creates a Value referencing an upstream step output.
// Format: "step-id.output-name"
func Ref(ref string) Value {
	return Value{Ref: ref}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -count=1 -run 'TestLit|TestRef|TestStepDefaults' ./internal/ops/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ops/step.go internal/ops/step_test.go
git commit -m "feat(ops): add Step and Value types with kind/phase/status constants"
```

---

### Task 2: Operation type and persistence

**Files:**
- Create: `internal/ops/operation.go`
- Test: `internal/ops/operation_test.go`
- Modify: `internal/stack/stack.go` (add Operation field to LocalState)

- [ ] **Step 1: Write failing tests for Operation load/save/clear**

```go
// internal/ops/operation_test.go
package ops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOperationSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	sdfDir := filepath.Join(dir, ".sdf")
	os.MkdirAll(sdfDir, 0755)

	op := &Operation{
		Command:        "sync",
		StackID:        "my-feature",
		OriginalBranch: "feat/auth",
		Snapshot:       map[string]string{"feat/auth": "abc123", "feat/api": "def456"},
		Steps: []*Step{
			{ID: "rebase-auth", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
				Inputs: map[string]Value{"branch": Lit("feat/auth"), "onto": Lit("main")}},
		},
	}

	if err := Save(dir, op); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}
	if loaded.Command != "sync" {
		t.Errorf("Command = %q, want %q", loaded.Command, "sync")
	}
	if loaded.Snapshot["feat/auth"] != "abc123" {
		t.Errorf("Snapshot[feat/auth] = %q, want %q", loaded.Snapshot["feat/auth"], "abc123")
	}
	if len(loaded.Steps) != 1 || loaded.Steps[0].ID != "rebase-auth" {
		t.Errorf("Steps not preserved: %+v", loaded.Steps)
	}
}

func TestOperationLoadReturnsNilWhenNoOperation(t *testing.T) {
	dir := t.TempDir()
	sdfDir := filepath.Join(dir, ".sdf")
	os.MkdirAll(sdfDir, 0755)
	// Write empty local.json
	os.WriteFile(filepath.Join(sdfDir, "local.json"), []byte("{}\n"), 0644)

	op, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if op != nil {
		t.Fatalf("expected nil operation, got %+v", op)
	}
}

func TestOperationClear(t *testing.T) {
	dir := t.TempDir()
	sdfDir := filepath.Join(dir, ".sdf")
	os.MkdirAll(sdfDir, 0755)

	op := &Operation{Command: "restack", Steps: []*Step{}}
	Save(dir, op)

	if err := Clear(dir); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	loaded, _ := Load(dir)
	if loaded != nil {
		t.Fatalf("expected nil after Clear, got %+v", loaded)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -count=1 -run 'TestOperation' ./internal/ops/`
Expected: FAIL — Operation type not defined

- [ ] **Step 3: Implement Operation type and persistence**

```go
// internal/ops/operation.go
package ops

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const sdfDir = ".sdf"
const localFile = "local.json"

// Operation represents an in-progress sdf command with its full step pipeline.
type Operation struct {
	Command        string            `json:"command"`
	StackID        string            `json:"stack_id"`
	StartedAt      time.Time         `json:"started_at"`
	OriginalBranch string            `json:"original_branch"`
	Snapshot       map[string]string `json:"snapshot,omitempty"`
	Steps          []*Step           `json:"steps"`
	CommandData    json.RawMessage   `json:"command_data,omitempty"`
}

// localState mirrors the shape of .sdf/local.json.
// We only touch the "operation" key; other fields pass through untouched.
type localState struct {
	Operation       *Operation      `json:"operation,omitempty"`
	SyncProgress    json.RawMessage `json:"sync_progress,omitempty"`
	SplitSessions   json.RawMessage `json:"split_sessions,omitempty"`
	RestackProgress json.RawMessage `json:"restack_progress,omitempty"`
}

// Load reads the current operation from .sdf/local.json.
// Returns nil, nil if no operation is in progress.
func Load(root string) (*Operation, error) {
	path := filepath.Join(root, sdfDir, localFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	var ls localState
	if err := json.Unmarshal(data, &ls); err != nil {
		return nil, nil // corrupted — treat as no operation
	}
	return ls.Operation, nil
}

// Save writes the operation to .sdf/local.json, preserving other fields.
func Save(root string, op *Operation) error {
	path := filepath.Join(root, sdfDir, localFile)

	// Read existing to preserve other fields
	var ls localState
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &ls) // ignore error — we'll overwrite
	}

	ls.Operation = op
	data, err := json.MarshalIndent(ls, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal local state: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

// Clear removes the operation from .sdf/local.json, preserving other fields.
func Clear(root string) error {
	return Save(root, nil)
}

// FindStep returns the step with the given ID, or nil.
func (op *Operation) FindStep(id string) *Step {
	for _, s := range op.Steps {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// CurrentStep returns the first step that is not done or skipped.
func (op *Operation) CurrentStep() *Step {
	for _, s := range op.Steps {
		if s.Status != StatusDone && s.Status != StatusSkipped {
			return s
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -count=1 -run 'TestOperation' ./internal/ops/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ops/operation.go internal/ops/operation_test.go
git commit -m "feat(ops): add Operation type with save/load/clear persistence"
```

---

### Task 3: Validation

**Files:**
- Create: `internal/ops/validate.go`
- Test: `internal/ops/validate_test.go`

- [ ] **Step 1: Write failing tests for validation rules**

```go
// internal/ops/validate_test.go
package ops

import (
	"strings"
	"testing"
)

func TestValidate_ValidPipeline(t *testing.T) {
	steps := []*Step{
		{ID: "ff", Kind: KindGitFastForward, Phase: PhasePreMutation, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("main")}},
		{ID: "rebase-a", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("feat/a"), "onto": Ref("ff.tip"), "old_base": Lit("aaa")}},
		{ID: "push-a", Kind: KindGitPush, Phase: PhaseCommit, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("feat/a")}},
	}
	op := &Operation{Steps: steps, Snapshot: map[string]string{"feat/a": "abc123"}}
	if err := Validate(op); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestValidate_RefToNonexistentStep(t *testing.T) {
	steps := []*Step{
		{ID: "rebase-a", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
			Inputs: map[string]Value{"onto": Ref("nonexistent.tip")}},
	}
	op := &Operation{Steps: steps}
	err := Validate(op)
	if err == nil || !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("expected ref error, got: %v", err)
	}
}

func TestValidate_RefToFutureStep(t *testing.T) {
	steps := []*Step{
		{ID: "rebase-a", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
			Inputs: map[string]Value{"onto": Ref("rebase-b.new_sha")}},
		{ID: "rebase-b", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
			Inputs: map[string]Value{"onto": Lit("main")}},
	}
	op := &Operation{Steps: steps}
	err := Validate(op)
	if err == nil || !strings.Contains(err.Error(), "after") {
		t.Fatalf("expected ordering error, got: %v", err)
	}
}

func TestValidate_MutationAfterCommit(t *testing.T) {
	steps := []*Step{
		{ID: "push-a", Kind: KindGitPush, Phase: PhaseCommit, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("feat/a")}},
		{ID: "rebase-b", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("feat/b")}},
	}
	op := &Operation{Steps: steps}
	err := Validate(op)
	if err == nil || !strings.Contains(err.Error(), "mutation") {
		t.Fatalf("expected phase error, got: %v", err)
	}
}

func TestValidate_DuplicateStepIDs(t *testing.T) {
	steps := []*Step{
		{ID: "push-a", Kind: KindGitPush, Phase: PhaseCommit, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("feat/a")}},
		{ID: "push-a", Kind: KindGitPush, Phase: PhaseCommit, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("feat/b")}},
	}
	op := &Operation{Steps: steps}
	err := Validate(op)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got: %v", err)
	}
}

func TestValidate_MissingSnapshotForMutationStep(t *testing.T) {
	steps := []*Step{
		{ID: "rebase-a", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("feat/a"), "onto": Lit("main"), "old_base": Lit("aaa")}},
	}
	op := &Operation{Steps: steps, Snapshot: map[string]string{}} // missing feat/a
	err := Validate(op)
	if err == nil || !strings.Contains(err.Error(), "snapshot") {
		t.Fatalf("expected snapshot error, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -count=1 -run 'TestValidate' ./internal/ops/`
Expected: FAIL — Validate not defined

- [ ] **Step 3: Implement validation**

```go
// internal/ops/validate.go
package ops

import (
	"fmt"
	"strings"
)

// Validate checks the operation's step graph for logic errors before execution.
// Returns nil if valid, or a descriptive error.
func Validate(op *Operation) error {
	// Check for duplicate IDs
	seen := make(map[string]int) // id → index
	for i, s := range op.Steps {
		if _, exists := seen[s.ID]; exists {
			return fmt.Errorf("duplicate step ID %q", s.ID)
		}
		seen[s.ID] = i
	}

	// Check phase ordering: no mutation after commit, no pre-mutation after mutation
	phaseOrder := map[string]int{
		PhasePreMutation: 0,
		PhaseMutation:    1,
		PhaseCommit:      2,
		PhasePostCommit:  3,
	}
	maxPhase := -1
	for _, s := range op.Steps {
		p, ok := phaseOrder[s.Phase]
		if !ok {
			return fmt.Errorf("step %q has unknown phase %q", s.ID, s.Phase)
		}
		if p < maxPhase {
			return fmt.Errorf("step %q is a %s step but follows a later phase — mutation steps cannot come after commit steps", s.ID, s.Phase)
		}
		if p > maxPhase {
			maxPhase = p
		}
	}

	// Check ref validity and ordering
	for i, s := range op.Steps {
		for inputName, val := range s.Inputs {
			if val.Ref == "" {
				continue
			}
			parts := strings.SplitN(val.Ref, ".", 2)
			if len(parts) != 2 {
				return fmt.Errorf("step %q input %q has malformed ref %q (expected step-id.output-name)", s.ID, inputName, val.Ref)
			}
			refStepID := parts[0]
			refIdx, exists := seen[refStepID]
			if !exists {
				return fmt.Errorf("step %q input %q references nonexistent step %q", s.ID, inputName, refStepID)
			}
			if refIdx >= i {
				return fmt.Errorf("step %q input %q references step %q which comes after it — refs can only point backward", s.ID, inputName, refStepID)
			}
		}
	}

	// Check snapshot completeness for mutation steps
	for _, s := range op.Steps {
		if s.Phase != PhaseMutation {
			continue
		}
		branch, ok := s.Inputs["branch"]
		if !ok {
			continue // not all mutation steps have a branch input
		}
		branchName := branch.Literal
		if branchName == "" {
			continue // ref-based branch — resolved at runtime
		}
		if op.Snapshot == nil || op.Snapshot[branchName] == "" {
			return fmt.Errorf("step %q mutates branch %q but no snapshot exists for it — add it to Operation.Snapshot", s.ID, branchName)
		}
	}

	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -count=1 -run 'TestValidate' ./internal/ops/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ops/validate.go internal/ops/validate_test.go
git commit -m "feat(ops): add step graph validation — refs, phases, snapshots"
```

---

### Task 4: Executor core — ref resolution and run loop

**Files:**
- Create: `internal/ops/executor.go`
- Test: `internal/ops/executor_test.go`

- [ ] **Step 1: Write failing tests for ref resolution and basic execution**

```go
// internal/ops/executor_test.go
package ops

import (
	"fmt"
	"testing"
)

// mockHandler tracks calls and returns configured outputs.
type mockHandler struct {
	calls   []mockCall
	results map[string]mockResult
}

type mockCall struct {
	StepID string
	Kind   string
	Inputs map[string]string
}

type mockResult struct {
	outputs map[string]string
	err     error
}

func (m *mockHandler) handle(stepID, kind string, inputs map[string]string) (map[string]string, error) {
	m.calls = append(m.calls, mockCall{StepID: stepID, Kind: kind, Inputs: inputs})
	if r, ok := m.results[stepID]; ok {
		return r.outputs, r.err
	}
	return nil, nil
}

func TestExecutor_RunsStepsInOrder(t *testing.T) {
	mock := &mockHandler{results: map[string]mockResult{
		"step-a": {outputs: map[string]string{"sha": "aaa"}},
		"step-b": {outputs: map[string]string{"sha": "bbb"}},
	}}

	steps := []*Step{
		{ID: "step-a", Kind: KindGitRevParse, Phase: PhasePreMutation, Status: StatusPending,
			Inputs: map[string]Value{"ref": Lit("main")}},
		{ID: "step-b", Kind: KindGitRevParse, Phase: PhasePreMutation, Status: StatusPending,
			Inputs: map[string]Value{"ref": Lit("feat/a")}},
	}
	op := &Operation{Steps: steps}
	exec := NewExecutor(op, WithHandler(mock.handle))

	if err := exec.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(mock.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(mock.calls))
	}
	if mock.calls[0].StepID != "step-a" || mock.calls[1].StepID != "step-b" {
		t.Errorf("wrong order: %v", mock.calls)
	}
	if steps[0].Status != StatusDone || steps[1].Status != StatusDone {
		t.Errorf("steps not marked done: %s, %s", steps[0].Status, steps[1].Status)
	}
}

func TestExecutor_ResolvesRefs(t *testing.T) {
	mock := &mockHandler{results: map[string]mockResult{
		"ff": {outputs: map[string]string{"tip": "abc123"}},
		"rebase": {outputs: map[string]string{"new_sha": "def456"}},
	}}

	steps := []*Step{
		{ID: "ff", Kind: KindGitFastForward, Phase: PhasePreMutation, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("main")}},
		{ID: "rebase", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
			Inputs: map[string]Value{
				"branch":   Lit("feat/a"),
				"onto":     Ref("ff.tip"),
				"old_base": Lit("aaa"),
			}},
	}
	op := &Operation{Steps: steps, Snapshot: map[string]string{"feat/a": "old"}}
	exec := NewExecutor(op, WithHandler(mock.handle))

	if err := exec.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Check that the rebase step received the resolved ref
	rebaseCall := mock.calls[1]
	if rebaseCall.Inputs["onto"] != "abc123" {
		t.Errorf("onto = %q, want %q", rebaseCall.Inputs["onto"], "abc123")
	}
}

func TestExecutor_SkipsDoneSteps(t *testing.T) {
	mock := &mockHandler{results: map[string]mockResult{
		"step-b": {outputs: map[string]string{}},
	}}

	steps := []*Step{
		{ID: "step-a", Kind: KindGitRevParse, Phase: PhasePreMutation, Status: StatusDone,
			Inputs: map[string]Value{"ref": Lit("main")},
			Outputs: map[string]string{"sha": "aaa"}},
		{ID: "step-b", Kind: KindGitRevParse, Phase: PhasePreMutation, Status: StatusPending,
			Inputs: map[string]Value{"ref": Lit("feat/a")}},
	}
	op := &Operation{Steps: steps}
	exec := NewExecutor(op, WithHandler(mock.handle))

	if err := exec.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(mock.calls) != 1 {
		t.Fatalf("expected 1 call (skipped done step), got %d", len(mock.calls))
	}
	if mock.calls[0].StepID != "step-b" {
		t.Errorf("expected step-b, got %s", mock.calls[0].StepID)
	}
}

func TestExecutor_StopsOnError(t *testing.T) {
	mock := &mockHandler{results: map[string]mockResult{
		"step-a": {err: fmt.Errorf("rebase conflict")},
	}}

	steps := []*Step{
		{ID: "step-a", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("feat/a")}},
		{ID: "step-b", Kind: KindGitPush, Phase: PhaseCommit, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("feat/a")}},
	}
	op := &Operation{Steps: steps, Snapshot: map[string]string{"feat/a": "old"}}
	exec := NewExecutor(op, WithHandler(mock.handle))

	err := exec.Run()
	if err == nil {
		t.Fatal("expected error")
	}
	if steps[0].Status != StatusFailed {
		t.Errorf("step-a status = %q, want %q", steps[0].Status, StatusFailed)
	}
	if steps[1].Status != StatusPending {
		t.Errorf("step-b status = %q, want %q (should not have run)", steps[1].Status, StatusPending)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.calls))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -count=1 -run 'TestExecutor' ./internal/ops/`
Expected: FAIL — NewExecutor not defined

- [ ] **Step 3: Implement the executor**

```go
// internal/ops/executor.go
package ops

import (
	"fmt"
	"strings"
)

// HandlerFunc executes a step and returns its outputs.
// The executor calls this for each step, passing the step ID, kind, and resolved inputs.
type HandlerFunc func(stepID, kind string, inputs map[string]string) (map[string]string, error)

// Option configures an Executor.
type Option func(*Executor)

// WithHandler sets a custom handler (used for testing).
func WithHandler(h HandlerFunc) Option {
	return func(e *Executor) { e.handler = h }
}

// WithPersistence enables saving progress to disk after each step.
func WithPersistence(root string) Option {
	return func(e *Executor) { e.root = root; e.persist = true }
}

// Executor runs an Operation's steps in order, resolving refs and tracking status.
type Executor struct {
	op      *Operation
	handler HandlerFunc
	root    string
	persist bool
}

// NewExecutor creates an Executor for the given operation.
func NewExecutor(op *Operation, opts ...Option) *Executor {
	e := &Executor{op: op}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Run validates the step graph, then executes steps in order.
func (e *Executor) Run() error {
	if err := Validate(e.op); err != nil {
		return fmt.Errorf("invalid operation plan: %w", err)
	}

	for _, step := range e.op.Steps {
		if step.Status == StatusDone || step.Status == StatusSkipped {
			continue
		}

		inputs, err := e.resolveInputs(step)
		if err != nil {
			return err
		}

		step.Status = StatusInProgress
		e.save()

		outputs, err := e.handler(step.ID, step.Kind, inputs)
		if err != nil {
			step.Status = StatusFailed
			step.Error = err.Error()
			e.save()
			return fmt.Errorf("step %s (%s) failed: %w", step.ID, step.Kind, err)
		}

		step.Outputs = outputs
		step.Status = StatusDone
		step.Error = ""
		e.save()
	}

	return nil
}

// resolveInputs converts Value refs to concrete strings using upstream step outputs.
func (e *Executor) resolveInputs(step *Step) (map[string]string, error) {
	resolved := make(map[string]string, len(step.Inputs))
	for name, val := range step.Inputs {
		if val.Literal != "" {
			resolved[name] = val.Literal
			continue
		}
		if val.Ref != "" {
			parts := strings.SplitN(val.Ref, ".", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("step %s: malformed ref %q", step.ID, val.Ref)
			}
			upstream := e.op.FindStep(parts[0])
			if upstream == nil {
				return nil, fmt.Errorf("step %s: references unknown step %q", step.ID, parts[0])
			}
			if upstream.Status != StatusDone {
				return nil, fmt.Errorf("step %s: depends on %q which has status %q", step.ID, parts[0], upstream.Status)
			}
			output, ok := upstream.Outputs[parts[1]]
			if !ok {
				return nil, fmt.Errorf("step %s: references output %s.%s which was not produced", step.ID, parts[0], parts[1])
			}
			resolved[name] = output
			continue
		}
		// Empty value — skip
	}
	return resolved, nil
}

// save persists the operation to disk if persistence is enabled.
func (e *Executor) save() {
	if e.persist && e.root != "" {
		Save(e.root, e.op)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -count=1 -run 'TestExecutor' ./internal/ops/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ops/executor.go internal/ops/executor_test.go
git commit -m "feat(ops): add Executor with ref resolution, run loop, and mock handler support"
```

---

### Task 5: Executor persistence (crash safety)

**Files:**
- Modify: `internal/ops/executor.go`
- Test: `internal/ops/executor_test.go` (append)

- [ ] **Step 1: Write failing test for persistence across steps**

Append to `internal/ops/executor_test.go`:

```go
func TestExecutor_PersistsProgressToDisk(t *testing.T) {
	dir := t.TempDir()
	sdfDir := filepath.Join(dir, ".sdf")
	os.MkdirAll(sdfDir, 0755)

	mock := &mockHandler{results: map[string]mockResult{
		"step-a": {outputs: map[string]string{"sha": "aaa"}},
		"step-b": {err: fmt.Errorf("conflict")},
	}}

	steps := []*Step{
		{ID: "step-a", Kind: KindGitRevParse, Phase: PhasePreMutation, Status: StatusPending,
			Inputs: map[string]Value{"ref": Lit("main")}},
		{ID: "step-b", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("feat/a")}},
	}
	op := &Operation{Steps: steps, Snapshot: map[string]string{"feat/a": "old"}}
	Save(dir, op)

	exec := NewExecutor(op, WithHandler(mock.handle), WithPersistence(dir))
	exec.Run() // will fail on step-b

	// Load from disk and verify step-a is done, step-b is failed
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Steps[0].Status != StatusDone {
		t.Errorf("step-a status on disk = %q, want %q", loaded.Steps[0].Status, StatusDone)
	}
	if loaded.Steps[0].Outputs["sha"] != "aaa" {
		t.Errorf("step-a output on disk = %q, want %q", loaded.Steps[0].Outputs["sha"], "aaa")
	}
	if loaded.Steps[1].Status != StatusFailed {
		t.Errorf("step-b status on disk = %q, want %q", loaded.Steps[1].Status, StatusFailed)
	}
}
```

- [ ] **Step 2: Add imports to test file**

Add `"os"` and `"path/filepath"` to the imports in `executor_test.go` if not already present.

- [ ] **Step 3: Run test to verify it passes** (persistence is already implemented in Task 4's executor)

Run: `go test -count=1 -run 'TestExecutor_PersistsProgressToDisk' ./internal/ops/`
Expected: PASS — the `save()` method and `WithPersistence` option already handle this.

If it fails, the issue is likely the import or a minor wiring bug. Fix and re-run.

- [ ] **Step 4: Commit**

```bash
git add internal/ops/executor_test.go
git commit -m "test(ops): add persistence crash-safety test for executor"
```

---

### Task 6: Recovery — Continue, Abort, Quit

**Files:**
- Create: `internal/ops/recover.go`
- Test: `internal/ops/recover_test.go`

- [ ] **Step 1: Write failing tests for Abort and Quit**

```go
// internal/ops/recover_test.go
package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAbort_RestoresSnapshotBranches(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".sdf"), 0755)

	var resetCalls []string
	handler := func(stepID, kind string, inputs map[string]string) (map[string]string, error) {
		if kind == KindGitResetHard {
			resetCalls = append(resetCalls, inputs["branch"]+"→"+inputs["sha"])
		}
		return nil, nil
	}

	op := &Operation{
		Command:        "restack",
		OriginalBranch: "feat/ui",
		Snapshot:       map[string]string{"feat/a": "aaa", "feat/b": "bbb"},
		Steps: []*Step{
			{ID: "rebase-a", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusDone,
				Inputs: map[string]Value{"branch": Lit("feat/a")}},
			{ID: "rebase-b", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusConflict,
				Inputs: map[string]Value{"branch": Lit("feat/b")}},
			{ID: "push-a", Kind: KindGitPush, Phase: PhaseCommit, Status: StatusPending,
				Inputs: map[string]Value{"branch": Lit("feat/a")}},
		},
	}
	Save(dir, op)

	err := Abort(dir, handler)
	if err != nil {
		t.Fatalf("Abort: %v", err)
	}

	// Should have reset both snapshot branches
	if len(resetCalls) != 2 {
		t.Fatalf("expected 2 reset calls, got %d: %v", len(resetCalls), resetCalls)
	}

	// Operation should be cleared
	loaded, _ := Load(dir)
	if loaded != nil {
		t.Fatalf("expected operation cleared, got %+v", loaded)
	}
}

func TestAbort_FailsAfterPushCompleted(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".sdf"), 0755)

	op := &Operation{
		Command:  "sync",
		Snapshot: map[string]string{"feat/a": "aaa"},
		Steps: []*Step{
			{ID: "rebase-a", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusDone,
				Inputs: map[string]Value{"branch": Lit("feat/a")}},
			{ID: "push-a", Kind: KindGitPush, Phase: PhaseCommit, Status: StatusDone,
				Inputs: map[string]Value{"branch": Lit("feat/a")}},
		},
	}
	Save(dir, op)

	err := Abort(dir, nil)
	if err == nil || !strings.Contains(err.Error(), "push") {
		t.Fatalf("expected push-already-done error, got: %v", err)
	}
}

func TestQuit_ClearsProgressOnly(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".sdf"), 0755)

	op := &Operation{
		Command: "sync",
		Steps: []*Step{
			{ID: "rebase-a", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusDone,
				Inputs: map[string]Value{"branch": Lit("feat/a")}},
		},
	}
	Save(dir, op)

	err := Quit(dir)
	if err != nil {
		t.Fatalf("Quit: %v", err)
	}

	loaded, _ := Load(dir)
	if loaded != nil {
		t.Fatalf("expected operation cleared, got %+v", loaded)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -count=1 -run 'TestAbort|TestQuit' ./internal/ops/`
Expected: FAIL — Abort, Quit not defined

- [ ] **Step 3: Implement recovery functions**

```go
// internal/ops/recover.go
package ops

import "fmt"

// Abort cancels an in-progress operation, restoring all branches to their
// pre-mutation snapshot SHAs. Fails if any commit-phase step has completed.
func Abort(root string, handler HandlerFunc) error {
	op, err := Load(root)
	if err != nil {
		return fmt.Errorf("cannot load operation: %w", err)
	}
	if op == nil {
		return fmt.Errorf("no operation in progress")
	}

	// Check if any commit-phase step has completed — abort is not possible
	for _, s := range op.Steps {
		if s.Phase == PhaseCommit && s.Status == StatusDone {
			return fmt.Errorf("cannot abort — push already completed for step %q. Use --quit to drop state without rolling back", s.ID)
		}
	}

	// Reset each snapshotted branch to its original SHA
	if handler != nil {
		for branch, sha := range op.Snapshot {
			// Checkout the branch, then reset it
			handler("abort-checkout-"+branch, KindGitCheckout, map[string]string{"branch": branch})
			_, err := handler("abort-reset-"+branch, KindGitResetHard, map[string]string{"branch": branch, "sha": sha})
			if err != nil {
				return fmt.Errorf("cannot reset %s to %s: %w", branch, sha[:10], err)
			}
		}

		// Restore original branch
		if op.OriginalBranch != "" {
			handler("abort-restore", KindGitCheckout, map[string]string{"branch": op.OriginalBranch})
		}
	}

	return Clear(root)
}

// Quit drops the operation state without restoring branches.
// Useful when the user has resolved things manually and wants sdf to stop tracking.
func Quit(root string) error {
	op, err := Load(root)
	if err != nil {
		return fmt.Errorf("cannot load operation: %w", err)
	}
	if op == nil {
		return fmt.Errorf("no operation in progress")
	}
	return Clear(root)
}

// Continue loads an in-progress operation and returns it for the executor to resume.
// The caller should create a new Executor with the returned operation and call Run().
// The executor's Run() loop already skips done/skipped steps.
func Continue(root string) (*Operation, error) {
	op, err := Load(root)
	if err != nil {
		return nil, fmt.Errorf("cannot load operation: %w", err)
	}
	if op == nil {
		return nil, fmt.Errorf("no operation in progress")
	}

	// Find the current step
	current := op.CurrentStep()
	if current == nil {
		// All steps done — just clear
		Clear(root)
		return nil, fmt.Errorf("operation already complete — nothing to continue")
	}

	// If the current step was in-progress or failed, reset to pending so the executor retries it
	if current.Status == StatusInProgress || current.Status == StatusFailed {
		current.Status = StatusPending
		current.Error = ""
	}

	return op, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -count=1 -run 'TestAbort|TestQuit' ./internal/ops/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ops/recover.go internal/ops/recover_test.go
git commit -m "feat(ops): add Abort, Quit, Continue recovery functions"
```

---

### Task 7: Plan display (verbose/default/dry-run)

**Files:**
- Create: `internal/ops/display.go`
- Test: `internal/ops/display_test.go`

- [ ] **Step 1: Write failing tests for plan formatting**

```go
// internal/ops/display_test.go
package ops

import (
	"strings"
	"testing"
)

func TestFormatPlan_DefaultMode(t *testing.T) {
	steps := []*Step{
		{ID: "rebase-a", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("feat/auth"), "onto": Lit("main"), "old_base": Lit("aaa")}},
		{ID: "rebase-b", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("feat/api"), "onto": Ref("rebase-a.new_sha"), "old_base": Lit("bbb")}},
		{ID: "push-a", Kind: KindGitPush, Phase: PhaseCommit, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("feat/auth")}},
		{ID: "push-b", Kind: KindGitPush, Phase: PhaseCommit, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("feat/api")}},
	}
	op := &Operation{Steps: steps}

	output := FormatPlan(op, false)

	if !strings.Contains(output, "rebase feat/auth onto main") {
		t.Errorf("missing rebase-a summary in:\n%s", output)
	}
	if !strings.Contains(output, "push 2 branch") {
		t.Errorf("missing push summary in:\n%s", output)
	}
	// Default mode should NOT contain raw git commands
	if strings.Contains(output, "git rebase --onto") {
		t.Errorf("default mode should not show raw commands:\n%s", output)
	}
}

func TestFormatPlan_VerboseMode(t *testing.T) {
	steps := []*Step{
		{ID: "rebase-a", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("feat/auth"), "onto": Lit("abc123"), "old_base": Lit("aaa111")}},
		{ID: "push-a", Kind: KindGitPush, Phase: PhaseCommit, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("feat/auth")}},
	}
	op := &Operation{Steps: steps}

	output := FormatPlan(op, true)

	if !strings.Contains(output, "git rebase --onto abc123 aaa111 feat/auth") {
		t.Errorf("missing rebase command in verbose:\n%s", output)
	}
	if !strings.Contains(output, "git push --force-with-lease origin feat/auth") {
		t.Errorf("missing push command in verbose:\n%s", output)
	}
}

func TestFormatPlan_VerboseShowsRefPlaceholders(t *testing.T) {
	steps := []*Step{
		{ID: "rebase-a", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
			Inputs: map[string]Value{"branch": Lit("feat/api"), "onto": Ref("rebase-auth.new_sha"), "old_base": Lit("bbb")}},
	}
	op := &Operation{Steps: steps}

	output := FormatPlan(op, true)

	if !strings.Contains(output, "<rebase-auth.new_sha>") {
		t.Errorf("expected ref placeholder in verbose output:\n%s", output)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -count=1 -run 'TestFormatPlan' ./internal/ops/`
Expected: FAIL — FormatPlan not defined

- [ ] **Step 3: Implement FormatPlan**

```go
// internal/ops/display.go
package ops

import (
	"fmt"
	"strings"
)

// FormatPlan renders the operation's step list for display.
// If verbose is false, shows a human-friendly summary.
// If verbose is true, shows the exact git/gh commands that will run.
func FormatPlan(op *Operation, verbose bool) string {
	if verbose {
		return formatVerbose(op)
	}
	return formatDefault(op)
}

func formatDefault(op *Operation) string {
	var lines []string
	var pushCount int
	var prUpdateCount int

	for _, s := range op.Steps {
		switch s.Kind {
		case KindGitRebase:
			branch := resolveDisplay(s.Inputs["branch"])
			onto := resolveDisplay(s.Inputs["onto"])
			lines = append(lines, fmt.Sprintf("  rebase %s onto %s", branch, onto))
		case KindGitCherryPick:
			onto := resolveDisplay(s.Inputs["onto"])
			lines = append(lines, fmt.Sprintf("  cherry-pick onto %s", onto))
		case KindGitPush, KindGitPushNew:
			pushCount++
		case KindGHPREditBase:
			prUpdateCount++
		case KindGHPRCreate:
			branch := resolveDisplay(s.Inputs["branch"])
			lines = append(lines, fmt.Sprintf("  create PR for %s", branch))
		case KindGHPRMerge:
			pr := resolveDisplay(s.Inputs["pr"])
			lines = append(lines, fmt.Sprintf("  merge PR %s", pr))
		case KindUpdateStackNav:
			lines = append(lines, "  update stack navigation")
		}
	}

	if pushCount > 0 {
		noun := "branches"
		if pushCount == 1 {
			noun = "branch"
		}
		lines = append(lines, fmt.Sprintf("  push %d %s", pushCount, noun))
	}
	if prUpdateCount > 0 {
		noun := "PR bases"
		if prUpdateCount == 1 {
			noun = "PR base"
		}
		lines = append(lines, fmt.Sprintf("  update %d %s", prUpdateCount, noun))
	}

	return strings.Join(lines, "\n")
}

func formatVerbose(op *Operation) string {
	var sections []string
	currentPhase := ""

	for i, s := range op.Steps {
		phase := phaseLabel(s.Phase)
		if phase != currentPhase {
			if currentPhase != "" {
				sections = append(sections, "")
			}
			sections = append(sections, fmt.Sprintf("  %s:", phase))
			currentPhase = phase
		}

		cmd := stepCommand(s)
		sections = append(sections, fmt.Sprintf("  %3d. %s", i+1, cmd))
	}

	return strings.Join(sections, "\n")
}

func phaseLabel(phase string) string {
	switch phase {
	case PhasePreMutation:
		return "Pre-mutation"
	case PhaseMutation:
		return "Mutation"
	case PhaseCommit:
		return "Push"
	case PhasePostCommit:
		return "Post-push"
	default:
		return phase
	}
}

func stepCommand(s *Step) string {
	resolve := func(key string) string {
		v, ok := s.Inputs[key]
		if !ok {
			return ""
		}
		return resolveDisplay(v)
	}

	switch s.Kind {
	case KindGitFetchAll:
		return "git fetch --all"
	case KindGitFastForward:
		return fmt.Sprintf("git fetch origin %s:%s", resolve("branch"), resolve("branch"))
	case KindGitRevParse:
		return fmt.Sprintf("git rev-parse %s", resolve("ref"))
	case KindGitRebase:
		return fmt.Sprintf("git rebase --onto %s %s %s", resolve("onto"), resolve("old_base"), resolve("branch"))
	case KindGitCherryPick:
		return fmt.Sprintf("git cherry-pick %s", resolve("commits"))
	case KindGitPush:
		return fmt.Sprintf("git push --force-with-lease origin %s", resolve("branch"))
	case KindGitPushNew:
		return fmt.Sprintf("git push -u origin %s", resolve("branch"))
	case KindGitCheckout:
		return fmt.Sprintf("git checkout %s", resolve("branch"))
	case KindGitCreateBranch:
		return fmt.Sprintf("git checkout -b %s", resolve("name"))
	case KindGHPREditBase:
		return fmt.Sprintf("gh pr edit %s --base %s", resolve("pr"), resolve("base"))
	case KindGHPRCreate:
		return fmt.Sprintf("gh pr create --head %s --base %s", resolve("branch"), resolve("base"))
	case KindGHPRMerge:
		return fmt.Sprintf("gh pr merge %s", resolve("pr"))
	case KindUpdateStackNav:
		return "(update stack navigation in PR descriptions)"
	default:
		return fmt.Sprintf("(%s)", s.Kind)
	}
}

// resolveDisplay returns the literal value or a <ref> placeholder for display.
func resolveDisplay(v Value) string {
	if v.Literal != "" {
		return v.Literal
	}
	if v.Ref != "" {
		return "<" + v.Ref + ">"
	}
	return "?"
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -count=1 -run 'TestFormatPlan' ./internal/ops/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ops/display.go internal/ops/display_test.go
git commit -m "feat(ops): add FormatPlan with default/verbose rendering"
```

---

### Task 8: Full integration test — build a pipeline, run it, verify

**Files:**
- Test: `internal/ops/integration_test.go`

- [ ] **Step 1: Write integration test exercising the full lifecycle**

```go
// internal/ops/integration_test.go
package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestIntegration_FullPipeline(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".sdf"), 0755)

	// Simulate: ff-main → rebase-auth (onto ff result) → rebase-api (onto auth result) → push both
	callLog := []string{}
	handler := func(stepID, kind string, inputs map[string]string) (map[string]string, error) {
		callLog = append(callLog, stepID)
		switch stepID {
		case "ff-main":
			return map[string]string{"tip": "main-tip-abc"}, nil
		case "rebase-auth":
			if inputs["onto"] != "main-tip-abc" {
				return nil, fmt.Errorf("rebase-auth: expected onto=main-tip-abc, got %s", inputs["onto"])
			}
			return map[string]string{"new_sha": "auth-rebased-def"}, nil
		case "rebase-api":
			if inputs["onto"] != "auth-rebased-def" {
				return nil, fmt.Errorf("rebase-api: expected onto=auth-rebased-def, got %s", inputs["onto"])
			}
			return map[string]string{"new_sha": "api-rebased-ghi"}, nil
		default:
			return nil, nil
		}
	}

	op := &Operation{
		Command:        "sync",
		StackID:        "my-feature",
		OriginalBranch: "feat/api",
		Snapshot:       map[string]string{"feat/auth": "old-auth", "feat/api": "old-api"},
		Steps: []*Step{
			{ID: "ff-main", Kind: KindGitFastForward, Phase: PhasePreMutation, Status: StatusPending,
				Inputs: map[string]Value{"branch": Lit("main")}},
			{ID: "rebase-auth", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
				Inputs: map[string]Value{"branch": Lit("feat/auth"), "onto": Ref("ff-main.tip"), "old_base": Lit("old-main")}},
			{ID: "rebase-api", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
				Inputs: map[string]Value{"branch": Lit("feat/api"), "onto": Ref("rebase-auth.new_sha"), "old_base": Lit("old-auth")}},
			{ID: "push-auth", Kind: KindGitPush, Phase: PhaseCommit, Status: StatusPending,
				Inputs: map[string]Value{"branch": Lit("feat/auth")}},
			{ID: "push-api", Kind: KindGitPush, Phase: PhaseCommit, Status: StatusPending,
				Inputs: map[string]Value{"branch": Lit("feat/api")}},
			{ID: "nav", Kind: KindUpdateStackNav, Phase: PhasePostCommit, Status: StatusPending,
				Inputs: map[string]Value{"stack": Lit("my-feature")}},
		},
	}

	exec := NewExecutor(op, WithHandler(handler), WithPersistence(dir))
	if err := exec.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// All steps should be done
	for _, s := range op.Steps {
		if s.Status != StatusDone {
			t.Errorf("step %s: status = %q, want done", s.ID, s.Status)
		}
	}

	// Verify execution order
	expected := []string{"ff-main", "rebase-auth", "rebase-api", "push-auth", "push-api", "nav"}
	if len(callLog) != len(expected) {
		t.Fatalf("call count = %d, want %d: %v", len(callLog), len(expected), callLog)
	}
	for i, id := range expected {
		if callLog[i] != id {
			t.Errorf("call[%d] = %q, want %q", i, callLog[i], id)
		}
	}

	// Verify ref resolution worked — rebase-auth got the ff output, rebase-api got the auth output
	if op.Steps[1].Outputs["new_sha"] != "auth-rebased-def" {
		t.Errorf("rebase-auth output not recorded")
	}
	if op.Steps[2].Outputs["new_sha"] != "api-rebased-ghi" {
		t.Errorf("rebase-api output not recorded")
	}

	// Verify persistence
	loaded, _ := Load(dir)
	if loaded == nil {
		t.Fatal("expected persisted operation")
	}
	for _, s := range loaded.Steps {
		if s.Status != StatusDone {
			t.Errorf("persisted step %s: status = %q, want done", s.ID, s.Status)
		}
	}
}

func TestIntegration_ContinueAfterFailure(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".sdf"), 0755)

	callCount := 0
	handler := func(stepID, kind string, inputs map[string]string) (map[string]string, error) {
		callCount++
		switch stepID {
		case "rebase-a":
			return map[string]string{"new_sha": "aaa"}, nil
		case "rebase-b":
			// First time: fail. Second time (after continue): succeed.
			if callCount <= 2 {
				return nil, fmt.Errorf("conflict")
			}
			return map[string]string{"new_sha": "bbb"}, nil
		default:
			return nil, nil
		}
	}

	op := &Operation{
		Command:  "sync",
		Snapshot: map[string]string{"feat/a": "old-a", "feat/b": "old-b"},
		Steps: []*Step{
			{ID: "rebase-a", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
				Inputs: map[string]Value{"branch": Lit("feat/a"), "onto": Lit("main"), "old_base": Lit("x")}},
			{ID: "rebase-b", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
				Inputs: map[string]Value{"branch": Lit("feat/b"), "onto": Ref("rebase-a.new_sha"), "old_base": Lit("y")}},
			{ID: "push-a", Kind: KindGitPush, Phase: PhaseCommit, Status: StatusPending,
				Inputs: map[string]Value{"branch": Lit("feat/a")}},
			{ID: "push-b", Kind: KindGitPush, Phase: PhaseCommit, Status: StatusPending,
				Inputs: map[string]Value{"branch": Lit("feat/b")}},
		},
	}
	Save(dir, op)

	// First run: rebase-a succeeds, rebase-b fails
	exec := NewExecutor(op, WithHandler(handler), WithPersistence(dir))
	err := exec.Run()
	if err == nil {
		t.Fatal("expected error on first run")
	}

	// Continue: loads from disk, rebase-a is already done, rebase-b retries
	resumed, err := Continue(dir)
	if err != nil {
		t.Fatalf("Continue: %v", err)
	}

	exec2 := NewExecutor(resumed, WithHandler(handler), WithPersistence(dir))
	if err := exec2.Run(); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	// All should be done
	for _, s := range resumed.Steps {
		if s.Status != StatusDone {
			t.Errorf("step %s: status = %q after continue", s.ID, s.Status)
		}
	}
}

func TestIntegration_AbortAfterPartialRebase(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".sdf"), 0755)

	var resets []string
	handler := func(stepID, kind string, inputs map[string]string) (map[string]string, error) {
		if kind == KindGitResetHard {
			resets = append(resets, inputs["branch"])
		}
		return nil, nil
	}

	op := &Operation{
		Command:        "sync",
		OriginalBranch: "feat/b",
		Snapshot:       map[string]string{"feat/a": "snap-a", "feat/b": "snap-b"},
		Steps: []*Step{
			{ID: "rebase-a", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusDone,
				Inputs: map[string]Value{"branch": Lit("feat/a")}, Outputs: map[string]string{"new_sha": "new-a"}},
			{ID: "rebase-b", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusConflict,
				Inputs: map[string]Value{"branch": Lit("feat/b")}},
			{ID: "push-a", Kind: KindGitPush, Phase: PhaseCommit, Status: StatusPending,
				Inputs: map[string]Value{"branch": Lit("feat/a")}},
		},
	}
	Save(dir, op)

	if err := Abort(dir, handler); err != nil {
		t.Fatalf("Abort: %v", err)
	}

	// Both snapshot branches should be reset
	if len(resets) != 2 {
		t.Fatalf("expected 2 resets, got %d: %v", len(resets), resets)
	}

	// Operation cleared
	loaded, _ := Load(dir)
	if loaded != nil {
		t.Fatal("expected operation cleared after abort")
	}
}
```

- [ ] **Step 2: Run all integration tests**

Run: `go test -count=1 -run 'TestIntegration' ./internal/ops/`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/ops/integration_test.go
git commit -m "test(ops): add integration tests for full pipeline, continue, and abort"
```

---

### Task 9: Run full test suite, verify nothing is broken

- [ ] **Step 1: Run the ops package tests**

Run: `go test -count=1 -v ./internal/ops/`
Expected: All tests PASS

- [ ] **Step 2: Run the full project test suite**

Run: `go test -count=1 ./...`
Expected: All existing tests still PASS (ops/ is a new package, no existing code touched)

- [ ] **Step 3: Run vet**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 4: Commit (if any fixes were needed)**

If any tests needed fixing, commit those fixes now.

- [ ] **Step 5: Final commit — push the branch**

```bash
git push
```

---

## What this plan produces

After all 9 tasks, the `internal/ops/` package exists with:

| File | Purpose |
|------|---------|
| `step.go` | Step, Value types, kind/phase/status constants, Lit(), Ref() helpers |
| `operation.go` | Operation type, Load/Save/Clear, FindStep, CurrentStep |
| `validate.go` | Pre-execution graph validation (refs, phases, snapshots, duplicates) |
| `executor.go` | Executor with ref resolution, run loop, persistence, mock handler support |
| `recover.go` | Abort (restore snapshots), Quit (drop state), Continue (resume) |
| `display.go` | FormatPlan with default/verbose modes |
| `*_test.go` | Unit + integration tests for all of the above |

No existing code is modified. The package is ready for commands to start using it.

## What comes next (separate plans, separate stack branches)

1. **ops-engine_migrate-restack** — Rewrite restack to build Steps and use the executor
2. **ops-engine_migrate-sync** — Rewrite sync to use the executor (deferred push, --abort, --quit)
3. **ops-engine_migrate-move** — Add progress tracking and recovery to move
4. **ops-engine_migrate-remaining** — merge, split, pr, branch
5. **ops-engine_transparency** — --verbose, --dry-run, --json plan output on all commands
6. **ops-engine_cleanup** — Remove SyncProgress, RestackProgress, migration logic
