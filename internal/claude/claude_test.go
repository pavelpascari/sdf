package claude

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pavelpascari/sdf/internal/testutil"
)

func TestRunPrompt_ReturnsFakeResponse(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.ClaudeFakeBin(t, dir)
	testutil.SetBinary(t, &Binary, fake)

	result, err := RunPrompt("test-session", "Generate a title for this PR")
	if err != nil {
		t.Fatalf("RunPrompt failed: %v", err)
	}

	if result != "This is a generated PR title" {
		t.Errorf("unexpected result: %q", result)
	}

	log := testutil.ReadLog(t, dir, "claude")
	if len(log) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(log))
	}
	// Should pass -p and the prompt
	if log[0] != "-p Generate a title for this PR" {
		t.Errorf("unexpected arguments: %s", log[0])
	}
}

func TestVersion_ReturnsFakeVersion(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.ClaudeFakeBin(t, dir)
	testutil.SetBinary(t, &Binary, fake)

	ver, err := Version()
	if err != nil {
		t.Fatalf("Version failed: %v", err)
	}

	if ver != "claude-code 1.0.0" {
		t.Errorf("unexpected version: %q", ver)
	}
}

func TestAvailable_WithFakeBinary(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.FakeBin(t, dir, "claude-test", map[string]string{})
	testutil.SetBinary(t, &Binary, fake)

	if !Available() {
		t.Error("expected Available()=true with fake binary")
	}
}

func TestAvailable_Missing(t *testing.T) {
	testutil.SetBinary(t, &Binary, filepath.Join(t.TempDir(), "nonexistent"))

	if Available() {
		t.Error("expected Available()=false with nonexistent binary")
	}
}

func TestRunPrompt_Error(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.FakeBinFail(t, dir, "claude", "API error")
	testutil.SetBinary(t, &Binary, fake)

	_, err := RunPrompt("test-session", "some prompt")
	if err == nil {
		t.Fatal("expected error from failing binary")
	}
}

func TestRunPromptStreaming_ReturnsFakeResponse(t *testing.T) {
	dir := t.TempDir()
	// Use a focused fake that only has stream-json (not -p) to avoid
	// the shell case statement matching -p first in the streaming args.
	fake := testutil.FakeBin(t, dir, "claude", map[string]string{
		"stream-json": testutil.ClaudeCanonicalFakes()["stream-json"].Response,
	})
	testutil.SetBinary(t, &Binary, fake)

	var buf bytes.Buffer
	result, err := RunPromptStreaming("test-stream", "Resolve conflicts", &buf)
	if err != nil {
		t.Fatalf("RunPromptStreaming failed: %v", err)
	}

	// The stream-json canonical fake sends a result event with this text.
	want := "Resolved conflict in main.go by keeping both changes."
	if result.Result != want {
		t.Errorf("result = %q, want %q", result.Result, want)
	}

	log := testutil.ReadLog(t, dir, "claude")
	if len(log) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(log))
	}
	// Should include streaming flags.
	if !strings.Contains(log[0], "stream-json") {
		t.Errorf("expected stream-json in args, got: %s", log[0])
	}
}

func TestRunPromptStreamingWithOpts_PassesAllowedTools(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.FakeBin(t, dir, "claude", map[string]string{
		"stream-json": testutil.ClaudeCanonicalFakes()["stream-json"].Response,
	})
	testutil.SetBinary(t, &Binary, fake)

	var buf bytes.Buffer
	opts := PromptOptions{AllowedTools: []string{"Write", "Read"}}
	_, err := RunPromptStreamingWithOpts("test-opts", "Do something", &buf, opts)
	if err != nil {
		t.Fatalf("RunPromptStreamingWithOpts failed: %v", err)
	}

	log := testutil.ReadLog(t, dir, "claude")
	if len(log) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(log))
	}
	args := log[0]
	if !strings.Contains(args, "--allowedTools Write") {
		t.Errorf("expected --allowedTools Write in args, got: %s", args)
	}
	if !strings.Contains(args, "--allowedTools Read") {
		t.Errorf("expected --allowedTools Read in args, got: %s", args)
	}
}

func TestSanitizeSessionName(t *testing.T) {
	tests := []struct {
		prefix, branch, want string
	}{
		{"pr-title", "feat/auth", "pr-title-feat-auth"},
		{"conflict", "users feature/db schema", "conflict-users-feature-db-schema"},
		{"pr-desc", "simple", "pr-desc-simple"},
	}
	for _, tt := range tests {
		got := SanitizeSessionName(tt.prefix, tt.branch)
		if got != tt.want {
			t.Errorf("SanitizeSessionName(%q, %q) = %q, want %q", tt.prefix, tt.branch, got, tt.want)
		}
	}
}
