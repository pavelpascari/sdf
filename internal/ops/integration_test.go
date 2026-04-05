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

	for _, s := range op.Steps {
		if s.Status != StatusDone {
			t.Errorf("step %s: status = %q, want done", s.ID, s.Status)
		}
	}

	expected := []string{"ff-main", "rebase-auth", "rebase-api", "push-auth", "push-api", "nav"}
	if len(callLog) != len(expected) {
		t.Fatalf("call count = %d, want %d: %v", len(callLog), len(expected), callLog)
	}
	for i, id := range expected {
		if callLog[i] != id {
			t.Errorf("call[%d] = %q, want %q", i, callLog[i], id)
		}
	}

	if op.Steps[1].Outputs["new_sha"] != "auth-rebased-def" {
		t.Errorf("rebase-auth output not recorded")
	}
	if op.Steps[2].Outputs["new_sha"] != "api-rebased-ghi" {
		t.Errorf("rebase-api output not recorded")
	}

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

	exec := NewExecutor(op, WithHandler(handler), WithPersistence(dir))
	err := exec.Run()
	if err == nil {
		t.Fatal("expected error on first run")
	}

	resumed, err := Continue(dir)
	if err != nil {
		t.Fatalf("Continue: %v", err)
	}

	exec2 := NewExecutor(resumed, WithHandler(handler), WithPersistence(dir))
	if err := exec2.Run(); err != nil {
		t.Fatalf("second Run: %v", err)
	}

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

	if len(resets) != 2 {
		t.Fatalf("expected 2 resets, got %d: %v", len(resets), resets)
	}

	loaded, _ := Load(dir)
	if loaded != nil {
		t.Fatal("expected operation cleared after abort")
	}
}
