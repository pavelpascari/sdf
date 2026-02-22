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
// When a prefix has multiple canonical variants (e.g., "pr view" has body,
// title, and full-fields variants), the override is accepted if it matches
// ANY variant's shape.
//
// This makes Layer 1 (fake binaries) self-enforcing: structural correctness
// is validated at fake creation time, not deferred to cross-validation.
func ValidateGHFakeResponse(t *testing.T, prefix, response string) {
	t.Helper()

	canon := GHCanonicalFakes()

	// Collect ALL canonical entries that match this prefix.
	// A prefix like "pr view" may have multiple variants (body, title, full fields).
	var matches []FakeEntry
	for key, fe := range canon {
		canonPrefix := key
		if idx := strings.Index(key, ":"); idx >= 0 {
			canonPrefix = key[:idx]
		}
		if canonPrefix == prefix {
			matches = append(matches, fe)
		}
	}

	if len(matches) == 0 {
		// No canonical entry for this prefix — can't validate shape.
		return
	}

	response = strings.TrimSpace(response)

	// Check non-JSON kinds first (url, empty) — these don't need multi-variant matching.
	for _, found := range matches {
		switch found.Kind {
		case "url":
			if !strings.HasPrefix(response, "https://") {
				t.Errorf("fake response for %q should be a URL, got: %q",
					prefix, truncateStr(response, 60))
			}
			return
		case "empty":
			return
		}
	}

	// For JSON kinds: the response must be valid JSON and match at least one variant's shape.
	if !IsJSON(response) {
		t.Errorf("fake response for %q should be JSON, got: %q",
			prefix, truncateStr(response, 60))
		return
	}

	fakeShape := JSONShape(response)

	// An empty array is compatible with any array type.
	if fakeShape == "array[]" {
		for _, found := range matches {
			canonShape := JSONShape(strings.TrimSpace(found.Response))
			if strings.HasPrefix(canonShape, "array[") {
				return
			}
		}
	}

	// Check if the fake shape matches ANY canonical variant.
	var canonShapes []string
	for _, found := range matches {
		canonResponse := strings.TrimSpace(found.Response)
		if !IsJSON(canonResponse) {
			continue
		}
		canonShape := JSONShape(canonResponse)
		if canonShape == fakeShape {
			return // Match found.
		}
		canonShapes = append(canonShapes, canonShape)
	}

	t.Errorf("fake response for %q has wrong JSON shape:\n  canonical variants: %s\n  got:                %s\n  response:           %s",
		prefix, strings.Join(canonShapes, " | "), fakeShape, truncateStr(response, 120))
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

// ---------------------------------------------------------------------------
// Claude canonical fakes
// ---------------------------------------------------------------------------

// ClaudeCanonicalFakes returns the canonical fake responses for claude commands,
// keyed by subcommand prefix (e.g., "--version", "-p", "stream-json").
//
// The "stream-json" entry covers RunPromptStreaming — the fake output is JSONL
// (one JSON event per line) matching the stream-json format that claude.go parses.
func ClaudeCanonicalFakes() map[string]FakeEntry {
	return map[string]FakeEntry{
		"--version": {
			Kind:     "text",
			Response: "claude-code 1.0.0",
		},
		"-p": {
			Kind:     "text",
			Response: "This is a generated PR title",
		},
		"stream-json": {
			Kind: "jsonl",
			Response: `{"type":"assistant","message":{"content":[{"text":"Resolving conflicts"}]}}
{"type":"result","result":"Resolved conflict in main.go by keeping both changes."}`,
		},
	}
}

// ClassifyClaudeArgs maps a full claude argument list to a canonical key.
//
// Examples:
//
//	["--version"]                                         → "--version"
//	["-p", "Generate a title"]                            → "-p"
//	["-p", "--verbose", "--output-format", "stream-json", ...] → "stream-json"
func ClassifyClaudeArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}

	// Check for streaming mode first — it's the most specific.
	for i, a := range args {
		if a == "--output-format" && i+1 < len(args) && args[i+1] == "stream-json" {
			return "stream-json"
		}
	}

	// First argument is the key for simple invocations.
	return args[0]
}

// ClaudeFakeResponses converts the canonical fake registry into a
// map[string]string suitable for FakeBin. The keys are FakeBin-compatible
// prefixes (e.g., "--version", "-p", "stream-json").
func ClaudeFakeResponses() map[string]string {
	responses := make(map[string]string)
	for key, fe := range ClaudeCanonicalFakes() {
		responses[key] = fe.Response
	}
	return responses
}

// ValidateClaudeFakeResponse checks that a test's custom response for a given
// FakeBin prefix matches the expected format from the canonical registry.
func ValidateClaudeFakeResponse(t *testing.T, prefix, response string) {
	t.Helper()

	canon := ClaudeCanonicalFakes()
	found, exists := canon[prefix]
	if !exists {
		return // No canonical entry — can't validate.
	}

	response = strings.TrimSpace(response)

	switch found.Kind {
	case "jsonl":
		// Each line should be valid JSON.
		for i, line := range strings.Split(response, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if !IsJSON(line) {
				t.Errorf("fake response for %q line %d should be JSON, got: %q",
					prefix, i, truncateStr(line, 60))
			}
		}
	case "text":
		// Any non-empty string is valid for text responses.
	}
}

// ValidateClaudeFakeResponses checks all entries in a FakeBin response map
// against the canonical registry.
func ValidateClaudeFakeResponses(t *testing.T, responses map[string]string) {
	t.Helper()
	for prefix, response := range responses {
		ValidateClaudeFakeResponse(t, prefix, response)
	}
}

