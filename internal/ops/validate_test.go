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
