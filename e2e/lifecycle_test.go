//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestE2E_FullStackLifecycle exercises the complete sdf workflow by building
// a user-registration feature as a stack of three dependent PRs:
//
//	main ← db-schema ← api-endpoints ← frontend
//
// Scenario: A developer breaks a "user registration" feature into three
// reviewable pieces — the database layer, the API, and the UI — then
// creates PRs for each and verifies everything is in sync.
//
// This test creates real branches and real PRs on the sandbox repo.
func TestE2E_FullStackLifecycle(t *testing.T) {
	dir := e2eRepo(t)
	setupRecording(t)
	prefix := testPrefix()

	// Cleanup after test
	t.Cleanup(func() {
		runGit(t, dir, "checkout", "main")
		cleanupPRs(t, dir, prefix)
		cleanupBranches(t, dir, prefix)
		// Remove local .sdf state
		os.RemoveAll(dir + "/.sdf")
		runGit(t, dir, "checkout", "main")
	})

	// Ensure we start on main with a clean tree
	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "pull", "origin", "main")

	stackName := prefix

	// Initialize the stack with the first branch: the database schema
	t.Log("Initializing user-registration stack with the database schema branch")
	output := runSDF(t, dir, "new", "--base", "main", "--branch", "db-schema", stackName)
	t.Log(output)

	// Verify we're on the new branch
	branch := runGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	expectedBranch := stackName + "/db-schema"
	if branch != expectedBranch {
		t.Fatalf("expected branch %s, got %s", expectedBranch, branch)
	}

	// Add the users table migration
	writeCommit(t, dir, prefix+"-schema.sql", "CREATE TABLE users (id INT);\n", "feat: add users table schema")

	// Verify: stack has 1 node, no PR yet
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/db-schema", Status: "open"},
	})

	// Stack the API layer on top of the database schema
	t.Log("Adding api-endpoints branch on top of db-schema")
	runSDF(t, dir, "branch", "api-endpoints")

	branch = runGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	expectedBranch = stackName + "/api-endpoints"
	if branch != expectedBranch {
		t.Fatalf("expected branch %s, got %s", expectedBranch, branch)
	}

	writeCommit(t, dir, prefix+"-api.go", "package api\n\nfunc CreateUser() {}\n", "feat: add user creation endpoint")

	// Verify: stack has 2 nodes
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/db-schema", Status: "open"},
		{BranchSuffix: "/api-endpoints", Status: "open"},
	})

	// Stack the frontend on top of the API
	t.Log("Adding frontend branch on top of api-endpoints")
	runSDF(t, dir, "branch", "frontend")

	branch = runGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	expectedBranch = stackName + "/frontend"
	if branch != expectedBranch {
		t.Fatalf("expected branch %s, got %s", expectedBranch, branch)
	}

	writeCommit(t, dir, prefix+"-ui.tsx", "export const UserForm = () => <form/>;\n", "feat: add user registration form")

	// Verify: stack has 3 nodes, no PRs yet
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/db-schema", Status: "open"},
		{BranchSuffix: "/api-endpoints", Status: "open"},
		{BranchSuffix: "/frontend", Status: "open"},
	})

	// Create pull requests for all three branches
	t.Log("Creating PRs for db-schema, api-endpoints, and frontend")

	branches := []string{
		stackName + "/db-schema",
		stackName + "/api-endpoints",
		stackName + "/frontend",
	}

	type prResult struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
		Title  string `json:"title"`
	}

	prNumbers := make([]int, 3)
	for i, br := range branches {
		runGit(t, dir, "checkout", br)
		out := runSDF(t, dir, "pr", "--json")
		t.Logf("PR created for %s: %s", br, out)

		var result prResult
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatalf("cannot parse PR JSON for %s: %v\noutput: %s", br, err, out)
		}
		if result.Number == 0 {
			t.Fatalf("PR number is 0 for %s", br)
		}
		prNumbers[i] = result.Number
	}

	// Verify: all 3 nodes now have PRs
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/db-schema", HasPR: true, Status: "open"},
		{BranchSuffix: "/api-endpoints", HasPR: true, Status: "open"},
		{BranchSuffix: "/frontend", HasPR: true, Status: "open"},
	})

	// Verify PR base chain: db-schema→main, api-endpoints→db-schema, frontend→api-endpoints
	t.Log("Verifying PR base chain on GitHub")

	// db-schema PR should target main
	pr1 := runGH(t, dir, "pr", "view", branches[0], "--json", "baseRefName")
	if !strings.Contains(pr1, `"main"`) {
		t.Errorf("PR for db-schema should have base=main, got: %s", pr1)
	}

	// api-endpoints PR should target db-schema
	pr2 := runGH(t, dir, "pr", "view", branches[1], "--json", "baseRefName")
	if !strings.Contains(pr2, "db-schema") {
		t.Errorf("PR for api-endpoints should have base containing db-schema, got: %s", pr2)
	}

	// frontend PR should target api-endpoints
	pr3 := runGH(t, dir, "pr", "view", branches[2], "--json", "baseRefName")
	if !strings.Contains(pr3, "api-endpoints") {
		t.Errorf("PR for frontend should have base containing api-endpoints, got: %s", pr3)
	}

	// Sync should confirm the stack is already up to date
	t.Log("Syncing — expecting everything is already up to date")
	runGit(t, dir, "checkout", branches[2])
	syncOut := runSDF(t, dir, "sync", "-y")
	t.Log(syncOut)

	if !strings.Contains(syncOut, "in sync") && !strings.Contains(syncOut, "Everything is in sync") {
		t.Errorf("expected 'in sync' message, got: %s", syncOut)
	}

	t.Logf("Full lifecycle complete. Created stack %q with %d PRs: %v", stackName, len(prNumbers), prNumbers)
}