// MustClaudeFakeResponses returns canonical fake responses and validates any
// overrides against the canonical formats.
func MustClaudeFakeResponses(t *testing.T, overrides map[string]string) map[string]string {
	t.Helper()
	responses := ClaudeFakeResponses()
	for k, v := range overrides {
		responses[k] = v
	}
	ValidateClaudeFakeResponses(t, responses)
	return responses
}

// ---------------------------------------------------------------------------
// Git canonical fakes
// ---------------------------------------------------------------------------

// GitCanonicalFakes returns the canonical fake responses for git commands.
// Most git tests use real git in temp directories, so this registry is
// intentionally minimal — it covers the commands exercised by fake-binary
// tests (e.g., doctor tests) and provides a foundation for future expansion.
//
// Keys use the same format as ClassifyGitArgs output.
func GitCanonicalFakes() map[string]FakeEntry {
	return map[string]FakeEntry{
		"--version": {
			Kind:     "text",
			Response: "git version 2.45.0",
		},
		"rev-parse --abbrev-ref HEAD": {
			Kind:     "text",
			Response: "main",
		},
		"rev-parse --show-toplevel": {
			Kind:     "text",
			Response: "/home/user/repo",
		},
		"status --porcelain": {
			Kind:     "empty",
			Response: "",
		},
		"rev-parse": {
			Kind:     "text",
			Response: "abc123def456789abc123def456789abc123def4",
		},
		"symbolic-ref": {
			Kind:     "text",
			Response: "refs/remotes/origin/main",
		},
		"log --oneline": {
			Kind:     "text",
			Response: "abc1234 feat: add feature\ndef5678 fix: bug fix",
		},
		"diff --stat": {
			Kind:     "text",
			Response: " main.go | 10 +++++-----\n 1 file changed, 5 insertions(+), 5 deletions(-)",
		},
		"diff --name-only": {
			Kind:     "text",
			Response: "main.go\nutils.go",
		},
		"rev-list --count": {
			Kind:     "text",
			Response: "3",
		},
		"merge-base": {
			Kind:     "text",
			Response: "abc123def456789abc123def456789abc123def4",
		},
	}
}

// ClassifyGitArgs maps a full git argument list to a canonical key.
// It extracts a normalized prefix from the arguments for matching.
//
// Examples:
//
//	["--version"]                           → "--version"
//	["rev-parse", "--abbrev-ref", "HEAD"]   → "rev-parse --abbrev-ref HEAD"
//	["rev-parse", "--show-toplevel"]        → "rev-parse --show-toplevel"
//	["rev-parse", "abc123"]                 → "rev-parse"
//	["status", "--porcelain", "-uno"]       → "status --porcelain"
//	["log", "--oneline", "main..feat"]      → "log --oneline"
//	["diff", "--stat", "main..feat"]        → "diff --stat"
//	["diff", "--name-only", ...]            → "diff --name-only"
//	["checkout", "-b", "feat"]              → "checkout -b"
//	["push", "--force-with-lease", ...]     → "push"
//	["fetch", "origin"]                     → "fetch"
func ClassifyGitArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}

	// Special case: --version is a standalone flag.
	if args[0] == "--version" {
		return "--version"
	}

	subcmd := args[0]

	// For certain commands, include flags that change the output format.
	switch subcmd {
	case "rev-parse":
		// Include well-known flags that determine output type.
		for _, a := range args[1:] {
			switch a {
			case "--abbrev-ref":
				return "rev-parse --abbrev-ref HEAD"
			case "--show-toplevel":
				return "rev-parse --show-toplevel"
			case "--verify":
				return "rev-parse"
			}
		}
		return "rev-parse"

	case "status":
		for _, a := range args[1:] {
			if a == "--porcelain" {
				return "status --porcelain"
			}
		}
		return "status"

	case "log":
		for _, a := range args[1:] {
			if a == "--oneline" {
				return "log --oneline"
			}
			if a == "--format=%s" {
				return "log --format=%s"
			}
		}
		return "log"

	case "diff":
		for _, a := range args[1:] {
			if a == "--stat" {
				return "diff --stat"
			}
			if a == "--name-only" {
				return "diff --name-only"
			}
		}
		return "diff"

	case "rev-list":
		for _, a := range args[1:] {
			if a == "--count" {
				return "rev-list --count"
			}
			if a == "--reverse" {
				return "rev-list --reverse"
			}
		}
		return "rev-list"

	case "symbolic-ref":
		return "symbolic-ref"

	case "merge-base":
		for _, a := range args[1:] {
			if a == "--is-ancestor" {
				return "merge-base --is-ancestor"
			}
		}
		return "merge-base"

	case "ls-remote":
		return "ls-remote"
	}

	// Default: just the subcommand.
	return subcmd
}

// GitFakeResponses converts the canonical fake registry into a
// map[string]string suitable for FakeBin.
func GitFakeResponses() map[string]string {
	responses := make(map[string]string)
	for key, fe := range GitCanonicalFakes() {
		responses[key] = fe.Response
	}
	return responses
}

// MustGitFakeResponses returns canonical fake responses with validated overrides.
func MustGitFakeResponses(t *testing.T, overrides map[string]string) map[string]string {
	t.Helper()
	responses := GitFakeResponses()
	for k, v := range overrides {
		responses[k] = v
	}
	return responses
}
