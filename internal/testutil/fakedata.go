package testutil

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// FakeEntry describes a canonical fake response and its expected output format.
type FakeEntry struct {
	Response string // The canned response string
	Kind     string // "json-array", "json-object", "url", "empty"
}

// GHCanonicalFakes returns the canonical fake responses for gh commands,
// keyed by subcommand + sorted --json fields (e.g., "pr list:baseRefName,headRefName,...").
//
// These match the JSON structures that unit tests rely on across
// internal/gh/gh_test.go and cmd/sync_gh_test.go.
func GHCanonicalFakes() map[string]FakeEntry {
	return map[string]FakeEntry{
		"pr list:baseRefName,headRefName,number,state,url": {
			Kind: "json-array",
			Response: `[
				{"number":1,"headRefName":"feat/a","state":"OPEN","baseRefName":"main","url":"https://github.com/test/repo/pull/1"},
				{"number":2,"headRefName":"feat/b","state":"MERGED","baseRefName":"feat/a","url":"https://github.com/test/repo/pull/2"}
			]`,
		},
		"pr view:baseRefName,headRefName,number,state,url": {
			Kind: "json-object",
			Response: `{"number":42,"headRefName":"feat/auth","state":"OPEN","baseRefName":"main","url":"https://github.com/test/repo/pull/42"}`,
		},
		"pr view:body": {
			Kind:     "json-object",
			Response: `{"body":"This is the PR body."}`,
		},
		"pr view:title": {
			Kind:     "json-object",
			Response: `{"title":"feat: add auth"}`,
		},
		"pr create": {
			Kind:     "url",
			Response: "https://github.com/test/repo/pull/42",
		},
		"pr edit": {
			Kind:     "empty",
			Response: "",
		},
		"pr merge": {
			Kind:     "empty",
			Response: "",
		},
		"version": {
			Kind:     "empty",
			Response: "gh version 2.50.0",
		},
	}
}

// ClassifyGHArgs maps a full gh argument list to a canonical key.
// It extracts the subcommand (first 1-2 positional args) and appends
// sorted --json fields when present.
//
// Examples:
//
//	["pr","list","--state","all","--json","number,headRefName,..."] → "pr list:headRefName,number,..."
//	["pr","view","42","--json","body"]                             → "pr view:body"
//	["pr","create","--title","x","--head","y"]                     → "pr create"
//	["version"]                                                    → "version"
func ClassifyGHArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}

	// Extract subcommand: first 1-2 positional args (before any --flag).
	var subcmd []string
	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			break
		}
		subcmd = append(subcmd, a)
		if len(subcmd) == 2 {
			break
		}
	}

	key := strings.Join(subcmd, " ")

	// Find --json value if present.
	for i, a := range args {
		if a == "--json" && i+1 < len(args) {
			fields := strings.Split(args[i+1], ",")
			sort.Strings(fields)
			key += ":" + strings.Join(fields, ",")
			break
		}
	}

	return key
}

// GHFakeResponses converts the canonical fake registry into a
// map[string]string suitable for FakeBin. The keys are FakeBin-compatible
// prefixes (e.g., "pr list", "pr view", "pr create").
//
// For commands with multiple --json variants (like "pr view"), the default
// response uses the most complete variant (full PR fields).
//
// This is the primary way to get structurally correct fake gh responses
// without hardcoding JSON in every test.
func GHFakeResponses() map[string]string {
	responses := make(map[string]string)
	canon := GHCanonicalFakes()

	// Priority order for pr view variants: full fields > body > title.
	// When multiple canonical entries map to the same FakeBin prefix,
	// we keep the one with the most fields (longest key).
	type entry struct {
		prefix   string
		response string
		keyLen   int
	}

	var entries []entry
	for key, fe := range canon {
		prefix := key
		if idx := strings.Index(key, ":"); idx >= 0 {
			prefix = key[:idx]
		}
		entries = append(entries, entry{prefix, fe.Response, len(key)})
	}

	// Sort by key length descending so that the most specific variant
	// (longest canonical key) wins for each prefix.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].keyLen > entries[j].keyLen
	})
	for _, e := range entries {
		if _, exists := responses[e.prefix]; !exists {
			responses[e.prefix] = e.response
		}
	}

	return responses
}

// canonicalPrefixToKind maps FakeBin-compatible prefixes to the expected
// response kind from the canonical registry.
func canonicalPrefixToKind() map[string]string {
	kinds := make(map[string]string)
	for key, fe := range GHCanonicalFakes() {
		prefix := key
		if idx := strings.Index(key, ":"); idx >= 0 {
			prefix = key[:idx]
		}
		// Keep the most descriptive kind (json-array > json-object > url > empty).
		if _, exists := kinds[prefix]; !exists {
			kinds[prefix] = fe.Kind
		}
	}
	return kinds
}

