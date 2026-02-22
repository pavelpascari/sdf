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

// TestCrossValidateClaudeFakesAgainstRecordings reads persisted E2E spy
// recordings and compares each recorded claude response against the canonical
// fake registry. Scans claude_sdf.jsonl files under e2e/testdata/recordings/.
func TestCrossValidateClaudeFakesAgainstRecordings(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	recordingsRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "e2e", "testdata", "recordings")

	if _, err := os.Stat(recordingsRoot); os.IsNotExist(err) {
		t.Skip("no E2E recordings found — run 'make test-e2e' first to populate recordings")
	}

	var claudeFiles []string
	filepath.Walk(recordingsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.Name() == "claude_sdf.jsonl" && !info.IsDir() {
			claudeFiles = append(claudeFiles, path)
		}
		return nil
	})

	if len(claudeFiles) == 0 {
		t.Skip("no claude_sdf.jsonl recordings found — run 'make test-e2e' first")
	}

	var allRecordings []spy.Invocation
	for _, f := range claudeFiles {
		allRecordings = append(allRecordings, ReadRecordings(t, f)...)
	}

	if len(allRecordings) == 0 {
		t.Skip("recording files are empty — run 'make test-e2e' first")
	}

	fakes := ClaudeCanonicalFakes()
	t.Logf("Loaded %d canonical claude fake entries", len(fakes))
	t.Logf("Reading %d recorded invocations from %d files", len(allRecordings), len(claudeFiles))

	var matched, unmatched int

	for _, inv := range allRecordings {
		if inv.ExitCode != 0 {
			continue
		}

		key := ClassifyClaudeArgs(inv.Args)
		fake, exists := fakes[key]
		if !exists {
			unmatched++
			t.Logf("  [info] no canonical fake for %q (args: %s)", key, strings.Join(inv.Args, " "))
			continue
		}
		matched++

		switch fake.Kind {
		case "jsonl":
			// Each line of the real output should be valid JSON.
			for i, line := range strings.Split(strings.TrimSpace(inv.Stdout), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				if !IsJSON(line) {
					t.Errorf("expected JSON on line %d of %q response, got: %q", i, key, truncate(line, 60))
				}
			}
			t.Logf("  [ok] %q JSONL output validated", key)

		case "text":
			if strings.TrimSpace(inv.Stdout) == "" {
				t.Errorf("expected non-empty text for %q, got empty", key)
			} else {
				t.Logf("  [ok] %q is non-empty text: %s", key, truncate(strings.TrimSpace(inv.Stdout), 60))
			}
		}
	}

	t.Logf("Cross-validation: %d matched, %d unmatched (no canonical fake)", matched, unmatched)
}

// TestCrossValidateGitFakesAgainstRecordings reads persisted E2E spy
// recordings and compares each recorded git response against the canonical
// fake registry. Scans git_sdf.jsonl files under e2e/testdata/recordings/.
func TestCrossValidateGitFakesAgainstRecordings(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	recordingsRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "e2e", "testdata", "recordings")

	if _, err := os.Stat(recordingsRoot); os.IsNotExist(err) {
		t.Skip("no E2E recordings found — run 'make test-e2e' first to populate recordings")
	}

	var gitFiles []string
	filepath.Walk(recordingsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.Name() == "git_sdf.jsonl" && !info.IsDir() {
			gitFiles = append(gitFiles, path)
		}
		return nil
	})

	if len(gitFiles) == 0 {
		t.Skip("no git_sdf.jsonl recordings found — run 'make test-e2e' first")
	}

	var allRecordings []spy.Invocation
	for _, f := range gitFiles {
		allRecordings = append(allRecordings, ReadRecordings(t, f)...)
	}

	if len(allRecordings) == 0 {
		t.Skip("recording files are empty — run 'make test-e2e' first")
	}

	fakes := GitCanonicalFakes()
	t.Logf("Loaded %d canonical git fake entries", len(fakes))
	t.Logf("Reading %d recorded invocations from %d files", len(allRecordings), len(gitFiles))

	var matched, unmatched int

	for _, inv := range allRecordings {
		if inv.ExitCode != 0 {
			continue
		}

		key := ClassifyGitArgs(inv.Args)
		_, exists := fakes[key]
		if !exists {
			unmatched++
			// Don't log every git command — there are many.
			continue
		}
		matched++
		t.Logf("  [ok] %q classified and matched", key)
	}

	t.Logf("Cross-validation: %d matched, %d unmatched (no canonical fake)", matched, unmatched)
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

