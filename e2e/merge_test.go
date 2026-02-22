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
// Scenario:
//
//	main ← branch-A (PR#1) ← branch-B (PR#2)
//
// After merging PR#1:
//   - PR#2's base should be retargeted to main (BEFORE merge)
//   - PR#1 merged and branch-A deleted
//   - PR#2 should still be OPEN with base=main
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

	// --- Setup: Create a 2-branch stack with PRs ---
	t.Log("Setup: init stack with 2 branches")
	runSDF(t, dir, "init", "--base", "main", "--branch", "layer-a", stackName)
	writeCommit(t, dir, prefix+"-a.txt", "layer A content\n", "feat: add layer A")

	runSDF(t, dir, "branch", "layer-b")
	writeCommit(t, dir, prefix+"-b.txt", "layer B content\n", "feat: add layer B")

	branchA := stackName + "/layer-a"
	branchB := stackName + "/layer-b"

	// Create PRs
	runGit(t, dir, "checkout", branchA)
	prAOut := runSDF(t, dir, "pr", "--json")
	var prA struct {
		Number int `json:"number"`
	}
	json.Unmarshal([]byte(prAOut), &prA)
	t.Logf("PR#%d created for %s", prA.Number, branchA)

	runGit(t, dir, "checkout", branchB)
	prBOut := runSDF(t, dir, "pr", "--json")
	var prB struct {
		Number int `json:"number"`
	}
	json.Unmarshal([]byte(prBOut), &prB)
	t.Logf("PR#%d created for %s", prB.Number, branchB)

	// Verify: 2 nodes with PRs, both open
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/layer-a", HasPR: true, Status: "open"},
		{BranchSuffix: "/layer-b", HasPR: true, Status: "open"},
	})

	// Verify PR#2 base is branch-A
	pr2Info := runGH(t, dir, "pr", "view", fmt.Sprint(prB.Number), "--json", "baseRefName,state")
	t.Logf("PR#%d before merge: %s", prB.Number, pr2Info)
	if !strings.Contains(pr2Info, "layer-a") {
		t.Fatalf("expected PR#%d base to contain 'layer-a', got: %s", prB.Number, pr2Info)
	}

	// --- Critical test: sdf merge ---
	t.Log("Merging head PR via sdf merge")
	runGit(t, dir, "checkout", branchB) // must be on a stack branch
	mergeOut := runSDF(t, dir, "merge", "-y")
	t.Log(mergeOut)

	// --- Verify: PR#2 is still OPEN with base=main ---
	t.Log("Verifying PR#2 state after merge")
	pr2After := runGH(t, dir, "pr", "view", fmt.Sprint(prB.Number), "--json", "baseRefName,state")
	t.Logf("PR#%d after merge: %s", prB.Number, pr2After)

	var pr2State struct {
		BaseRefName string `json:"baseRefName"`
		State       string `json:"state"`
	}
	if err := json.Unmarshal([]byte(pr2After), &pr2State); err != nil {
		t.Fatalf("cannot parse PR state: %v", err)
	}

	// THE CRITICAL ASSERTION: PR#2 must still be open
	if pr2State.State != "OPEN" {
		t.Errorf("PR#%d should be OPEN after merge, got %s — retarget-before-merge ordering may be broken",
			prB.Number, pr2State.State)
	}

	// PR#2's base should now be main
	if pr2State.BaseRefName != "main" {
		t.Errorf("PR#%d base should be 'main' after merge, got %q",
			prB.Number, pr2State.BaseRefName)
	}

	// Verify PR#1 is merged
	pr1After := runGH(t, dir, "pr", "view", fmt.Sprint(prA.Number), "--json", "state")
	if !strings.Contains(pr1After, "MERGED") {
		t.Errorf("PR#%d should be MERGED, got: %s", prA.Number, pr1After)
	}

	// Verify: stack reflects merge — first node merged, second still open
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/layer-a", HasPR: true, Status: "merged"},
		{BranchSuffix: "/layer-b", HasPR: true, Status: "open"},
	})

	t.Log("Merge ordering test passed — downstream PR survived the merge")
}

// TestE2E_MergeThenSync verifies that after merging the head PR,
// sdf sync correctly rebases remaining branches onto the new base.
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

	// Create a 3-branch stack
	runSDF(t, dir, "init", "--base", "main", "--branch", "step-1", stackName)
	writeCommit(t, dir, prefix+"-1.txt", "step 1\n", "feat: step 1")

	runSDF(t, dir, "branch", "step-2")
	writeCommit(t, dir, prefix+"-2.txt", "step 2\n", "feat: step 2")

	runSDF(t, dir, "branch", "step-3")
	writeCommit(t, dir, prefix+"-3.txt", "step 3\n", "feat: step 3")

	// Create PRs for all 3
	for _, br := range []string{stackName + "/step-1", stackName + "/step-2", stackName + "/step-3"} {
		runGit(t, dir, "checkout", br)
		runSDF(t, dir, "pr", "--json")
	}

	// Verify: 3 nodes with PRs, all open
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/step-1", HasPR: true, Status: "open"},
		{BranchSuffix: "/step-2", HasPR: true, Status: "open"},
		{BranchSuffix: "/step-3", HasPR: true, Status: "open"},
	})

	// Merge head PR
	t.Log("Merging step-1...")
	runGit(t, dir, "checkout", stackName+"/step-3")
	mergeOut := runSDF(t, dir, "merge", "-y")
	t.Log(mergeOut)

	// The merge command runs sync automatically. Verify the remaining branches
	// were rebased correctly by checking that step-2 and step-3 are still valid.
	t.Log("Verifying remaining PRs are open")

	step2 := stackName + "/step-2"
	step3 := stackName + "/step-3"

	for _, br := range []string{step2, step3} {
		info := runGH(t, dir, "pr", "view", br, "--json", "state,baseRefName")
		if !strings.Contains(info, "OPEN") {
			t.Errorf("PR for %s should be OPEN, got: %s", br, info)
		}
	}

	// Step-2 should now have base=main (since step-1 was merged)
	step2Info := runGH(t, dir, "pr", "view", step2, "--json", "baseRefName")
	if !strings.Contains(step2Info, "main") {
		t.Errorf("after merge, step-2 base should be main, got: %s", step2Info)
	}

	// Verify: stack reflects merge — first node merged, others still open
	assertStack(t, dir, stackName, "main", []nodeExpectation{
		{BranchSuffix: "/step-1", HasPR: true, Status: "merged"},
		{BranchSuffix: "/step-2", HasPR: true, Status: "open"},
		{BranchSuffix: "/step-3", HasPR: true, Status: "open"},
	})

	t.Log("Merge-then-sync test passed")
}
