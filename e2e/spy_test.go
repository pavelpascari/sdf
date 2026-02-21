//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/spy"
	"github.com/pavelpascari/sdf/internal/testutil"
)

// TestE2E_RecordAndValidate runs a stack lifecycle with the gh spy enabled,
// captures all real GitHub API responses, then verifies that:
//
//  1. Recordings are written and contain expected invocations
//  2. The recorded JSON responses have the structure our code expects
//  3. The responses can be converted to FakeBin fixtures that are
//     structurally compatible with reality
//
// This is the bridge between E2E and unit tests: it proves our fakes
// are honest representations of the real API.
func TestE2E_RecordAndValidate(t *testing.T) {
	dir := e2eRepo(t)
	prefix := testPrefix()

	// Set up spy recording
	recordDir := filepath.Join(t.TempDir(), "recordings")
	rec := spy.NewRecorder(recordDir, "gh")
	testutil.SetSpy(t, &ghpkg.Spy, rec)

	t.Cleanup(func() {
		runGit(t, dir, "checkout", "main")
		cleanupPRs(t, dir, prefix)
		cleanupBranches(t, dir, prefix)
		os.RemoveAll(dir + "/.sdf")
	})

	// --- Run a small lifecycle that exercises key gh commands ---
	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "pull", "origin", "main")

	stackName := prefix

	// init + branch + commit
	runSDF(t, dir, "init", "--base", "main", "--branch", "rec-a", stackName)
	writeCommit(t, dir, prefix+"-rec-a.txt", "recording test A\n", "feat: rec A")

	runSDF(t, dir, "branch", "rec-b")
	writeCommit(t, dir, prefix+"-rec-b.txt", "recording test B\n", "feat: rec B")

	branchA := stackName + "/rec-a"
	branchB := stackName + "/rec-b"

	// Create PRs (exercises: pr create, pr view)
	runGit(t, dir, "checkout", branchA)
	runSDF(t, dir, "pr", "--json")

	runGit(t, dir, "checkout", branchB)
	runSDF(t, dir, "pr", "--json")

	// Sync (exercises: pr list, pr edit)
	runSDF(t, dir, "sync", "-y")

	// Force close the recorder so the file is flushed
	rec.Close()

	// --- Validate recordings ---
	recordingPath := filepath.Join(recordDir, "gh.jsonl")
	recordings := testutil.ReadRecordings(t, recordingPath)

	t.Logf("Captured %d gh invocations", len(recordings))
	if len(recordings) == 0 {
		t.Fatal("expected at least 1 recorded invocation, got 0")
	}

	// Log all captured invocations for visibility
	for i, inv := range recordings {
		argsSummary := strings.Join(inv.Args, " ")
		if len(argsSummary) > 80 {
			argsSummary = argsSummary[:80] + "..."
		}
		stdoutPreview := inv.Stdout
		if len(stdoutPreview) > 60 {
			stdoutPreview = stdoutPreview[:60] + "..."
		}
		t.Logf("  [%d] exit=%d args=%q stdout=%q", i, inv.ExitCode, argsSummary, stdoutPreview)
	}

	// --- Check that expected command types were recorded ---
	commandsSeen := make(map[string]bool)
	for _, inv := range recordings {
		if len(inv.Args) >= 2 {
			commandsSeen[inv.Args[0]+" "+inv.Args[1]] = true
		}
	}

	expectedCommands := []string{"pr list", "pr view", "pr create"}
	for _, cmd := range expectedCommands {
		if !commandsSeen[cmd] {
			t.Errorf("expected to see %q in recordings, but it was not captured", cmd)
		}
	}

	// --- Validate JSON structure of responses ---
	for _, inv := range recordings {
		if inv.ExitCode != 0 {
			continue
		}
		stdout := strings.TrimSpace(inv.Stdout)
		if stdout == "" || (!strings.HasPrefix(stdout, "{") && !strings.HasPrefix(stdout, "[")) {
			continue // skip non-JSON responses (like PR URLs)
		}

		// Verify it's valid JSON
		if strings.HasPrefix(stdout, "[") {
			var arr []json.RawMessage
			if err := json.Unmarshal([]byte(stdout), &arr); err != nil {
				t.Errorf("invalid JSON array from %v: %v", inv.Args[:2], err)
			}
		} else {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal([]byte(stdout), &obj); err != nil {
				t.Errorf("invalid JSON object from %v: %v", inv.Args[:2], err)
			}
		}
	}

	// --- Cross-validate: convert recordings to fake responses and verify structure ---
	fakeResponses := testutil.RecordingsToFakeResponses(recordings, 2)
	t.Logf("Generated %d fake response entries from recordings", len(fakeResponses))

	testutil.ValidateFakeAgainstRecordings(t, fakeResponses, recordings, 2)

	// --- Verify specific structural expectations ---
	// pr list should return array of objects with our expected fields
	if resp, ok := fakeResponses["pr list"]; ok {
		var prs []ghpkg.PRInfo
		if err := json.Unmarshal([]byte(resp), &prs); err != nil {
			t.Errorf("recorded pr list response doesn't parse as []PRInfo: %v", err)
		} else {
			t.Logf("pr list response contains %d PRs, parseable as []PRInfo", len(prs))
		}
	}

	// pr view should return object with number field
	if resp, ok := fakeResponses["pr view"]; ok {
		var pr ghpkg.PRInfo
		if err := json.Unmarshal([]byte(resp), &pr); err != nil {
			t.Errorf("recorded pr view response doesn't parse as PRInfo: %v", err)
		} else {
			t.Logf("pr view response: PR #%d (%s), parseable as PRInfo", pr.Number, pr.State)
		}
	}

	t.Log("Recording cross-validation passed — fakes match real API structure")
}
