package ops

import (
	"fmt"
	"strings"
)

// Validate checks the operation's step graph for logic errors before execution.
func Validate(op *Operation) error {
	seen := make(map[string]int)
	for i, s := range op.Steps {
		if _, exists := seen[s.ID]; exists {
			return fmt.Errorf("duplicate step ID %q", s.ID)
		}
		seen[s.ID] = i
	}

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

	for _, s := range op.Steps {
		if s.Phase != PhaseMutation {
			continue
		}
		branch, ok := s.Inputs["branch"]
		if !ok {
			continue
		}
		branchName := branch.Literal
		if branchName == "" {
			continue
		}
		if op.Snapshot == nil || op.Snapshot[branchName] == "" {
			return fmt.Errorf("step %q mutates branch %q but no snapshot exists for it — add it to Operation.Snapshot", s.ID, branchName)
		}
	}

	return nil
}