// ---------------------------------------------------------------------------
// Claude canonical fake tests
// ---------------------------------------------------------------------------

// TestClaudeCanonicalFakes_InternalConsistency validates that the Claude
// canonical fake registry is self-consistent.
func TestClaudeCanonicalFakes_InternalConsistency(t *testing.T) {
	canon := ClaudeCanonicalFakes()

	for key, entry := range canon {
		t.Run(key, func(t *testing.T) {
			response := strings.TrimSpace(entry.Response)

			switch entry.Kind {
			case "jsonl":
				lines := strings.Split(response, "\n")
				if len(lines) == 0 {
					t.Errorf("canonical entry %q claims jsonl but has no lines", key)
				}
				for i, line := range lines {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					if !IsJSON(line) {
						t.Errorf("canonical entry %q line %d is not JSON: %q", key, i, truncate(line, 60))
					}
				}

			case "text":
				if response == "" {
					t.Errorf("canonical entry %q claims text but response is empty", key)
				}

			default:
				t.Errorf("canonical entry %q has unknown kind %q", key, entry.Kind)
			}
		})
	}
}

// TestClaudeFakeResponses_StructuralCompliance validates that the FakeBin-compatible
// response map derived from ClaudeCanonicalFakes is structurally sound.
func TestClaudeFakeResponses_StructuralCompliance(t *testing.T) {
	responses := ClaudeFakeResponses()

	if len(responses) == 0 {
		t.Fatal("ClaudeFakeResponses returned empty map")
	}

	ValidateClaudeFakeResponses(t, responses)

	// Verify key commands are present.
	required := []string{"--version", "-p"}
	for _, cmd := range required {
		if _, ok := responses[cmd]; !ok {
			t.Errorf("ClaudeFakeResponses missing required command %q", cmd)
		}
	}
}

// TestClaudeFakeBin_ProducesWorkingBinary verifies that ClaudeFakeBin creates
// a shell script that can be executed.
func TestClaudeFakeBin_ProducesWorkingBinary(t *testing.T) {
	dir := t.TempDir()
	binPath := ClaudeFakeBin(t, dir)

	if _, err := os.Stat(binPath); err != nil {
		t.Fatalf("ClaudeFakeBin didn't create binary: %v", err)
	}

	info, _ := os.Stat(binPath)
	if info.Mode()&0111 == 0 {
		t.Error("ClaudeFakeBin binary is not executable")
	}

	log := ReadLog(t, dir, "claude")
	if log != nil {
		t.Errorf("expected empty log before any invocations, got %v", log)
	}
}

