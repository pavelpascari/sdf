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
