package testutil

import (
	"sort"
	"strings"
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
