package cmd

import (
	"errors"

	"github.com/pavelpascari/sdf/internal/stack"
)

// errorCodeFor returns a stable machine code for an error, or "" for ordinary errors.
func errorCodeFor(err error) string {
	if errors.Is(err, stack.ErrLockTimeout) {
		return "lock_timeout"
	}
	return ""
}

// exitCodeFor maps an error to a process exit code: 75 (EX_TEMPFAIL) for a
// lock-timeout (retryable), 1 otherwise.
func exitCodeFor(err error) int {
	if errorCodeFor(err) == "lock_timeout" {
		return 75
	}
	return 1
}
