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
	if s.ID != "rebase-auth" {
		t.Fatalf("expected ID %q, got %q", "rebase-auth", s.ID)
	}
	if s.Kind != KindGitRebase {
		t.Fatalf("expected Kind %q, got %q", KindGitRebase, s.Kind)
	}
	if s.Phase != PhaseMutation {
		t.Fatalf("expected Phase %q, got %q", PhaseMutation, s.Phase)
	}
	if len(s.Inputs) != 3 {
		t.Fatalf("expected 3 inputs, got %d", len(s.Inputs))
	}
	if s.Status != "" {
		t.Fatalf("expected empty status, got %q", s.Status)
	}
	if s.Outputs != nil {
		t.Fatalf("expected nil outputs, got %v", s.Outputs)
	}
}
