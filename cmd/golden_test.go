package cmd

import (
	"flag"
	"path/filepath"
	"testing"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/testutil"
)

var update = flag.Bool("update", false, "update golden files")

// --- buildStackNav golden tests ---

func TestBuildStackNav_ThreeBranches(t *testing.T) {
	s := &stack.Stack{
		StackID: "auth-feature",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "auth/db-schema", PR: 10, Status: "open"},
			{Branch: "auth/api-endpoints", PR: 11, Status: "open"},
			{Branch: "auth/frontend", PR: 12, Status: "open"},
		},
	}

	prs := map[int]ghpkg.PRInfo{
		10: {Number: 10, URL: "https://github.com/test/repo/pull/10", HeadRefName: "auth/db-schema"},
		11: {Number: 11, URL: "https://github.com/test/repo/pull/11", HeadRefName: "auth/api-endpoints"},
		12: {Number: 12, URL: "https://github.com/test/repo/pull/12", HeadRefName: "auth/frontend"},
	}

	actual := buildStackNav(s, prs, "auth/api-endpoints")
	testutil.AssertGolden(t, filepath.Join("testdata", "stacknav_three_branches.golden"), *update, actual)
}

func TestBuildStackNav_WithMergedBranch(t *testing.T) {
	s := &stack.Stack{
		StackID: "payments",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "pay/models", PR: 20, Status: "merged"},
			{Branch: "pay/api", PR: 21, Status: "open"},
			{Branch: "pay/ui", PR: 0, Status: "open"},
		},
	}

	prs := map[int]ghpkg.PRInfo{
		20: {Number: 20, URL: "https://github.com/test/repo/pull/20", HeadRefName: "pay/models"},
		21: {Number: 21, URL: "https://github.com/test/repo/pull/21", HeadRefName: "pay/api"},
	}

	actual := buildStackNav(s, prs, "pay/api")
	testutil.AssertGolden(t, filepath.Join("testdata", "stacknav_with_merged.golden"), *update, actual)
}

// --- replaceStackNav golden tests ---

func TestReplaceStackNav_Insert(t *testing.T) {
	body := "Part of stack: **auth**\n\nBase: `main`"
	nav := "<!-- sdf:stack-nav -->\n---\nStack: `auth`\n1. https://github.com/test/pull/1 ◀ this PR\n<!-- /sdf:stack-nav -->"

	actual := replaceStackNav(body, nav)
	testutil.AssertGolden(t, filepath.Join("testdata", "replacenav_insert.golden"), *update, actual)
}

func TestReplaceStackNav_Replace(t *testing.T) {
	body := "Part of stack: **auth**\n\n<!-- sdf:stack-nav -->\n---\nStack: `auth`\n1. old nav\n<!-- /sdf:stack-nav -->"
	nav := "<!-- sdf:stack-nav -->\n---\nStack: `auth`\n1. https://github.com/test/pull/1 ◀ this PR\n2. https://github.com/test/pull/2\n<!-- /sdf:stack-nav -->"

	actual := replaceStackNav(body, nav)
	testutil.AssertGolden(t, filepath.Join("testdata", "replacenav_replace.golden"), *update, actual)
}

// --- replaceDescription golden tests ---

func TestReplaceDescription_Insert(t *testing.T) {
	body := "Part of stack: **auth**\n\nBase: `main`"
	desc := "This PR adds the database schema for user authentication."

	actual := replaceDescription(body, desc)
	testutil.AssertGolden(t, filepath.Join("testdata", "replacedesc_insert.golden"), *update, actual)
}

func TestReplaceDescription_Replace(t *testing.T) {
	body := "<!-- sdf:description -->\nOld description here.\n<!-- /sdf:description -->\n\nPart of stack: **auth**"
	desc := "Updated: adds JWT token support."

	actual := replaceDescription(body, desc)
	testutil.AssertGolden(t, filepath.Join("testdata", "replacedesc_replace.golden"), *update, actual)
}

// --- buildTitlePrompt golden tests ---

func TestBuildTitlePrompt_Golden(t *testing.T) {
	subjects := []string{"feat: add user auth", "fix: handle edge case"}
	diff := "diff --git a/auth.go b/auth.go\n+func Login() {}\n"

	actual := buildTitlePrompt("feat/auth", subjects, diff, "")
	testutil.AssertGolden(t, filepath.Join("testdata", "titleprompt_basic.golden"), *update, actual)
}

func TestBuildTitlePrompt_WithExisting_Golden(t *testing.T) {
	subjects := []string{"feat: add user auth"}
	diff := ""

	actual := buildTitlePrompt("feat/auth", subjects, diff, "add user authentication")
	testutil.AssertGolden(t, filepath.Join("testdata", "titleprompt_existing.golden"), *update, actual)
}

// --- buildDescriptionPrompt golden tests ---

func TestBuildDescriptionPrompt_Golden(t *testing.T) {
	subjects := []string{"feat: add user auth", "fix: handle edge case"}
	diff := "diff --git a/auth.go b/auth.go\n+func Login() {}\n"

	actual := buildDescriptionPrompt("feat/auth", subjects, diff, "")
	testutil.AssertGolden(t, filepath.Join("testdata", "descprompt_basic.golden"), *update, actual)
}

func TestBuildDescriptionPrompt_WithExisting_Golden(t *testing.T) {
	subjects := []string{"feat: add user auth"}
	diff := ""

	actual := buildDescriptionPrompt("feat/auth", subjects, diff, "This adds user authentication.")
	testutil.AssertGolden(t, filepath.Join("testdata", "descprompt_existing.golden"), *update, actual)
}

// --- buildConflictPrompt golden tests ---

func TestBuildConflictPrompt_Golden(t *testing.T) {
	upstream := " auth.go | 5 +++++\n 1 file changed, 5 insertions(+)"
	branchDesc := "This PR adds JWT-based authentication for the API layer."
	files := map[string]string{
		"auth.go": "<<<<<<< HEAD\nfunc Login() { /* old */ }\n=======\nfunc Login() { /* new */ }\n>>>>>>> feat/auth",
	}

	actual := buildConflictPrompt(upstream, branchDesc, files)
	testutil.AssertGolden(t, filepath.Join("testdata", "conflictprompt_basic.golden"), *update, actual)
}
