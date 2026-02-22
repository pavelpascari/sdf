// Package testutil provides helpers for creating fake external binaries
// (git, gh, claude) in tests.
package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FakeBin creates a shell script at dir/<name> that logs its arguments
// to dir/<name>.log and writes stdout to the caller. Returns the full
// path to the fake binary.
//
// The script behaviour is controlled by responses: a map from a
// sub-command prefix to the stdout it should produce. The first
// matching prefix wins. If no prefix matches, the script exits 0
// with empty output.
//
// Example:
//
//	FakeBin(t, dir, "gh", map[string]string{
//	    "pr list":  `[{"number":1,"headRefName":"feat"}]`,
//	    "pr edit":  "",
//	    "version":  "gh version 2.50.0",
//	})
func FakeBin(t *testing.T, dir, name string, responses map[string]string) string {
	t.Helper()

	binPath := filepath.Join(dir, name)
	logPath := filepath.Join(dir, name+".log")

	// Build the case branches for the shell script.
	var cases strings.Builder
	for prefix, output := range responses {
		// Escape single quotes in output for safe embedding in shell.
		escaped := strings.ReplaceAll(output, "'", "'\\''")
		cases.WriteString(fmt.Sprintf(
			"  *\"%s\"*)\n    cat <<'FAKEEOF'\n%s\nFAKEEOF\n    ;;\n",
			prefix, escaped,
		))
	}

	script := fmt.Sprintf(`#!/bin/sh
# Fake %s binary for testing — logs calls and returns canned responses.
ARGS="$*"
echo "$ARGS" >> '%s'

case "$ARGS" in
%s  *)
    # Unknown sub-command: succeed silently
    ;;
esac
exit 0
`, name, logPath, cases.String())

	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatalf("fakebin: write %s: %v", binPath, err)
	}
	return binPath
}

// FakeBinFail creates a fake binary that always exits with code 1 and
// writes the given message to stderr. Useful for testing error paths.
func FakeBinFail(t *testing.T, dir, name, stderr string) string {
	t.Helper()

	binPath := filepath.Join(dir, name)
	logPath := filepath.Join(dir, name+".log")

	escaped := strings.ReplaceAll(stderr, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >> '%s'
echo '%s' >&2
exit 1
`, logPath, escaped)

	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatalf("fakebin: write %s: %v", binPath, err)
	}
	return binPath
}

// ReadLog returns the recorded invocations from a fake binary's log file.
// Each line is one invocation with all arguments space-joined.
func ReadLog(t *testing.T, dir, name string) []string {
	t.Helper()

	logPath := filepath.Join(dir, name+".log")
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("fakebin: read log %s: %v", logPath, err)
	}

	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}

// SetBinary sets a package-level Binary variable to point at a fake binary
// and registers a cleanup to restore the original value.
// Usage:
//
//	SetBinary(t, &gh.Binary, fakePath)
func SetBinary(t *testing.T, target *string, fakePath string) {
	t.Helper()
	orig := *target
	*target = fakePath
	t.Cleanup(func() { *target = orig })
}

