package cmd

import (
	"strings"
	"testing"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
)

func TestNavHash_Deterministic(t *testing.T) {
	s := &stack.Stack{
		StackID: "my-stack",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "my-stack/first", PR: 10, Status: "open"},
			{Branch: "my-stack/second", PR: 11, Status: "open"},
		},
	}

	prs := map[int]ghpkg.PRInfo{
		10: {Number: 10, URL: "https://github.com/owner/repo/pull/10", State: "OPEN"},
		11: {Number: 11, URL: "https://github.com/owner/repo/pull/11", State: "OPEN"},
	}

	hash1 := navHash(s, prs, "my-stack/first")
	hash2 := navHash(s, prs, "my-stack/first")

	if hash1 != hash2 {
		t.Errorf("same input produced different hashes: %s vs %s", hash1, hash2)
	}
}

func TestNavHash_ChangesWhenStackChanges(t *testing.T) {
	s := &stack.Stack{
		StackID: "my-stack",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "my-stack/first", PR: 10, Status: "open"},
		},
	}

	prs := map[int]ghpkg.PRInfo{
		10: {Number: 10, URL: "https://github.com/owner/repo/pull/10", State: "OPEN"},
	}

	hashBefore := navHash(s, prs, "my-stack/first")

	// Add a node
	s.Nodes = append(s.Nodes, stack.Node{Branch: "my-stack/second", PR: 11, Status: "open"})
	prs[11] = ghpkg.PRInfo{Number: 11, URL: "https://github.com/owner/repo/pull/11", State: "OPEN"}

	hashAfter := navHash(s, prs, "my-stack/first")

	if hashBefore == hashAfter {
		t.Error("hash should change when stack changes")
	}
}