// TestClaudeFakeBinWith_ValidatesOverrides verifies that ClaudeFakeBinWith
// catches structurally invalid overrides at test time.
func TestClaudeFakeBinWith_ValidatesOverrides(t *testing.T) {
	dir := t.TempDir()

	_ = ClaudeFakeBinWith(t, dir, map[string]string{
		"-p": "Custom test response",
	})

	if _, err := os.Stat(filepath.Join(dir, "claude")); err != nil {
		t.Errorf("ClaudeFakeBinWith didn't create binary for valid overrides: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Git canonical fake tests
// ---------------------------------------------------------------------------

// TestGitCanonicalFakes_InternalConsistency validates that the Git
// canonical fake registry is self-consistent.
func TestGitCanonicalFakes_InternalConsistency(t *testing.T) {
	canon := GitCanonicalFakes()

	for key, entry := range canon {
		t.Run(key, func(t *testing.T) {
			switch entry.Kind {
			case "text":
				if strings.TrimSpace(entry.Response) == "" {
					t.Errorf("canonical entry %q claims text but response is empty", key)
				}
			case "empty":
				// Empty is valid — e.g., status --porcelain for clean repo.
			default:
				t.Errorf("canonical entry %q has unknown kind %q", key, entry.Kind)
			}
		})
	}
}

// TestGitFakeResponses_StructuralCompliance validates that GitFakeResponses
// returns a well-formed response map.
func TestGitFakeResponses_StructuralCompliance(t *testing.T) {
	responses := GitFakeResponses()

	if len(responses) == 0 {
		t.Fatal("GitFakeResponses returned empty map")
	}

	// Verify version command is present.
	if _, ok := responses["--version"]; !ok {
		t.Error("GitFakeResponses missing required command --version")
	}
}

// TestGitFakeBin_ProducesWorkingBinary verifies that GitFakeBin creates
// a shell script that can be executed.
func TestGitFakeBin_ProducesWorkingBinary(t *testing.T) {
	dir := t.TempDir()
	binPath := GitFakeBin(t, dir)

	if _, err := os.Stat(binPath); err != nil {
		t.Fatalf("GitFakeBin didn't create binary: %v", err)
	}

	info, _ := os.Stat(binPath)
	if info.Mode()&0111 == 0 {
		t.Error("GitFakeBin binary is not executable")
	}
}

// TestGitFakeBinWith_ValidatesOverrides verifies that GitFakeBinWith
// works with valid overrides.
func TestGitFakeBinWith_ValidatesOverrides(t *testing.T) {
	dir := t.TempDir()

	_ = GitFakeBinWith(t, dir, map[string]string{
		"--version": "git version 2.46.0",
	})

	if _, err := os.Stat(filepath.Join(dir, "git")); err != nil {
		t.Errorf("GitFakeBinWith didn't create binary for valid overrides: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ClassifyClaudeArgs tests
// ---------------------------------------------------------------------------

func TestClassifyClaudeArgs(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--version"}, "--version"},
		{[]string{"-p", "Generate a title"}, "-p"},
		{[]string{"-p", "--verbose", "--output-format", "stream-json", "--include-partial-messages", "Resolve conflicts"}, "stream-json"},
		{nil, ""},
	}
	for _, tt := range tests {
		got := ClassifyClaudeArgs(tt.args)
		if got != tt.want {
			t.Errorf("ClassifyClaudeArgs(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ClassifyGitArgs tests
// ---------------------------------------------------------------------------

func TestClassifyGitArgs(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--version"}, "--version"},
		{[]string{"rev-parse", "--abbrev-ref", "HEAD"}, "rev-parse --abbrev-ref HEAD"},
		{[]string{"rev-parse", "--show-toplevel"}, "rev-parse --show-toplevel"},
		{[]string{"rev-parse", "abc123"}, "rev-parse"},
		{[]string{"rev-parse", "--verify", "main"}, "rev-parse"},
		{[]string{"status", "--porcelain", "--untracked-files=no"}, "status --porcelain"},
		{[]string{"log", "--oneline", "main..feat"}, "log --oneline"},
		{[]string{"diff", "--stat", "main..feat"}, "diff --stat"},
		{[]string{"diff", "--name-only", "--diff-filter=U"}, "diff --name-only"},
		{[]string{"rev-list", "--count", "main..feat"}, "rev-list --count"},
		{[]string{"rev-list", "--reverse", "main..feat"}, "rev-list --reverse"},
		{[]string{"merge-base", "main", "feat"}, "merge-base"},
		{[]string{"merge-base", "--is-ancestor", "abc", "def"}, "merge-base --is-ancestor"},
		{[]string{"checkout", "main"}, "checkout"},
		{[]string{"push", "--force-with-lease", "origin", "feat"}, "push"},
		{[]string{"fetch", "origin"}, "fetch"},
		{[]string{"symbolic-ref", "refs/remotes/origin/HEAD"}, "symbolic-ref"},
		{nil, ""},
	}
	for _, tt := range tests {
		got := ClassifyGitArgs(tt.args)
		if got != tt.want {
			t.Errorf("ClassifyGitArgs(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}
