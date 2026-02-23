//go:build e2e

package e2e

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pavelpascari/sdf/internal/testutil"
)

// TestE2E_AIIntro_SpyRecording runs `sdf ai intro` and verifies the Claude
// spy captures the invocation with the correct arguments. Requires the
// -with-claude flag because it invokes the real Claude CLI.
func TestE2E_AIIntro_SpyRecording(t *testing.T) {
	if !*withClaude {
		t.Skip("-with-claude not set — skipping Claude-dependent test")
	}

	dir := e2eRepo(t)
	setupRecording(t)

	// Run sdf ai intro — this invokes Claude with the intro prompt.
	output := runSDF(t, dir, "ai", "intro")
	t.Logf("sdf ai intro output (first 200 chars): %.200s", output)

	// Verify spy recorded the claude invocation.
	recordingPath := filepath.Join(recordingsBaseDir(), runID, t.Name(), "claude_sdf.jsonl")
	recordings := testutil.ReadRecordings(t, recordingPath)

	t.Logf("Captured %d claude invocations", len(recordings))
	if len(recordings) == 0 {
		t.Fatal("expected at least 1 recorded claude invocation, got 0")
	}

	// Verify the invocation used streaming mode with correct args.
	inv := recordings[0]
	argsStr := strings.Join(inv.Args, " ")
	t.Logf("Claude args: %s", argsStr[:min(len(argsStr), 200)])

	if !strings.Contains(argsStr, "stream-json") {
		t.Error("expected stream-json in claude args")
	}
	if !strings.Contains(argsStr, "--allowedTools") {
		t.Error("expected --allowedTools in claude args")
	}

	// Verify the prompt mentions SDF (sanity check that the right prompt was sent).
	if !strings.Contains(argsStr, "SDF") {
		t.Error("expected prompt to contain 'SDF'")
	}

	// Verify the full recording log also captured this.
	fullPath := filepath.Join(recordingsBaseDir(), runID, t.Name(), "full.jsonl")
	fullRecordings := testutil.ReadRecordings(t, fullPath)

	claudeInFull := false
	for _, r := range fullRecordings {
		if r.Binary == "claude" {
			claudeInFull = true
			break
		}
	}
	if !claudeInFull {
		t.Error("expected claude invocation in full.jsonl recording")
	}

	t.Log("Claude spy recording validated — ai intro invocation captured correctly")
}

// TestE2E_AISetup_SpyRecording runs `sdf ai setup` and verifies the Claude
// spy captures the invocation with setup-specific arguments (Edit tool, hooks).
func TestE2E_AISetup_SpyRecording(t *testing.T) {
	if !*withClaude {
		t.Skip("-with-claude not set — skipping Claude-dependent test")
	}

	dir := e2eRepo(t)
	setupRecording(t)

	output := runSDF(t, dir, "ai", "setup")
	t.Logf("sdf ai setup output (first 200 chars): %.200s", output)

	recordingPath := filepath.Join(recordingsBaseDir(), runID, t.Name(), "claude_sdf.jsonl")
	recordings := testutil.ReadRecordings(t, recordingPath)

	t.Logf("Captured %d claude invocations", len(recordings))
	if len(recordings) == 0 {
		t.Fatal("expected at least 1 recorded claude invocation, got 0")
	}

	inv := recordings[0]
	argsStr := strings.Join(inv.Args, " ")

	// Setup adds Edit to allowed tools (intro only has Write, Read, Bash).
	if !strings.Contains(argsStr, "Edit") {
		t.Error("expected Edit in allowed tools for ai setup")
	}

	// Setup prompt includes hooks/settings.json content.
	if !strings.Contains(argsStr, "settings.json") {
		t.Error("expected settings.json in setup prompt")
	}

	t.Log("Claude spy recording validated — ai setup invocation captured correctly")
}
