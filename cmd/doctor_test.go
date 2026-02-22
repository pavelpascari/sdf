package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/testutil"
)

func TestDoctor_AllAvailable(t *testing.T) {
	dir := t.TempDir()

	fakeGit := testutil.FakeBin(t, dir, "fake-git", map[string]string{
		"--version": "git version 2.45.0",
	})
	fakeGH := testutil.GHFakeBinWith(t, dir, map[string]string{
		"version": "gh version 2.50.0 (2024-06-01)",
	})
	fakeClaude := testutil.FakeBin(t, dir, "fake-claude", map[string]string{
		"--version": "claude-code 1.0.0",
	})

	testutil.SetBinary(t, &gitpkg.Binary, fakeGit)
	testutil.SetBinary(t, &ghpkg.Binary, fakeGH)
	testutil.SetBinary(t, &claudepkg.Binary, fakeClaude)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runDoctor(nil, nil)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := stripANSI(buf.String())

	if err != nil {
		t.Fatalf("runDoctor failed: %v", err)
	}

	checks := []string{
		"git version 2.45.0",
		"gh version 2.50.0",
		"claude-code 1.0.0",
		"All required dependencies are available.",
	}
	for _, check := range checks {
		if !contains(output, check) {
			t.Errorf("output missing %q\ngot:\n%s", check, output)
		}
	}
}

func TestDoctor_GitMissing(t *testing.T) {
	dir := t.TempDir()

	// Point git at a nonexistent binary
	testutil.SetBinary(t, &gitpkg.Binary, filepath.Join(dir, "nonexistent-git"))
	// gh and claude are available
	fakeGH := testutil.GHFakeBinWith(t, dir, map[string]string{
		"version": "gh version 2.50.0",
	})
	fakeClaude := testutil.FakeBin(t, dir, "fake-claude", map[string]string{
		"--version": "claude-code 1.0.0",
	})
	testutil.SetBinary(t, &ghpkg.Binary, fakeGH)
	testutil.SetBinary(t, &claudepkg.Binary, fakeClaude)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runDoctor(nil, nil)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := stripANSI(buf.String())

	if err == nil {
		t.Fatal("expected error when git is missing")
	}

	if !contains(output, "not found (required)") {
		t.Errorf("expected 'not found' message for git\ngot:\n%s", output)
	}
}

func TestDoctor_OptionalToolsMissing(t *testing.T) {
	dir := t.TempDir()

	// git is available, gh and claude are missing
	fakeGit := testutil.FakeBin(t, dir, "fake-git", map[string]string{
		"--version": "git version 2.45.0",
	})
	testutil.SetBinary(t, &gitpkg.Binary, fakeGit)
	testutil.SetBinary(t, &ghpkg.Binary, filepath.Join(dir, "nonexistent-gh"))
	testutil.SetBinary(t, &claudepkg.Binary, filepath.Join(dir, "nonexistent-claude"))

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runDoctor(nil, nil)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := stripANSI(buf.String())

	// Should succeed — gh and claude are optional
	if err != nil {
		t.Fatalf("runDoctor should succeed when only optional tools are missing, got: %v", err)
	}

	if !contains(output, "not found (needed for PR operations)") {
		t.Errorf("expected gh missing message\ngot:\n%s", output)
	}
	if !contains(output, "not found (needed for conflict resolution") {
		t.Errorf("expected claude missing message\ngot:\n%s", output)
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && bytes.Contains([]byte(s), []byte(substr))
}
