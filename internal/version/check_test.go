package version

import (
	"fmt"
	"testing"

	"github.com/pavelpascari/sdf/internal/gh"
)

func TestIsNewer(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.1.0", "0.2.0", false},
		{"0.1.0", "0.1.0", false},
		{"1.0.0", "0.9.9", true},
		{"0.10.0", "0.9.0", true},
		{"0.1.1", "0.1.0", true},
		{"v0.2.0", "0.1.0", true},
		{"0.2.0", "v0.1.0", true},
		{"v0.2.0", "v0.1.0", true},
	}

	for _, tt := range tests {
		got := isNewer(tt.latest, tt.current)
		if got != tt.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input               string
		major, minor, patch int
		ok                  bool
	}{
		{"0.1.0", 0, 1, 0, true},
		{"1.2.3", 1, 2, 3, true},
		{"v1.2.3", 1, 2, 3, true},
		{"1.2.3-beta", 1, 2, 3, true},
		{"bad", 0, 0, 0, false},
		{"1.2", 0, 0, 0, false},
		{"", 0, 0, 0, false},
	}

	for _, tt := range tests {
		major, minor, patch, ok := parseSemver(tt.input)
		if ok != tt.ok || major != tt.major || minor != tt.minor || patch != tt.patch {
			t.Errorf("parseSemver(%q) = (%d,%d,%d,%v), want (%d,%d,%d,%v)",
				tt.input, major, minor, patch, ok,
				tt.major, tt.minor, tt.patch, tt.ok)
		}
	}
}

func stubRelease(tag, url string) func() {
	orig := LatestReleaseFunc
	LatestReleaseFunc = func() (*gh.ReleaseInfo, error) {
		return &gh.ReleaseInfo{TagName: tag, URL: url}, nil
	}
	return func() { LatestReleaseFunc = orig }
}

func stubReleaseError() func() {
	orig := LatestReleaseFunc
	LatestReleaseFunc = func() (*gh.ReleaseInfo, error) {
		return nil, fmt.Errorf("gh: not found")
	}
	return func() { LatestReleaseFunc = orig }
}

func TestCheck_NewerVersionAvailable(t *testing.T) {
	defer stubRelease("v0.3.0", "https://github.com/pavelpascari/sdf/releases/tag/v0.3.0")()

	// Should not panic; we just verify it runs without error.
	// The function prints to stdout — a full assertion would capture stdout,
	// but this confirms no crash on the happy path.
	Check("0.1.0")
}

func TestCheck_SameVersion(t *testing.T) {
	defer stubRelease("v0.1.0", "https://github.com/pavelpascari/sdf/releases/tag/v0.1.0")()

	// Should silently do nothing.
	Check("0.1.0")
}

func TestCheck_DevVersion(t *testing.T) {
	// Should return immediately without calling gh.
	Check("dev")
}

func TestCheck_GhError(t *testing.T) {
	defer stubReleaseError()()

	// Should not panic when gh fails.
	Check("0.1.0")
}
