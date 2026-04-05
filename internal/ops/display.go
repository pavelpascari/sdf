package ops

import (
	"fmt"
	"strings"
)

// FormatPlan renders the operation's step list for display.
// verbose=false: human-friendly summary. verbose=true: exact git/gh commands.
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

func resolveDisplay(v Value) string {
	if v.Literal != "" {
		return v.Literal
	}
	if v.Ref != "" {
		return "<" + v.Ref + ">"
	}
	return "?"
}
