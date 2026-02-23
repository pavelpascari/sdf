package version

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
		input                      string
		major, minor, patch        int
		ok                         bool
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

func TestCheck_NewerVersionAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"version":"0.3.0","changelog":"https://github.com/pavelpascari/sdf/blob/main/CHANGELOG.md"}`))
	}))
	defer srv.Close()

	origURL := VersionURL
	origClient := HTTPClient
	VersionURL = srv.URL
	HTTPClient = srv.Client()
	defer func() {
		VersionURL = origURL
		HTTPClient = origClient
	}()

	// Should not panic; we just verify it runs without error.
	// The function prints to stdout — a full assertion would capture stdout,
	// but this confirms no crash on the happy path.
	Check("0.1.0")
}

func TestCheck_SameVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"version":"0.1.0","changelog":"https://github.com/pavelpascari/sdf/blob/main/CHANGELOG.md"}`))
	}))
	defer srv.Close()

	origURL := VersionURL
	origClient := HTTPClient
	VersionURL = srv.URL
	HTTPClient = srv.Client()
	defer func() {
		VersionURL = origURL
		HTTPClient = origClient
	}()

	// Should silently do nothing.
	Check("0.1.0")
}

func TestCheck_DevVersion(t *testing.T) {
	// Should return immediately without making any HTTP request.
	Check("dev")
}

func TestCheck_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	origURL := VersionURL
	origClient := HTTPClient
	VersionURL = srv.URL
	HTTPClient = srv.Client()
	defer func() {
		VersionURL = origURL
		HTTPClient = origClient
	}()

	// Should not panic on server error.
	Check("0.1.0")
}
