package cmd

import (
	"fmt"
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
)

func TestLockTimeoutMapsToErrorCode(t *testing.T) {
	if code := errorCodeFor(fmt.Errorf("wrap: %w", stack.ErrLockTimeout)); code != "lock_timeout" {
		t.Errorf("got %q, want lock_timeout", code)
	}
	if code := errorCodeFor(fmt.Errorf("other")); code != "" {
		t.Errorf("non-lock error must have empty code, got %q", code)
	}
	if exitCodeFor(fmt.Errorf("x: %w", stack.ErrLockTimeout)) != 75 {
		t.Errorf("lock timeout must exit 75")
	}
}
