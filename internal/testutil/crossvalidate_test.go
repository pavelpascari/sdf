package testutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCrossValidateFakesAgainstRecordings reads persisted E2E spy recordings
// and compares each recorded gh response against the canonical fake registry.
//
// For JSON responses, it compares structural shapes (key sets).
// For non-JSON responses, it validates the output format (URL, empty, etc.).
//
// This test gracefully skips when no recordings exist (E2E hasn't been run).
// Run E2E first to populate recordings: make test-e2e
func TestCrossValidateFakesAgainstRecordings(t *testing.T) {
	// Locate recordings relative to this file: ../../e2e/testdata/recordings/
	_, thisFile, _, _ := runtime.Caller(0)
	recordingPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "e2e", "testdata", "recordings", "gh.jsonl")

	if _, err := os.Stat(recordingPath); os.IsNotExist(err) {
		t.Skip("no E2E recordings found — run 'make test-e2e' first to populate recordings")
	}

	recordings := ReadRecordings(t, recordingPath)
	if len(recordings) == 0 {
		t.Skip("recording file is empty — run 'make test-e2e' first")
	}

	fakes := GHCanonicalFakes()
	t.Logf("Loaded %d canonical fake entries", len(fakes))
	t.Logf("Reading %d recorded invocations", len(recordings))

	var matched, unmatched int

	for _, inv := range recordings {
		if inv.ExitCode != 0 {
			continue
		}

		key := ClassifyGHArgs(inv.Args)
		fake, exists := fakes[key]
		if !exists {
			unmatched++
			t.Logf("  [info] no canonical fake for %q (args: %s)", key, strings.Join(inv.Args, " "))
			continue
		}
		matched++

		stdout := strings.TrimSpace(inv.Stdout)

		switch fake.Kind {
		case "json-array", "json-object":
			if !IsJSON(stdout) {
				t.Errorf("expected JSON response for %q, got: %q", key, truncate(stdout, 60))
				continue
			}
			realShape := JSONShape(stdout)
			fakeShape := JSONShape(fake.Response)
			if realShape != fakeShape {
				t.Errorf("JSON shape mismatch for %q:\n  real: %s\n  fake: %s", key, realShape, fakeShape)
			} else {
				t.Logf("  [ok] %q shape matches: %s", key, realShape)
			}

		case "url":
			if !strings.HasPrefix(stdout, "https://") {
				t.Errorf("expected URL for %q, got: %q", key, truncate(stdout, 60))
			} else {
				t.Logf("  [ok] %q is a URL: %s", key, truncate(stdout, 60))
			}

		case "empty":
			// "empty" means the command's meaningful output is empty or
			// non-structured. For version commands, we just check it
			// succeeded (exit 0), which we already know at this point.
			t.Logf("  [ok] %q exited 0 (empty/text kind)", key)
		}
	}

	t.Logf("Cross-validation: %d matched, %d unmatched (no canonical fake)", matched, unmatched)

	if matched == 0 {
		t.Error("no recordings matched any canonical fakes — check ClassifyGHArgs or GHCanonicalFakes keys")
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
