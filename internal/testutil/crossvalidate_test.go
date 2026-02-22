package testutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pavelpascari/sdf/internal/spy"
)

// TestCrossValidateFakesAgainstRecordings reads persisted E2E spy recordings
// and compares each recorded gh response against the canonical fake registry.
//
// For JSON responses, it compares structural shapes (key sets).
// For non-JSON responses, it validates the output format (URL, empty, etc.).
//
// Scans all recording directories under e2e/testdata/recordings/<run>/<test>/gh.jsonl.
// Gracefully skips when no recordings exist (E2E hasn't been run).
// Run E2E first to populate recordings: make test-e2e
func TestCrossValidateFakesAgainstRecordings(t *testing.T) {
	// Locate recordings relative to this file: ../../e2e/testdata/recordings/
	_, thisFile, _, _ := runtime.Caller(0)
	recordingsRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "e2e", "testdata", "recordings")

	if _, err := os.Stat(recordingsRoot); os.IsNotExist(err) {
		t.Skip("no E2E recordings found — run 'make test-e2e' first to populate recordings")
	}

	// Find all gh.jsonl files across run/test directories.
	var ghFiles []string
	filepath.Walk(recordingsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.Name() == "gh.jsonl" && !info.IsDir() {
			ghFiles = append(ghFiles, path)
		}
		return nil
	})

	if len(ghFiles) == 0 {
		t.Skip("no gh.jsonl recordings found — run 'make test-e2e' first")
	}

	// Collect all recordings across files.
	var allRecordings []spy.Invocation
	for _, f := range ghFiles {
		recordings := ReadRecordings(t, f)
		allRecordings = append(allRecordings, recordings...)
	}

	if len(allRecordings) == 0 {
		t.Skip("recording files are empty — run 'make test-e2e' first")
	}

	fakes := GHCanonicalFakes()
	t.Logf("Loaded %d canonical fake entries", len(fakes))
	t.Logf("Reading %d recorded invocations from %d files", len(allRecordings), len(ghFiles))

	var matched, unmatched int

	for _, inv := range allRecordings {
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
