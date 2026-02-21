package claude

import (
	"path/filepath"
	"testing"

	"github.com/pavelpascari/sdf/internal/testutil"
)

func TestRunPrompt_ReturnsFakeResponse(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.FakeBin(t, dir, "claude", map[string]string{
		"-p": "This is a generated PR title",
	})
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
	fake := testutil.FakeBin(t, dir, "claude", map[string]string{
		"--version": "claude-code 1.0.0",
	})
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
