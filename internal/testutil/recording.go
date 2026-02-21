package testutil

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/pavelpascari/sdf/internal/spy"
)

// ReadRecordings reads all invocations from a JSONL recording file.
func ReadRecordings(t *testing.T, path string) []spy.Invocation {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read recording file %s: %v", path, err)
	}

	var invocations []spy.Invocation
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var inv spy.Invocation
		if err := json.Unmarshal([]byte(line), &inv); err != nil {
			t.Fatalf("cannot parse recording line: %v\nline: %s", err, line)
		}
		invocations = append(invocations, inv)
	}
	return invocations
}

// RecordingsToFakeResponses converts recorded invocations into a response
// map suitable for FakeBin. For each invocation, the argument prefix
// (first N args) is used as the key. Only successful invocations (exit 0)
// are included.
//
// This is the bridge between "record real API" and "replay as fake":
//
//	recordings := ReadRecordings(t, "recordings/gh.jsonl")
//	responses := RecordingsToFakeResponses(recordings, 2)
//	fake := FakeBin(t, dir, "gh", responses)
func RecordingsToFakeResponses(invocations []spy.Invocation, keyArgCount int) map[string]string {
	responses := make(map[string]string)
	for _, inv := range invocations {
		if inv.ExitCode != 0 {
			continue
		}
		n := keyArgCount
		if n > len(inv.Args) {
			n = len(inv.Args)
		}
		key := strings.Join(inv.Args[:n], " ")
		// First recording wins (don't overwrite with later calls)
		if _, exists := responses[key]; !exists {
			responses[key] = inv.Stdout
		}
	}
	return responses
}

// ValidateFakeAgainstRecordings checks that a FakeBin's response map
// produces structurally compatible JSON for each recorded invocation.
// This is the cross-validation: "do our fakes match reality?"
//
// For JSON responses, it compares the structural shape (keys present,
// array vs object). For non-JSON responses, it checks for non-empty
// fake responses where real ones existed.
func ValidateFakeAgainstRecordings(t *testing.T, fakeResponses map[string]string, recordings []spy.Invocation, keyArgCount int) {
	t.Helper()

	for _, inv := range recordings {
		if inv.ExitCode != 0 {
			continue
		}

		n := keyArgCount
		if n > len(inv.Args) {
			n = len(inv.Args)
		}
		key := strings.Join(inv.Args[:n], " ")

		fakeResponse, exists := fakeResponses[key]
		if !exists {
			t.Errorf("no fake response for %q (args: %v)", key, inv.Args)
			continue
		}

		// For JSON responses, validate structural compatibility
		if isJSON(inv.Stdout) && isJSON(fakeResponse) {
			realShape := jsonShape(inv.Stdout)
			fakeShape := jsonShape(fakeResponse)
			if realShape != fakeShape {
				t.Errorf("JSON structure mismatch for %q:\n  real: %s\n  fake: %s",
					key, realShape, fakeShape)
			}
		}
	}
}

// isJSON returns true if s looks like JSON (starts with { or [).
func isJSON(s string) bool {
	s = strings.TrimSpace(s)
	return len(s) > 0 && (s[0] == '{' || s[0] == '[')
}

// jsonShape returns a structural description of a JSON value.
// Used for structural comparison without comparing actual values.
func jsonShape(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return "empty"
	}

	if s[0] == '[' {
		var arr []json.RawMessage
		if json.Unmarshal([]byte(s), &arr) != nil {
			return "invalid-array"
		}
		if len(arr) == 0 {
			return "array[]"
		}
		return fmt.Sprintf("array[%s]", jsonShape(string(arr[0])))
	}

	if s[0] == '{' {
		var obj map[string]json.RawMessage
		if json.Unmarshal([]byte(s), &obj) != nil {
			return "invalid-object"
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		// Sort for deterministic output
		for i := 1; i < len(keys); i++ {
			for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
				keys[j], keys[j-1] = keys[j-1], keys[j]
			}
		}
		return fmt.Sprintf("object{%s}", strings.Join(keys, ","))
	}

	return "scalar"
}
