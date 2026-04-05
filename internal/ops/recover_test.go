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

	if len(resetCalls) != 2 {
		t.Fatalf("expected 2 reset calls, got %d: %v", len(resetCalls), resetCalls)
	}

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