// ValidateGHFakeResponse checks that a test's custom response for a given
// FakeBin prefix matches the expected structural shape from the canonical
// registry. Call this when a test uses custom data but wants to ensure
// structural compliance.
//
// For JSON responses, it compares the JSON shape (key sets for objects,
// element shape for arrays). For URL responses, it checks for https:// prefix.
// For empty/text responses, any value is accepted.
//
// This makes Layer 1 (fake binaries) self-enforcing: structural correctness
// is validated at fake creation time, not deferred to cross-validation.
func ValidateGHFakeResponse(t *testing.T, prefix, response string) {
	t.Helper()

	canon := GHCanonicalFakes()

	// Find the canonical entry for this prefix. Try exact match first,
	// then look for any canonical key that starts with this prefix.
	var found *FakeEntry
	for key, fe := range canon {
		canonPrefix := key
		if idx := strings.Index(key, ":"); idx >= 0 {
			canonPrefix = key[:idx]
		}
		if canonPrefix == prefix {
			feCopy := fe
			found = &feCopy
			break
		}
	}

	if found == nil {
		// No canonical entry for this prefix — can't validate shape.
		return
	}

	response = strings.TrimSpace(response)
	canonResponse := strings.TrimSpace(found.Response)

	switch found.Kind {
	case "json-array", "json-object":
		if !IsJSON(response) {
			t.Errorf("fake response for %q should be JSON (%s), got: %q",
				prefix, found.Kind, truncateStr(response, 60))
			return
		}
		if !IsJSON(canonResponse) {
			return // canonical entry isn't valid JSON, skip shape check
		}
		realShape := JSONShape(canonResponse)
		fakeShape := JSONShape(response)
		// An empty array ("array[]") is structurally compatible with any
		// array type — it's a valid degenerate case (e.g., pr list
		// returning no PRs). Skip shape mismatch for empty collections.
		if fakeShape == "array[]" && strings.HasPrefix(realShape, "array[") {
			break
		}
		if realShape != fakeShape {
			t.Errorf("fake response for %q has wrong JSON shape:\n  canonical: %s\n  got:       %s\n  response:  %s",
				prefix, realShape, fakeShape, truncateStr(response, 120))
		}

	case "url":
		if !strings.HasPrefix(response, "https://") {
			t.Errorf("fake response for %q should be a URL, got: %q",
				prefix, truncateStr(response, 60))
		}

	case "empty":
		// Any value is acceptable for empty/text responses.
	}
}

func truncateStr(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// ValidateGHFakeResponses checks all entries in a FakeBin response map
// against the canonical registry. This is the batch version of
// ValidateGHFakeResponse.
func ValidateGHFakeResponses(t *testing.T, responses map[string]string) {
	t.Helper()
	for prefix, response := range responses {
		ValidateGHFakeResponse(t, prefix, response)
	}
}

// MustGHFakeResponses returns canonical fake responses and validates any
// overrides against the canonical shapes. Overrides replace canonical
// responses for matching prefixes. The merged result is validated
// for structural compliance.
//
// Usage:
//
//	responses := testutil.MustGHFakeResponses(t, map[string]string{
//	    "pr list": `[{"number":10,"headRefName":"my-branch",...}]`,
//	})
//	fake := testutil.FakeBin(t, dir, "gh", responses)
func MustGHFakeResponses(t *testing.T, overrides map[string]string) map[string]string {
	t.Helper()
	responses := GHFakeResponses()
	for k, v := range overrides {
		responses[k] = v
	}
	ValidateGHFakeResponses(t, responses)
	return responses
}

// GHFakeResponsesFor returns a minimal response map containing only the
// specified prefixes, sourced from the canonical registry. This is useful
// when a test only exercises specific gh commands and wants to avoid
// matching unrelated commands.
//
// Usage:
//
//	responses := testutil.GHFakeResponsesFor("pr list", "pr edit")
func GHFakeResponsesFor(prefixes ...string) map[string]string {
	all := GHFakeResponses()
	responses := make(map[string]string, len(prefixes))
	for _, p := range prefixes {
		if v, ok := all[p]; ok {
			responses[p] = v
		}
	}
	return responses
}

// GHFakeResponsesForWith returns canonical responses for the specified
// prefixes with test-specific overrides, validated for structural compliance.
//
// Usage:
//
//	responses := testutil.GHFakeResponsesForWith(t,
//	    []string{"pr list", "pr edit"},
//	    map[string]string{
//	        "pr list": `[{"number":10,"headRefName":"branchA","state":"OPEN","baseRefName":"main","url":""}]`,
//	    },
//	)
func GHFakeResponsesForWith(t *testing.T, prefixes []string, overrides map[string]string) map[string]string {
	t.Helper()
	responses := GHFakeResponsesFor(prefixes...)
	for k, v := range overrides {
		responses[k] = v
	}
	ValidateGHFakeResponses(t, responses)
	return responses
}

// GHShapeDescription returns a human-readable description of the expected
// response shape for a given FakeBin prefix. Useful in error messages.
func GHShapeDescription(prefix string) string {
	canon := GHCanonicalFakes()
	for key, fe := range canon {
		canonPrefix := key
		if idx := strings.Index(key, ":"); idx >= 0 {
			canonPrefix = key[:idx]
		}
		if canonPrefix == prefix {
			switch fe.Kind {
			case "json-array":
				return fmt.Sprintf("JSON array with shape: %s", JSONShape(fe.Response))
			case "json-object":
				return fmt.Sprintf("JSON object with shape: %s", JSONShape(fe.Response))
			case "url":
				return "URL (https://...)"
			case "empty":
				return "empty or text"
			}
		}
	}
	return "unknown (no canonical entry)"
}
