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

	for _, s := range op.Steps {
		if s.Phase == PhaseCommit && s.Status == StatusDone {
			return fmt.Errorf("cannot abort — push already completed for step %q. Use --quit to drop state without rolling back", s.ID)
		}
	}

	if handler != nil {
		for branch, sha := range op.Snapshot {
			if _, err := handler("abort-checkout-"+branch, KindGitCheckout, map[string]string{"branch": branch}); err != nil {
				return fmt.Errorf("cannot checkout %s: %w", branch, err)
			}
			_, err := handler("abort-reset-"+branch, KindGitResetHard, map[string]string{"branch": branch, "sha": sha})
			if err != nil {
				return fmt.Errorf("cannot reset %s to %s: %w", branch, sha[:10], err)
			}
		}
		if op.OriginalBranch != "" {
			if _, err := handler("abort-restore", KindGitCheckout, map[string]string{"branch": op.OriginalBranch}); err != nil {
				return fmt.Errorf("cannot restore branch %s: %w", op.OriginalBranch, err)
			}
		}
	}

	return Clear(root)
}

// Quit drops the operation state without restoring branches.
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
// The caller creates a new Executor with the returned operation and calls Run().
func Continue(root string) (*Operation, error) {
	op, err := Load(root)
	if err != nil {
		return nil, fmt.Errorf("cannot load operation: %w", err)
	}
	if op == nil {
		return nil, fmt.Errorf("no operation in progress")
	}

	current := op.CurrentStep()
	if current == nil {
		_ = Clear(root)
		return nil, fmt.Errorf("operation already complete — nothing to continue")
	}

	if current.Status == StatusInProgress || current.Status == StatusFailed {
		current.Status = StatusPending
		current.Error = ""
	}

	return op, nil
}
