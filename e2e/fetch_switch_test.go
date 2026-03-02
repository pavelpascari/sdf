//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestE2E_FetchReconstructsStack verifies that sdf fetch can rebuild local
// .sdf state from an existing PR chain after local metadata is removed.
func TestE2E_FetchReconstructsStack(t *testing.T) {
	dir := e2eRepo(t)
	setupRecording(t)
	prefix := testPrefix()
	stackName := prefix
	baseBranch := prefix + "-base"

	t.Cleanup(func() {
		runGitMayFail(t, dir, "checkout", "main")
		cleanupPRs(t, dir, prefix)
		cleanupBranches(t, dir, prefix)
		os.RemoveAll(dir + "/.sdf")
		runGitMayFail(t, dir, "checkout", "main")
	})

	// Create an isolated base branch so fetch discovery can be scoped tightly.
	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "pull", "origin", "main")
	runGit(t, dir, "checkout", "-B", baseBranch, "main")
	runGit(t, dir, "push", "-u", "origin", baseBranch)
	runGit(t, dir, "checkout", "main")

	runSDF(t, dir, "new", "--base", baseBranch, "--branch", "data-model", stackName)
	writeCommit(t, dir, prefix+"-model.go", "package model\n\ntype User struct{ ID string }\n", "feat: add user model")

	runSDF(t, dir, "branch", "api")
	writeCommit(t, dir, prefix+"-api.go", "package api\n\nfunc GetUser() {}\n", "feat: add user api")

	branchModel := stackName + "/data-model"
	branchAPI := stackName + "/api"

	for _, br := range []string{branchModel, branchAPI} {
		runGit(t, dir, "checkout", br)
		out := runSDF(t, dir, "pr", "--json")
		var pr struct {
			Number int `json:"number"`
		}
		if err := json.Unmarshal([]byte(out), &pr); err != nil {
			t.Fatalf("cannot parse PR JSON for %s: %v\noutput: %s", br, err, out)
		}
		if pr.Number == 0 {
			t.Fatalf("PR number is 0 for %s", br)
		}
	}

	// Simulate local state loss, then recover with fetch.
	if err := os.RemoveAll(dir + "/.sdf"); err != nil {
		t.Fatalf("failed to remove .sdf: %v", err)
	}

	// GitHub search/index visibility can lag briefly after PR creation.
	// Retry fetch a few times to avoid flakes where no stack is detected yet.
	var fetchOut string
	for i := 0; i < 5; i++ {
		fetchOut = runSDF(t, dir, "fetch", "--base", baseBranch, "--stack", stackName, "--json")
		if strings.TrimSpace(fetchOut) != "" {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if strings.TrimSpace(fetchOut) == "" {
		prList := runGH(t, dir, "pr", "list", "--state", "open", "--json", "number,headRefName,baseRefName", "--limit", "200")
		t.Fatalf("fetch returned empty JSON output after retries; open PRs: %s", prList)
	}

	jsonStart := strings.Index(fetchOut, "{")
	if jsonStart < 0 {
		t.Fatalf("fetch output did not include JSON payload: %s", fetchOut)
	}
	fetchJSON := fetchOut[jsonStart:]

	var fetchResult struct {
		Stack  string `json:"stack"`
		Base   string `json:"base"`
		Action string `json:"action"`
		Nodes  []struct {
			Branch string `json:"branch"`
			PR     int    `json:"pr"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(fetchJSON), &fetchResult); err != nil {
		t.Fatalf("cannot parse fetch JSON: %v\noutput: %s", err, fetchOut)
	}
	if fetchResult.Stack != stackName {
		t.Fatalf("fetch stack: want %q, got %q", stackName, fetchResult.Stack)
	}
	if fetchResult.Base != baseBranch {
		t.Fatalf("fetch base: want %q, got %q", baseBranch, fetchResult.Base)
	}
	if fetchResult.Action != "registered" {
		t.Fatalf("fetch action: want %q, got %q", "registered", fetchResult.Action)
	}
	if len(fetchResult.Nodes) != 2 {
		t.Fatalf("fetch nodes: want 2, got %d", len(fetchResult.Nodes))
	}

	assertStack(t, dir, stackName, baseBranch, []nodeExpectation{
		{BranchSuffix: "/data-model", HasPR: true, Status: "open"},
		{BranchSuffix: "/api", HasPR: true, Status: "open"},
	})

	statusOut := runSDF(t, dir, "status", "--json")
	var status struct {
		Stack string `json:"stack"`
		Base  string `json:"base"`
		Nodes []struct {
			Branch string `json:"branch"`
			PR     int    `json:"pr"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(statusOut), &status); err != nil {
		t.Fatalf("cannot parse status JSON: %v\noutput: %s", err, statusOut)
	}
	if status.Stack != stackName {
		t.Fatalf("status stack: want %q, got %q", stackName, status.Stack)
	}
	if status.Base != baseBranch {
		t.Fatalf("status base: want %q, got %q", baseBranch, status.Base)
	}
	if len(status.Nodes) != 2 {
		t.Fatalf("status nodes: want 2, got %d", len(status.Nodes))
	}
}

// TestE2E_SwitchAndShorthand verifies navigation commands:
// - `sdf switch --json <branch>` reports stack position
// - `sdf <branch>` shorthand switches via root command dispatch
// - `sdf switch` lists known stack branches
func TestE2E_SwitchAndShorthand(t *testing.T) {
	dir := e2eRepo(t)
	setupRecording(t)
	prefix := testPrefix()
	stackName := prefix

	t.Cleanup(func() {
		runGitMayFail(t, dir, "checkout", "main")
		cleanupBranches(t, dir, prefix)
		os.RemoveAll(dir + "/.sdf")
		runGitMayFail(t, dir, "checkout", "main")
	})

	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "pull", "origin", "main")

	runSDF(t, dir, "new", "--base", "main", "--branch", "layer-one", stackName)
	writeCommit(t, dir, prefix+"-layer1.txt", "layer one\n", "feat: add first layer")

	runSDF(t, dir, "branch", "layer-two")
	writeCommit(t, dir, prefix+"-layer2.txt", "layer two\n", "feat: add second layer")

	branchOne := stackName + "/layer-one"
	branchTwo := stackName + "/layer-two"

	switchJSON := runSDF(t, dir, "switch", "--json", branchOne)
	var sw struct {
		Branch string `json:"branch"`
		Stack  string `json:"stack"`
		Layer  int    `json:"layer"`
		Total  int    `json:"total"`
	}
	if err := json.Unmarshal([]byte(switchJSON), &sw); err != nil {
		t.Fatalf("cannot parse switch JSON: %v\noutput: %s", err, switchJSON)
	}
	if sw.Branch != branchOne {
		t.Fatalf("switch branch: want %q, got %q", branchOne, sw.Branch)
	}
	if sw.Stack != stackName {
		t.Fatalf("switch stack: want %q, got %q", stackName, sw.Stack)
	}
	if sw.Layer != 1 || sw.Total != 2 {
		t.Fatalf("switch position: want layer 1/2, got %d/%d", sw.Layer, sw.Total)
	}

	shorthandOut := runSDF(t, dir, branchTwo)
	if !strings.Contains(shorthandOut, "Switched to") || !strings.Contains(shorthandOut, branchTwo) {
		t.Fatalf("unexpected shorthand switch output: %s", shorthandOut)
	}

	current := runGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if current != branchTwo {
		t.Fatalf("expected current branch %q, got %q", branchTwo, current)
	}

	listOut := runSDF(t, dir, "switch")
	if !strings.Contains(listOut, stackName) || !strings.Contains(listOut, branchOne) || !strings.Contains(listOut, branchTwo) {
		t.Fatalf("switch list output missing expected entries: %s", listOut)
	}
}
