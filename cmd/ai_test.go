package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	"github.com/pavelpascari/sdf/internal/testutil"
)

// claudeStreamingFake creates a fake claude binary that only matches
// streaming invocations (stream-json). Using ClaudeFakeBin would also
// match "-p" which appears in streaming args, causing the wrong
// response to be returned.
func claudeStreamingFake(t *testing.T, dir string) string {
	t.Helper()
	return testutil.FakeBin(t, dir, "claude", map[string]string{
		"stream-json": testutil.ClaudeCanonicalFakes()["stream-json"].Response,
		"--version":   testutil.ClaudeCanonicalFakes()["--version"].Response,
	})
}

func TestAIIntro_InvokesClaude(t *testing.T) {
	dir := t.TempDir()
	fake := claudeStreamingFake(t, dir)
	testutil.SetBinary(t, &claudepkg.Binary, fake)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runAIIntro(nil, nil)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := stripANSI(buf.String())

	if err != nil {
		t.Fatalf("runAIIntro failed: %v", err)
	}

	// ReadLog splits by newlines, but the prompt contains newlines,
	// so join the full log to get the complete args string.
	log := testutil.ReadLog(t, dir, "claude")
	if len(log) == 0 {
		t.Fatal("expected at least 1 claude log line, got 0")
	}
	args := strings.Join(log, "\n")

	if !strings.Contains(args, "stream-json") {
		t.Errorf("expected stream-json in args")
	}
	if !strings.Contains(args, "--allowedTools Write") {
		t.Errorf("expected --allowedTools Write in args")
	}
	if !strings.Contains(args, "--allowedTools Read") {
		t.Errorf("expected --allowedTools Read in args")
	}

	// Verify the prompt contains SDF info
	if !strings.Contains(args, "SDF") {
		t.Errorf("expected prompt to mention SDF")
	}

	// Verify output contains status messages
	if !strings.Contains(output, "Skill created") {
		t.Errorf("expected success message in output, got:\n%s", output)
	}
}

func TestAIIntro_ClaudeNotAvailable(t *testing.T) {
	testutil.SetBinary(t, &claudepkg.Binary, "/nonexistent/claude")

	err := runAIIntro(nil, nil)
	if err == nil {
		t.Fatal("expected error when claude is not available")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("expected 'not installed' error, got: %v", err)
	}
}

