package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// UpdateGolden is a package-level flag that controls whether golden files
// should be regenerated. Set via `go test -update` in test code.
//
// Usage in tests:
//
//	var update = flag.Bool("update", false, "update golden files")
//
//	func TestFoo(t *testing.T) {
//	    actual := doSomething()
//	    testutil.AssertGolden(t, "testdata/foo.golden", *update, actual)
//	}
//
// Run with: go test -update ./...

// AssertGolden compares actual against a golden file. If update is true,
// the golden file is written instead. If the golden file doesn't exist
// and update is false, the test fails.
func AssertGolden(t *testing.T, goldenPath string, update bool, actual string) {
	t.Helper()

	if update {
		dir := filepath.Dir(goldenPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("cannot create golden dir %s: %v", dir, err)
		}
		if err := os.WriteFile(goldenPath, []byte(actual), 0644); err != nil {
			t.Fatalf("cannot write golden file %s: %v", goldenPath, err)
		}
		t.Logf("updated golden file: %s", goldenPath)
		return
	}

	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("golden file %s not found — run with -update to create it:\n  go test -run %s -update ./...", goldenPath, t.Name())
	}

	if string(expected) != actual {
		t.Errorf("output does not match golden file %s\n\n--- want ---\n%s\n--- got ---\n%s\n--- diff ---\n%s",
			goldenPath, string(expected), actual, lineDiff(string(expected), actual))
	}
}

// lineDiff produces a simple line-by-line diff for readability.
func lineDiff(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")

	var b strings.Builder
	maxLen := len(wantLines)
	if len(gotLines) > maxLen {
		maxLen = len(gotLines)
	}

	for i := 0; i < maxLen; i++ {
		var wl, gl string
		if i < len(wantLines) {
			wl = wantLines[i]
		}
		if i < len(gotLines) {
			gl = gotLines[i]
		}
		if wl != gl {
			if i < len(wantLines) {
				b.WriteString("- " + wl + "\n")
			}
			if i < len(gotLines) {
				b.WriteString("+ " + gl + "\n")
			}
		}
	}
	return b.String()
}
