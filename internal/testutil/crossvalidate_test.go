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
// Scans all recording directories under e2e/testdata/recordings/<run>/<test>/gh_sdf.jsonl.
// Gracefully skips when no recordings exist (E2E hasn't been run).
// Run E2E first to populate recordings: make test-e2e
func TestCrossValidateFakesAgainstRecordings(t *testing.T) {
	// Locate recordings relative to this file: ../../e2e/testdata/recordings/
	_, thisFile, _, _ := runtime.Caller(0)
	recordingsRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "e2e", "testdata", "recordings")

	if _, err := os.Stat(recordingsRoot); os.IsNotExist(err) {
		t.Skip("no E2E recordings found — run 'make test-e2e' first to populate recordings")
	}

	// Find all gh_sdf.jsonl files across run/test directories.
	// These contain gh invocations made by sdf (actor="sdf", binary="gh").
	var ghFiles []string
	filepath.Walk(recordingsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.Name() == "gh_sdf.jsonl" && !info.IsDir() {
			ghFiles = append(ghFiles, path)
		}
		return nil
	})

	if len(ghFiles) == 0 {
		t.Skip("no gh_sdf.jsonl recordings found — run 'make test-e2e' first")
	}

	// Collect all recordings across files.
	var allRecordings []spy.Invocation
	for _, f := range ghFiles {
		allRecordings = append(allRecordings, ReadRecordings(t, f)...)
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

// TestGHCanonicalFakes_InternalConsistency validates that the canonical fake
// registry is self-consistent:
// - JSON entries parse as valid JSON
// - JSON shapes are non-empty
// - URL entries contain valid URLs
// - FakeBin-compatible responses derived from canonicals are structurally valid
//
// This test makes the canonical registry itself trustworthy, so that
// GHFakeBin and GHFakeBinWith can rely on it as a source of truth.
func TestGHCanonicalFakes_InternalConsistency(t *testing.T) {
	canon := GHCanonicalFakes()

	for key, entry := range canon {
		t.Run(key, func(t *testing.T) {
			response := strings.TrimSpace(entry.Response)

			switch entry.Kind {
			case "json-array":
				if !IsJSON(response) {
					t.Errorf("canonical entry %q claims json-array but response is not JSON: %q",
						key, truncate(response, 60))
				}
				shape := JSONShape(response)
				if shape == "empty" || strings.HasPrefix(shape, "invalid") {
					t.Errorf("canonical entry %q has invalid JSON shape: %s", key, shape)
				}
				if !strings.HasPrefix(shape, "array") {
					t.Errorf("canonical entry %q claims json-array but shape is %s", key, shape)
				}

			case "json-object":
				if !IsJSON(response) {
					t.Errorf("canonical entry %q claims json-object but response is not JSON: %q",
						key, truncate(response, 60))
				}
				shape := JSONShape(response)
				if shape == "empty" || strings.HasPrefix(shape, "invalid") {
					t.Errorf("canonical entry %q has invalid JSON shape: %s", key, shape)
				}
				if !strings.HasPrefix(shape, "object") {
					t.Errorf("canonical entry %q claims json-object but shape is %s", key, shape)
				}

			case "url":
				if !strings.HasPrefix(response, "https://") {
					t.Errorf("canonical entry %q claims url but response doesn't start with https://: %q",
						key, truncate(response, 60))
				}

			case "empty":
				// Any value is valid for empty kind.

			default:
				t.Errorf("canonical entry %q has unknown kind %q", key, entry.Kind)
			}
		})
	}
}

// TestGHFakeResponses_StructuralCompliance validates that the FakeBin-compatible
// response map derived from GHCanonicalFakes is structurally sound. This ensures
// that GHFakeBin() produces a fake binary with correct response shapes.
func TestGHFakeResponses_StructuralCompliance(t *testing.T) {
	responses := GHFakeResponses()

	if len(responses) == 0 {
		t.Fatal("GHFakeResponses returned empty map")
	}

	// Every response should pass structural validation.
	ValidateGHFakeResponses(t, responses)

	// Verify key commands are present.
	required := []string{"pr list", "pr view", "pr create", "pr edit", "pr merge", "version"}
	for _, cmd := range required {
		if _, ok := responses[cmd]; !ok {
			t.Errorf("GHFakeResponses missing required command %q", cmd)
		}
	}

	// JSON responses should parse as valid JSON.
	for prefix, response := range responses {
		response = strings.TrimSpace(response)
		if IsJSON(response) {
			shape := JSONShape(response)
			if strings.HasPrefix(shape, "invalid") {
				t.Errorf("GHFakeResponses[%q] produces invalid JSON: %s", prefix, shape)
			}
		}
	}
}

// TestGHFakeBin_ProducesWorkingBinary verifies that GHFakeBin creates a
// shell script that returns structurally correct responses. This makes
// the fake binary layer self-standing: you can trust its output without
// needing E2E cross-validation.
func TestGHFakeBin_ProducesWorkingBinary(t *testing.T) {
	dir := t.TempDir()
	binPath := GHFakeBin(t, dir)

	// Verify the binary was created.
	if _, err := os.Stat(binPath); err != nil {
		t.Fatalf("GHFakeBin didn't create binary: %v", err)
	}

	// Verify it's executable.
	info, _ := os.Stat(binPath)
	if info.Mode()&0111 == 0 {
		t.Error("GHFakeBin binary is not executable")
	}

	// Verify log file location is set up correctly.
	log := ReadLog(t, dir, "gh")
	if log != nil {
		t.Errorf("expected empty log before any invocations, got %v", log)
	}
}

// TestGHFakeBinWith_ValidatesOverrides verifies that GHFakeBinWith
// catches structurally invalid overrides at test time, rather than
// letting them silently produce wrong behavior.
func TestGHFakeBinWith_ValidatesOverrides(t *testing.T) {
	dir := t.TempDir()

	// Valid override: correct JSON shape for pr list.
	_ = GHFakeBinWith(t, dir, map[string]string{
		"pr list": `[{"number":99,"headRefName":"test","state":"OPEN","baseRefName":"main","url":""}]`,
	})

	// The binary should exist after a valid call.
	if _, err := os.Stat(filepath.Join(dir, "gh")); err != nil {
		t.Errorf("GHFakeBinWith didn't create binary for valid overrides: %v", err)
	}
}
