package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

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
		"ff":     {outputs: map[string]string{"tip": "abc123"}},
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
			Inputs:  map[string]Value{"ref": Lit("main")},
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
		t.Errorf("step-b status = %q, want %q", steps[1].Status, StatusPending)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.calls))
	}
}

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