func TestNavHash_SameWhenStatusChanges(t *testing.T) {
	s := &stack.Stack{
		StackID: "my-stack",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "my-stack/first", PR: 10, Status: "open"},
		},
	}

	prsOpen := map[int]ghpkg.PRInfo{
		10: {Number: 10, URL: "https://github.com/owner/repo/pull/10", State: "OPEN"},
	}
	prsMerged := map[int]ghpkg.PRInfo{
		10: {Number: 10, URL: "https://github.com/owner/repo/pull/10", State: "MERGED"},
	}

	hashOpen := navHash(s, prsOpen, "my-stack/first")
	hashMerged := navHash(s, prsMerged, "my-stack/first")

	if hashOpen != hashMerged {
		t.Error("hash should be the same — status is not part of nav output")
	}
}

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

	// Should contain bare PR URLs
	if !strings.Contains(nav, "https://github.com/owner/repo/pull/16") {
		t.Error("missing PR #16 URL")
	}
	if !strings.Contains(nav, "https://github.com/owner/repo/pull/17") {
		t.Error("missing PR #17 URL")
	}
	if !strings.Contains(nav, "https://github.com/owner/repo/pull/18") {
		t.Error("missing PR #18 URL")
	}

	// Current PR should be marked
	for _, line := range strings.Split(nav, "\n") {
		if strings.Contains(line, "pull/17") && !strings.Contains(line, "◀ this PR") {
			t.Error("current PR not marked")
		}
	}

	// Should not contain status text or markdown links
	if strings.Contains(nav, "- open") {
		t.Error("should not contain status text")
	}
	if strings.Contains(nav, "[#") {
		t.Error("should use bare URLs, not markdown links")
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

	// Merged PR should still have its URL (GitHub renders status via autolink)
	if !strings.Contains(nav, "https://github.com/owner/repo/pull/16") {
		t.Error("missing merged PR URL")
	}
	// Should not contain explicit status text
	if strings.Contains(nav, "merged") {
		t.Error("should not contain explicit status text — GitHub renders it via autolink")
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

// --- replaceDescription tests ---

func TestReplaceDescription_InsertsWhenNoMarkers(t *testing.T) {
	body := "Some existing content"
	result := replaceDescription(body, "New description")

	if !strings.Contains(result, descOpen) {
		t.Error("missing opening marker")
	}
	if !strings.Contains(result, descClose) {
		t.Error("missing closing marker")
	}
	if !strings.Contains(result, "New description") {
		t.Error("missing new description")
	}
	// Original content should be preserved after the description
	if !strings.Contains(result, "Some existing content") {
		t.Error("original content lost")
	}
}

func TestReplaceDescription_InsertsIntoEmptyBody(t *testing.T) {
	result := replaceDescription("", "First description")

	if !strings.HasPrefix(result, descOpen) {
		t.Error("should start with opening marker")
	}
	if !strings.Contains(result, "First description") {
		t.Error("missing description")
	}
}

func TestReplaceDescription_ReplacesExisting(t *testing.T) {
	body := descOpen + "\nOld description\n" + descClose + "\n\nUser notes here"
	result := replaceDescription(body, "Updated description")

	if !strings.Contains(result, "Updated description") {
		t.Error("new description missing")
	}
	if strings.Contains(result, "Old description") {
		t.Error("old description still present")
	}
	// Content after the close marker should be preserved
	if !strings.Contains(result, "User notes here") {
		t.Error("content after close marker lost")
	}
}

func TestReplaceDescription_PreservesNavSection(t *testing.T) {
	body := descOpen + "\nOld desc\n" + descClose + "\n\n" +
		stackNavOpen + "\nStack nav\n" + stackNavClose

	result := replaceDescription(body, "New desc")

	if !strings.Contains(result, "New desc") {
		t.Error("new description missing")
	}
	if !strings.Contains(result, stackNavOpen) {
		t.Error("nav section lost")
	}
	if !strings.Contains(result, "Stack nav") {
		t.Error("nav content lost")
	}
}

// --- extractDescription tests ---

func TestExtractDescription_WithMarkers(t *testing.T) {
	body := "preamble\n" + descOpen + "\nThis is the description.\n" + descClose + "\nfooter"

	result := extractDescription(body)
	if result != "This is the description." {
		t.Errorf("expected 'This is the description.', got %q", result)
	}
}

func TestExtractDescription_NoMarkers(t *testing.T) {
	body := "Just a plain body with no markers"
	result := extractDescription(body)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestExtractDescription_EmptyBetweenMarkers(t *testing.T) {
	body := descOpen + "\n\n" + descClose
	result := extractDescription(body)
	if result != "" {
		t.Errorf("expected empty string for whitespace-only content, got %q", result)
	}
}

// --- Body with both description and nav sections ---

func TestReplaceDescription_ThenNav_Roundtrip(t *testing.T) {
	// Simulate the flow: start empty, add description, then add nav
	body := ""
	body = replaceDescription(body, "PR adds authentication")
	nav := stackNavOpen + "\n---\nStack: test\n1. https://github.com/o/r/pull/1\n" + stackNavClose
	body = replaceStackNav(body, nav)

	// Both sections should be present
	if !strings.Contains(body, "PR adds authentication") {
		t.Error("description lost after adding nav")
	}
	if !strings.Contains(body, stackNavOpen) {
		t.Error("nav not present")
	}

	// Now update the description — nav should survive
	body = replaceDescription(body, "PR adds authentication and sessions")

	if !strings.Contains(body, "PR adds authentication and sessions") {
		t.Error("updated description missing")
	}
	if strings.Contains(body, "PR adds authentication\n") {
		t.Error("old description still present")
	}
	if !strings.Contains(body, stackNavOpen) {
		t.Error("nav lost after description update")
	}

	// Now update the nav — description should survive
	nav2 := stackNavOpen + "\n---\nStack: test\n1. https://github.com/o/r/pull/1\n2. https://github.com/o/r/pull/2\n" + stackNavClose
	body = replaceStackNav(body, nav2)

	if !strings.Contains(body, "PR adds authentication and sessions") {
		t.Error("description lost after nav update")
	}
	if !strings.Contains(body, "pull/2") {
		t.Error("new nav content missing")
	}
}

// --- similar (Jaccard) tests ---

func TestSimilar_Identical(t *testing.T) {
	if !similar("hello world", "hello world", 0.8) {
		t.Error("identical strings should be similar")
	}
}

func TestSimilar_BothEmpty(t *testing.T) {
	if !similar("", "", 0.8) {
		t.Error("two empty strings should be similar")
	}
}

func TestSimilar_OneEmpty(t *testing.T) {
	if similar("", "hello world", 0.8) {
		t.Error("empty vs non-empty should not be similar")
	}
}

func TestSimilar_HighOverlap(t *testing.T) {
	a := "add user authentication to the API"
	b := "add user authentication to the API endpoints"
	if !similar(a, b, 0.7) {
		t.Error("high overlap strings should be similar at 0.7 threshold")
	}
}

func TestSimilar_LowOverlap(t *testing.T) {
	a := "add user authentication"
	b := "fix database connection pooling"
	if similar(a, b, 0.5) {
		t.Error("unrelated strings should not be similar")
	}
}

func TestSimilar_CaseInsensitive(t *testing.T) {
	if !similar("Hello World", "hello world", 0.8) {
		t.Error("should be case insensitive")
	}
}

func TestSimilar_IgnoresPunctuation(t *testing.T) {
	if !similar("feat: add auth", "feat add auth", 0.8) {
		t.Error("should ignore punctuation")
	}
}

// --- buildStackNav: marker precision ---

func TestBuildStackNav_MarkerOnlyOnCurrentBranch(t *testing.T) {
	s := &stack.Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "test/a", PR: 1, Status: "open"},
			{Branch: "test/b", PR: 2, Status: "open"},
			{Branch: "test/c", PR: 3, Status: "open"},
		},
	}
	prs := map[int]ghpkg.PRInfo{
		1: {Number: 1, URL: "https://github.com/o/r/pull/1"},
		2: {Number: 2, URL: "https://github.com/o/r/pull/2"},
		3: {Number: 3, URL: "https://github.com/o/r/pull/3"},
	}

	nav := buildStackNav(s, prs, "test/b")

	lines := strings.Split(nav, "\n")
	markerCount := 0
	for _, line := range lines {
		if strings.Contains(line, "◀ this PR") {
			markerCount++
			if !strings.Contains(line, "pull/2") {
				t.Errorf("marker on wrong line: %s", line)
			}
		}
	}
	if markerCount != 1 {
		t.Errorf("expected exactly 1 marker, got %d", markerCount)
	}
}
