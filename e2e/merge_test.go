//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestE2E_MergeRetargetOrdering verifies the most critical correctness
// property of sdf merge: the downstream PR's base MUST be retargeted
// BEFORE the head PR is merged+deleted, otherwise GitHub auto-closes
// the downstream PR.
//
// Scenario: A developer builds an authentication feature in two stacked PRs:
//
//	main ← add-login-page (PR#1) ← add-session-management (PR#2)
//
// When the login page PR is merged first:
//   - The session-management PR's base is retargeted to main (BEFORE merge)
//   - The login page PR is merged and its branch deleted
//   - The session-management PR must still be OPEN with base=main
func TestE2E_MergeRetargetOrdering(t *testing.T) {
	dir := e2eRepo(t)
	setupRecording(t)
	prefix := testPrefix()

	t.Cleanup(func() {
		runGit(t, dir, "checkout", "main")
		cleanupPRs(t, dir, prefix)
		cleanupBranches(t, dir, prefix)
		os.RemoveAll(dir + "/.sdf")
	})

	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "pull", "origin", "main")

	stackName := prefix

	// Build a 2-branch authentication stack
	t.Log("Initializing auth stack: add-login-page → add-session-management")
	runSDF(t, dir, "init", "--base", "main", "--branch", "add-login-page", stackName)
	writeCommit(t, dir, prefix+"-login.html", "<form action=\"/login\"><input name=\"email\"/></form>\n", "feat: add login page with email form")

	runSDF(t, dir, "branch", "add-session-management")
	writeCommit(t, dir, prefix+"-session.go", "package auth\n\nfunc CreateSession(userID string) {}\n", "feat: add session creation after login")

	branchLogin := stackName + "/add-login-page"
	branchSession := stackName + "/add-session-management"

	// Create PRs for both branches
	runGit(t, dir, "checkout", branchLogin)
	prLoginOut := runSDF(t, dir, "pr", "--json")
	var prLogin struct {
		Number int `json:"number"`
	}
	json.Unmarshal([]byte(prLoginOut), &prLogin)
	t.Logf("PR#%d created for %s", prLogin.Number, branchLogin)

	runGit(t, dir, "checkout", branchSession)
	prSessionOut := runSDF(t, dir, "pr", "--json")
	var prSession struct {
		Number int `json:"number"`
	}
	json.Unmarshal([]byte(prSessionOut), &prSession)
	t.Logf("PR#%d created for %s", prSession.Number, branchSession)

	// Verify: 2 nodes with PRs, both open
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/add-login-page", HasPR: true, Status: "open"},
		{BranchSuffix: "/add-session-management", HasPR: true, Status: "open"},
	})

	// Confirm session-management PR targets the login-page branch
	pr2Info := runGH(t, dir, "pr", "view", fmt.Sprint(prSession.Number), "--json", "baseRefName,state")
	t.Logf("PR#%d before merge: %s", prSession.Number, pr2Info)
	if !strings.Contains(pr2Info, "add-login-page") {
		t.Fatalf("expected PR#%d base to contain 'add-login-page', got: %s", prSession.Number, pr2Info)
	}

	// Merge the login-page PR — the critical ordering test
	t.Log("Merging add-login-page PR via sdf merge")
	runGit(t, dir, "checkout", branchSession) // must be on a stack branch
	mergeOut := runSDF(t, dir, "merge", "-y")
	t.Log(mergeOut)

	// THE CRITICAL ASSERTION: session-management PR must still be open
	t.Log("Verifying add-session-management PR survived the merge")
	pr2After := runGH(t, dir, "pr", "view", fmt.Sprint(prSession.Number), "--json", "baseRefName,state")
	t.Logf("PR#%d after merge: %s", prSession.Number, pr2After)

	var pr2State struct {
		BaseRefName string `json:"baseRefName"`
		State       string `json:"state"`
	}
	if err := json.Unmarshal([]byte(pr2After), &pr2State); err != nil {
		t.Fatalf("cannot parse PR state: %v", err)
	}

	if pr2State.State != "OPEN" {
		t.Errorf("PR#%d should be OPEN after merge, got %s — retarget-before-merge ordering may be broken",
			prSession.Number, pr2State.State)
	}

	// Session-management PR's base should now be main
	if pr2State.BaseRefName != "main" {
		t.Errorf("PR#%d base should be 'main' after merge, got %q",
			prSession.Number, pr2State.BaseRefName)
	}

	// Verify login-page PR is merged
	pr1After := runGH(t, dir, "pr", "view", fmt.Sprint(prLogin.Number), "--json", "state")
	if !strings.Contains(pr1After, "MERGED") {
		t.Errorf("PR#%d should be MERGED, got: %s", prLogin.Number, pr1After)
	}

	// Stack reflects the merge: login merged, session still open
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/add-login-page", HasPR: true, Status: "merged"},
		{BranchSuffix: "/add-session-management", HasPR: true, Status: "open"},
	})

	t.Log("Merge ordering verified — session-management PR survived the login-page merge")
}

