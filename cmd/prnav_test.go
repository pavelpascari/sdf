package cmd

import (
	"strings"
	"testing"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
)

func TestBuildStackNav(t *testing.T) {
	s := &stack.Stack{
		StackID: "init-dx",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "init-dx/branch-creation", PR: 16, Status: "open"},
			{Branch: "init-dx/json-output", PR: 17, Status: "open"},
			{Branch: "init-dx/docs", PR: 18, Status: "open"},
		},
	}

	prs := map[int]ghpkg.PRInfo{
		16: {Number: 16, URL: "https://github.com/owner/repo/pull/16", State: "OPEN"},
		17: {Number: 17, URL: "https://github.com/owner/repo/pull/17", State: "OPEN"},
		18: {Number: 18, URL: "https://github.com/owner/repo/pull/18", State: "OPEN"},
	}

	nav := buildStackNav(s, prs, "init-dx/json-output")

	// Should contain the marker comments
	if !strings.Contains(nav, "<!-- sdf:stack-nav -->") {
		t.Error("missing opening marker")
	}
	if !strings.Contains(nav, "<!-- /sdf:stack-nav -->") {
		t.Error("missing closing marker")
	}

	// Should contain stack name
	if !strings.Contains(nav, "`init-dx`") {
		t.Error("missing stack name")
	}

	// Should contain PR links
	if !strings.Contains(nav, "[#16") {
		t.Error("missing PR #16 link")
	}
	if !strings.Contains(nav, "[#17") {
		t.Error("missing PR #17 link")
	}
	if !strings.Contains(nav, "[#18") {
		t.Error("missing PR #18 link")
	}

	// Current PR should be marked
	if !strings.Contains(nav, "this PR") {
		t.Error("current PR not marked")
	}

	// The marker for current PR should be on the line with #17
	for _, line := range strings.Split(nav, "\n") {
		if strings.Contains(line, "#17") && !strings.Contains(line, "this PR") {
			t.Error("current PR marker not on the #17 line")
		}
	}
}

func TestBuildStackNav_WithMergedPR(t *testing.T) {
	s := &stack.Stack{
		StackID: "init-dx",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "init-dx/branch-creation", PR: 16, Status: "merged"},
			{Branch: "init-dx/json-output", PR: 17, Status: "open"},
		},
	}

	prs := map[int]ghpkg.PRInfo{
		16: {Number: 16, URL: "https://github.com/owner/repo/pull/16", State: "MERGED"},
		17: {Number: 17, URL: "https://github.com/owner/repo/pull/17", State: "OPEN"},
	}

	nav := buildStackNav(s, prs, "init-dx/json-output")

	// Merged PR should show merged status
	for _, line := range strings.Split(nav, "\n") {
		if strings.Contains(line, "#16") {
			if !strings.Contains(strings.ToLower(line), "merged") {
				t.Errorf("expected merged status for PR #16, got: %s", line)
			}
		}
	}
}

func TestBuildStackNav_NodeWithoutPR(t *testing.T) {
	s := &stack.Stack{
		StackID: "my-stack",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "my-stack/first", PR: 10, Status: "open"},
			{Branch: "my-stack/second", Status: "open"}, // no PR yet
		},
	}

	prs := map[int]ghpkg.PRInfo{
		10: {Number: 10, URL: "https://github.com/owner/repo/pull/10", State: "OPEN"},
	}

	nav := buildStackNav(s, prs, "my-stack/first")

	// Should still list the branch without a PR link
	if !strings.Contains(nav, "my-stack/second") {
		t.Error("missing branch without PR")
	}
	// Should not contain a broken link for the no-PR branch
	if strings.Contains(nav, "[#0") {
		t.Error("should not contain link for branch without PR")
	}
}

func TestReplaceStackNav_InsertsWhenMissing(t *testing.T) {
	body := "## Intent\nThis PR does something.\n\n---\n*Part of stack: **init-dx***"
	nav := "<!-- sdf:stack-nav -->\n---\nStack nav here\n<!-- /sdf:stack-nav -->"

	result := replaceStackNav(body, nav)

	// Should append nav at the end
	if !strings.HasSuffix(strings.TrimSpace(result), "<!-- /sdf:stack-nav -->") {
		t.Error("nav not appended at end")
	}
	// Original body should be preserved
	if !strings.Contains(result, "This PR does something.") {
		t.Error("original body lost")
	}
}

func TestReplaceStackNav_ReplacesExisting(t *testing.T) {
	body := "## Intent\nThis PR does something.\n\n<!-- sdf:stack-nav -->\nold nav content\n<!-- /sdf:stack-nav -->"
	nav := "<!-- sdf:stack-nav -->\nnew nav content\n<!-- /sdf:stack-nav -->"

	result := replaceStackNav(body, nav)

	// Should contain new nav
	if !strings.Contains(result, "new nav content") {
		t.Error("new nav not present")
	}
	// Should not contain old nav
	if strings.Contains(result, "old nav content") {
		t.Error("old nav still present")
	}
	// Original body preserved
	if !strings.Contains(result, "This PR does something.") {
		t.Error("original body lost")
	}
}
