package cmd

import (
	"testing"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/testutil"
)

// TestReconcileSyncPRStates_FillsFromGitHub tests that reconcileSyncPRStates
// correctly fills PR numbers and states from GitHub when gh is available.
func TestReconcileSyncPRStates_FillsFromGitHub(t *testing.T) {
	syncTestRepo(t)

	dir := t.TempDir()
	fake := testutil.GHFakeBinWith(t, dir, map[string]string{
		"pr list": `[
			{"number":10,"headRefName":"branchA","state":"OPEN","baseRefName":"main","url":"https://github.com/test/pull/10"},
			{"number":11,"headRefName":"branchB","state":"OPEN","baseRefName":"branchA","url":"https://github.com/test/pull/11"},
			{"number":12,"headRefName":"branchC","state":"OPEN","baseRefName":"branchB","url":"https://github.com/test/pull/12"}
		]`,
	})
	testutil.SetBinary(t, &ghpkg.Binary, fake)

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Initially no PRs
	for _, node := range s.Nodes {
		if node.PR != 0 {
			t.Fatalf("expected no PRs initially, got PR #%d for %s", node.PR, node.Branch)
		}
	}

	reconcileSyncPRStates(s)

	// After reconciliation, PRs should be filled
	expected := map[string]int{
		"branchA": 10,
		"branchB": 11,
		"branchC": 12,
	}
	for _, node := range s.Nodes {
		want, ok := expected[node.Branch]
		if !ok {
			continue
		}
		if node.PR != want {
			t.Errorf("expected PR #%d for %s, got #%d", want, node.Branch, node.PR)
		}
	}

	// Verify gh was called
	log := testutil.ReadLog(t, dir, "gh")
	if len(log) != 1 {
		t.Fatalf("expected 1 gh invocation, got %d", len(log))
	}
}

// TestReconcileSyncPRStates_DetectsMerged verifies that reconciliation
// marks PRs as merged when GitHub reports them as MERGED.
func TestReconcileSyncPRStates_DetectsMerged(t *testing.T) {
	syncTestRepo(t)

	dir := t.TempDir()
	fake := testutil.GHFakeBinWith(t, dir, map[string]string{
		"pr list": `[
			{"number":10,"headRefName":"branchA","state":"MERGED","baseRefName":"main","url":""},
			{"number":11,"headRefName":"branchB","state":"OPEN","baseRefName":"branchA","url":""},
			{"number":12,"headRefName":"branchC","state":"OPEN","baseRefName":"branchB","url":""}
		]`,
	})
	testutil.SetBinary(t, &ghpkg.Binary, fake)

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	reconcileSyncPRStates(s)

	if s.Nodes[0].Status != "merged" {
		t.Errorf("expected branchA status 'merged', got %q", s.Nodes[0].Status)
	}
	if s.Nodes[1].Status == "merged" {
		t.Error("branchB should not be merged")
	}
}

// TestComputeSyncPlan_WithFakeGH_MergedHead tests that computeSyncPlan
// correctly generates update-pr-base actions when gh is available (via fake binary).
func TestComputeSyncPlan_WithFakeGH_MergedHead(t *testing.T) {
	syncTestRepo(t)

	dir := t.TempDir()
	fake := testutil.GHFakeBinWith(t, dir, map[string]string{
		"pr list": `[]`,
	})
	testutil.SetBinary(t, &ghpkg.Binary, fake)

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// branchA merged, branchB has a PR
	s.Nodes[0].Status = "merged"
	s.Nodes[1].PR = 42

	plan := computeSyncPlan(s, nil)

	// With gh available, there should be an update-pr-base action
	prActions := filterActions(plan, "update-pr-base")
	if len(prActions) != 1 {
		t.Fatalf("expected 1 update-pr-base action, got %d", len(prActions))
	}
	if prActions[0].pr != 42 {
		t.Errorf("expected PR #42, got #%d", prActions[0].pr)
	}
	if prActions[0].onto != "main" {
		t.Errorf("expected new base 'main', got %q", prActions[0].onto)
	}
}

// TestComputeSyncPlan_WithoutGH_NoPRActions tests that computeSyncPlan
// does not generate PR actions when gh is unavailable.
func TestComputeSyncPlan_WithoutGH_NoPRActions(t *testing.T) {
	syncTestRepo(t)

	// Point gh at a nonexistent binary
	testutil.SetBinary(t, &ghpkg.Binary, "/nonexistent/gh")

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	s.Nodes[0].Status = "merged"
	s.Nodes[1].PR = 42

	plan := computeSyncPlan(s, nil)

	// Without gh, there should be no PR actions
	prActions := filterActions(plan, "update-pr-base")
	if len(prActions) != 0 {
		t.Errorf("expected no update-pr-base actions without gh, got %d", len(prActions))
	}
}

// TestReconcileSyncPRStates_GHError verifies graceful handling when gh fails.
func TestReconcileSyncPRStates_GHError(t *testing.T) {
	syncTestRepo(t)

	dir := t.TempDir()
	fake := testutil.FakeBinFail(t, dir, "gh", "authentication required")
	testutil.SetBinary(t, &ghpkg.Binary, fake)

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Should not panic, should just print a warning
	reconcileSyncPRStates(s)

	// PRs should remain unchanged
	for _, node := range s.Nodes {
		if node.PR != 0 {
			t.Errorf("expected no PRs after gh error, got PR #%d for %s", node.PR, node.Branch)
		}
	}
}