// TestE2E_MergeThenSync verifies that after merging the head PR,
// sdf sync correctly rebases remaining branches onto the new base.
//
// Scenario: A developer builds a search feature in three stacked PRs:
//
//	main ← add-search-index ← add-search-api ← add-search-ui
//
// After the search-index PR is merged, the remaining two PRs should
// be rebased correctly with search-api now targeting main.
func TestE2E_MergeThenSync(t *testing.T) {
	dir := e2eRepo(t)
	setupRecording(t)
	prefix := testPrefix()

	t.Cleanup(func() {
		runGit(t, dir, "checkout", "main")
		cleanupPRs(t, dir, prefix)
		cleanupBranches(t, dir, prefix)
		os.RemoveAll(dir + "/.sdf")
	})

	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "pull", "origin", "main")

	stackName := prefix

	// Build a 3-branch search feature stack
	t.Log("Building search feature stack: add-search-index → add-search-api → add-search-ui")

	runSDF(t, dir, "init", "--base", "main", "--branch", "add-search-index", stackName)
	writeCommit(t, dir, prefix+"-index.go", "package search\n\ntype Index struct{ docs map[string]string }\n", "feat: add full-text search index")

	runSDF(t, dir, "branch", "add-search-api")
	writeCommit(t, dir, prefix+"-search-api.go", "package api\n\nfunc SearchHandler(w http.ResponseWriter, r *http.Request) {}\n", "feat: add search API endpoint")

	runSDF(t, dir, "branch", "add-search-ui")
	writeCommit(t, dir, prefix+"-search-ui.tsx", "export const SearchBar = () => <input type=\"search\" placeholder=\"Search...\"/>;\n", "feat: add search bar component")

	// Create PRs for all 3 branches
	for _, br := range []string{stackName + "/add-search-index", stackName + "/add-search-api", stackName + "/add-search-ui"} {
		runGit(t, dir, "checkout", br)
		runSDF(t, dir, "pr", "--json")
	}

	// Verify: 3 nodes with PRs, all open
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/add-search-index", HasPR: true, Status: "open"},
		{BranchSuffix: "/add-search-api", HasPR: true, Status: "open"},
		{BranchSuffix: "/add-search-ui", HasPR: true, Status: "open"},
	})

	// Merge the search-index PR (head of the stack)
	t.Log("Merging add-search-index PR...")
	runGit(t, dir, "checkout", stackName+"/add-search-ui")
	mergeOut := runSDF(t, dir, "merge", "-y")
	t.Log(mergeOut)

	// Verify remaining PRs are still open
	t.Log("Verifying add-search-api and add-search-ui PRs remain open")

	branchAPI := stackName + "/add-search-api"
	branchUI := stackName + "/add-search-ui"

	for _, br := range []string{branchAPI, branchUI} {
		info := runGH(t, dir, "pr", "view", br, "--json", "state,baseRefName")
		if !strings.Contains(info, "OPEN") {
			t.Errorf("PR for %s should be OPEN, got: %s", br, info)
		}
	}

	// search-api should now target main (since search-index was merged)
	apiInfo := runGH(t, dir, "pr", "view", branchAPI, "--json", "baseRefName")
	if !strings.Contains(apiInfo, "main") {
		t.Errorf("after merge, add-search-api base should be main, got: %s", apiInfo)
	}

	// Stack reflects the merge
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/add-search-index", HasPR: true, Status: "merged"},
		{BranchSuffix: "/add-search-api", HasPR: true, Status: "open"},
		{BranchSuffix: "/add-search-ui", HasPR: true, Status: "open"},
	})

	t.Log("Merge-then-sync verified — search-api and search-ui survived the merge")
}
