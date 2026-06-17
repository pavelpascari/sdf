package stack

import "testing"

func TestReconcile_NoChange(t *testing.T) {
	local := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "open"},
			{Branch: "branchB", PR: 11, Status: "open"},
			{Branch: "branchC", PR: 12, Status: "open"},
		},
	}
	discovered := DiscoveredStack{
		Base: "main",
		Chains: []PRRecord{
			{Number: 10, HeadRefName: "branchA", BaseRefName: "main", Status: "open"},
			{Number: 11, HeadRefName: "branchB", BaseRefName: "branchA", Status: "open"},
			{Number: 12, HeadRefName: "branchC", BaseRefName: "branchB", Status: "open"},
		},
	}

	changes := Reconcile(local, discovered)
	if len(changes) != 0 {
		t.Errorf("expected 0 changes, got %d: %v", len(changes), changes)
	}
}

func TestReconcile_StatusUpdate(t *testing.T) {
	local := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "open"},
		},
	}
	discovered := DiscoveredStack{
		Base: "main",
		Chains: []PRRecord{
			{Number: 10, HeadRefName: "branchA", BaseRefName: "main", Status: "merged"},
		},
	}

	changes := Reconcile(local, discovered)
	assertHasChange(t, changes, "status", "branchA", false)
}

func TestReconcile_PRNumberFill(t *testing.T) {
	local := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 0, Status: "open"},
		},
	}
	discovered := DiscoveredStack{
		Base: "main",
		Chains: []PRRecord{
			{Number: 10, HeadRefName: "branchA", BaseRefName: "main", Status: "open"},
		},
	}

	changes := Reconcile(local, discovered)
	assertHasChange(t, changes, "pr-number", "branchA", false)
}

func TestReconcile_Append(t *testing.T) {
	local := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "open"},
			{Branch: "branchB", PR: 11, Status: "open"},
		},
	}
	discovered := DiscoveredStack{
		Base: "main",
		Chains: []PRRecord{
			{Number: 10, HeadRefName: "branchA", BaseRefName: "main", Status: "open"},
			{Number: 11, HeadRefName: "branchB", BaseRefName: "branchA", Status: "open"},
			{Number: 12, HeadRefName: "branchC", BaseRefName: "branchB", Status: "open"},
		},
	}

	changes := Reconcile(local, discovered)
	assertHasChange(t, changes, "append", "branchC", false)
}

func TestReconcile_Insert(t *testing.T) {
	local := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "open"},
			{Branch: "branchC", PR: 12, Status: "open"},
		},
	}
	discovered := DiscoveredStack{
		Base: "main",
		Chains: []PRRecord{
			{Number: 10, HeadRefName: "branchA", BaseRefName: "main", Status: "open"},
			{Number: 11, HeadRefName: "branchB", BaseRefName: "branchA", Status: "open"},
			{Number: 12, HeadRefName: "branchC", BaseRefName: "branchB", Status: "open"},
		},
	}

	changes := Reconcile(local, discovered)
	assertHasChange(t, changes, "insert", "branchB", true)
}

func TestReconcile_Remove(t *testing.T) {
	local := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "open"},
			{Branch: "branchB", PR: 11, Status: "open"},
			{Branch: "branchC", PR: 12, Status: "open"},
		},
	}
	discovered := DiscoveredStack{
		Base: "main",
		Chains: []PRRecord{
			{Number: 10, HeadRefName: "branchA", BaseRefName: "main", Status: "open"},
			{Number: 12, HeadRefName: "branchC", BaseRefName: "branchA", Status: "open"},
		},
	}

	changes := Reconcile(local, discovered)
	assertHasChange(t, changes, "remove", "branchB", true)
}

func TestReconcile_Reorder(t *testing.T) {
	local := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "open"},
			{Branch: "branchB", PR: 11, Status: "open"},
			{Branch: "branchC", PR: 12, Status: "open"},
		},
	}
	discovered := DiscoveredStack{
		Base: "main",
		Chains: []PRRecord{
			{Number: 10, HeadRefName: "branchA", BaseRefName: "main", Status: "open"},
			{Number: 12, HeadRefName: "branchC", BaseRefName: "branchA", Status: "open"},
			{Number: 11, HeadRefName: "branchB", BaseRefName: "branchC", Status: "open"},
		},
	}

	changes := Reconcile(local, discovered)
	assertHasChange(t, changes, "reorder", "branchC", true)
	assertHasChange(t, changes, "reorder", "branchB", true)
}

func TestReconcile_BaseChange(t *testing.T) {
	local := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "open"},
		},
	}
	discovered := DiscoveredStack{
		Base: "develop",
		Chains: []PRRecord{
			{Number: 10, HeadRefName: "branchA", BaseRefName: "develop", Status: "open"},
		},
	}

	changes := Reconcile(local, discovered)
	assertHasChange(t, changes, "base-change", "", true)
}

func TestReconcile_Mixed(t *testing.T) {
	local := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "open"},
			{Branch: "branchB", PR: 11, Status: "open"},
		},
	}
	discovered := DiscoveredStack{
		Base: "main",
		Chains: []PRRecord{
			{Number: 10, HeadRefName: "branchA", BaseRefName: "main", Status: "merged"},
			{Number: 11, HeadRefName: "branchB", BaseRefName: "branchA", Status: "open"},
			{Number: 12, HeadRefName: "branchC", BaseRefName: "branchB", Status: "open"},
		},
	}

	changes := Reconcile(local, discovered)
	assertHasChange(t, changes, "status", "branchA", false)
	assertHasChange(t, changes, "append", "branchC", false)
}

func TestReconcile_EmptyLocal(t *testing.T) {
	local := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes:   []Node{},
	}
	discovered := DiscoveredStack{
		Base: "main",
		Chains: []PRRecord{
			{Number: 10, HeadRefName: "branchA", BaseRefName: "main", Status: "open"},
			{Number: 11, HeadRefName: "branchB", BaseRefName: "branchA", Status: "open"},
			{Number: 12, HeadRefName: "branchC", BaseRefName: "branchB", Status: "open"},
		},
	}

	changes := Reconcile(local, discovered)
	appendCount := 0
	for _, c := range changes {
		if c.Kind == "append" {
			appendCount++
		}
	}
	if appendCount != 3 {
		t.Errorf("expected 3 appends, got %d; changes: %v", appendCount, changes)
	}
}

func TestApplyChanges_RebuildsNodes(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "open", BaseTip: "abc123", NavHash: "hash1"},
			{Branch: "branchB", PR: 11, Status: "open", BaseTip: "def456", NavHash: "hash2"},
		},
	}
	discovered := DiscoveredStack{
		Base: "main",
		Chains: []PRRecord{
			{Number: 10, HeadRefName: "branchA", BaseRefName: "main", Status: "merged"},
			{Number: 11, HeadRefName: "branchB", BaseRefName: "branchA", Status: "open"},
			{Number: 12, HeadRefName: "branchC", BaseRefName: "branchB", Status: "open"},
		},
	}

	changes := Reconcile(s, discovered)
	ApplyChanges(s, discovered, changes)

	if len(s.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(s.Nodes))
	}

	// branchA: preserved local fields, updated status
	if s.Nodes[0].Branch != "branchA" {
		t.Errorf("node 0: expected branchA, got %s", s.Nodes[0].Branch)
	}
	if s.Nodes[0].Status != "merged" {
		t.Errorf("node 0: expected merged, got %s", s.Nodes[0].Status)
	}
	if s.Nodes[0].BaseTip != "abc123" {
		t.Errorf("node 0: expected preserved BaseTip abc123, got %s", s.Nodes[0].BaseTip)
	}
	if s.Nodes[0].NavHash != "hash1" {
		t.Errorf("node 0: expected preserved NavHash hash1, got %s", s.Nodes[0].NavHash)
	}

	// branchC: new node
	if s.Nodes[2].Branch != "branchC" {
		t.Errorf("node 2: expected branchC, got %s", s.Nodes[2].Branch)
	}
	if s.Nodes[2].PR != 12 {
		t.Errorf("node 2: expected PR 12, got %d", s.Nodes[2].PR)
	}
	if s.Nodes[2].Status != "open" {
		t.Errorf("node 2: expected open, got %s", s.Nodes[2].Status)
	}
}

func TestApplyChanges_BaseChange(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "open"},
		},
	}
	discovered := DiscoveredStack{
		Base: "develop",
		Chains: []PRRecord{
			{Number: 10, HeadRefName: "branchA", BaseRefName: "develop", Status: "open"},
		},
	}

	changes := Reconcile(s, discovered)
	ApplyChanges(s, discovered, changes)

	if s.Base != "develop" {
		t.Errorf("expected base develop, got %s", s.Base)
	}
}

// --- ReconcileFromPRs tests ---

func TestReconcileFromPRs_NoChange(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "open"},
			{Branch: "branchB", PR: 11, Status: "open"},
		},
	}
	prs := []PRState{
		{Number: 10, HeadRefName: "branchA", BaseRefName: "main", State: "OPEN"},
		{Number: 11, HeadRefName: "branchB", BaseRefName: "branchA", State: "OPEN"},
	}

	changes := ReconcileFromPRs(s, prs)
	if len(changes) != 0 {
		t.Errorf("expected 0 changes, got %d: %v", len(changes), changes)
	}
}

func TestReconcileFromPRs_StatusMerged(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "open"},
		},
	}
	prs := []PRState{
		{Number: 10, HeadRefName: "branchA", BaseRefName: "main", State: "MERGED"},
	}

	changes := ReconcileFromPRs(s, prs)
	assertHasChange(t, changes, "status", "branchA", false)
	if changes[0].NewStatus != "merged" {
		t.Errorf("expected NewStatus=merged, got %s", changes[0].NewStatus)
	}
}

func TestReconcileFromPRs_StatusClosed(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "open"},
		},
	}
	prs := []PRState{
		{Number: 10, HeadRefName: "branchA", BaseRefName: "main", State: "CLOSED"},
	}

	changes := ReconcileFromPRs(s, prs)
	assertHasChange(t, changes, "status", "branchA", false)
	if changes[0].NewStatus != "closed" {
		t.Errorf("expected NewStatus=closed, got %s", changes[0].NewStatus)
	}
}

func TestReconcileFromPRs_PRNumberFill(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 0, Status: "open"},
		},
	}
	prs := []PRState{
		{Number: 10, HeadRefName: "branchA", BaseRefName: "main", State: "OPEN"},
	}

	changes := ReconcileFromPRs(s, prs)
	assertHasChange(t, changes, "pr-number", "branchA", false)
	if changes[0].NewPR != 10 {
		t.Errorf("expected NewPR=10, got %d", changes[0].NewPR)
	}
}

func TestReconcileFromPRs_BaseMismatch(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "open"},
			{Branch: "branchB", PR: 11, Status: "open"},
		},
	}
	prs := []PRState{
		{Number: 10, HeadRefName: "branchA", BaseRefName: "main", State: "OPEN"},
		// branchB's base was retargeted to main on GitHub (expected: branchA)
		{Number: 11, HeadRefName: "branchB", BaseRefName: "main", State: "OPEN"},
	}

	changes := ReconcileFromPRs(s, prs)
	assertHasChange(t, changes, "base-mismatch", "branchB", true)
}

func TestReconcileFromPRs_PRMissing(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "open"},
			{Branch: "branchB", PR: 11, Status: "open"},
		},
	}
	// Only branchA returned — branchB's PR is missing
	prs := []PRState{
		{Number: 10, HeadRefName: "branchA", BaseRefName: "main", State: "OPEN"},
	}

	changes := ReconcileFromPRs(s, prs)
	assertHasChange(t, changes, "pr-missing", "branchB", true)
}

func TestReconcileFromPRs_Mixed(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "open"},
			{Branch: "branchB", PR: 11, Status: "open"},
			{Branch: "branchC", PR: 12, Status: "open"},
		},
	}
	prs := []PRState{
		{Number: 10, HeadRefName: "branchA", BaseRefName: "main", State: "MERGED"},
		// branchB retargeted to main (was branchA)
		{Number: 11, HeadRefName: "branchB", BaseRefName: "main", State: "OPEN"},
		{Number: 12, HeadRefName: "branchC", BaseRefName: "branchB", State: "OPEN"},
	}

	changes := ReconcileFromPRs(s, prs)
	assertHasChange(t, changes, "status", "branchA", false)       // merged
	assertHasChange(t, changes, "base-mismatch", "branchB", true) // retargeted
}

func TestReconcileFromPRs_DetectsNewChildPR(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "open"},
			{Branch: "branchB", PR: 11, Status: "open"},
		},
	}
	prs := []PRState{
		{Number: 10, HeadRefName: "branchA", BaseRefName: "main", State: "OPEN"},
		{Number: 11, HeadRefName: "branchB", BaseRefName: "branchA", State: "OPEN"},
		{Number: 12, HeadRefName: "branchC", BaseRefName: "branchB", State: "OPEN"},
		{Number: 99, HeadRefName: "unrelated", BaseRefName: "main", State: "OPEN"},
	}

	changes := ReconcileFromPRs(s, prs)
	assertHasChange(t, changes, "new-child-pr", "branchC", true)
}

func TestApplyRoutineChange_Status(t *testing.T) {
	s := &Stack{
		Nodes: []Node{{Branch: "branchA", PR: 10, Status: "open"}},
	}
	ApplyRoutineChange(s, ReconcileChange{Kind: "status", Branch: "branchA", NewStatus: "merged"})
	if s.Nodes[0].Status != "merged" {
		t.Errorf("expected merged, got %s", s.Nodes[0].Status)
	}
}

func TestApplyRoutineChange_PRNumber(t *testing.T) {
	s := &Stack{
		Nodes: []Node{{Branch: "branchA", PR: 0, Status: "open"}},
	}
	ApplyRoutineChange(s, ReconcileChange{Kind: "pr-number", Branch: "branchA", NewPR: 42})
	if s.Nodes[0].PR != 42 {
		t.Errorf("expected PR 42, got %d", s.Nodes[0].PR)
	}
}

func TestReconcile_MergedNodeNotRemoved(t *testing.T) {
	// Merged nodes won't appear in open-PR discovery — they should
	// NOT be flagged as "remove".
	local := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "merged"},
			{Branch: "branchB", PR: 11, Status: "merged"},
			{Branch: "branchC", PR: 12, Status: "open"},
		},
	}
	discovered := DiscoveredStack{
		Base: "main",
		Chains: []PRRecord{
			{Number: 12, HeadRefName: "branchC", BaseRefName: "main", Status: "open"},
		},
	}

	changes := Reconcile(local, discovered)
	for _, c := range changes {
		if c.Kind == "remove" {
			t.Errorf("merged node should not be removed: %v", c)
		}
	}
}

func TestApplyChanges_PreservesMergedNodes(t *testing.T) {
	// When discovery only returns open PRs, merged nodes should be
	// preserved at the front of the node list.
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "branchA", PR: 10, Status: "merged", BaseTip: "aaa"},
			{Branch: "branchB", PR: 11, Status: "merged", BaseTip: "bbb"},
			{Branch: "branchC", PR: 12, Status: "open", BaseTip: "ccc"},
			{Branch: "branchD", PR: 13, Status: "open", BaseTip: "ddd"},
		},
	}
	discovered := DiscoveredStack{
		Base: "main",
		Chains: []PRRecord{
			{Number: 12, HeadRefName: "branchC", BaseRefName: "main", Status: "open"},
			{Number: 13, HeadRefName: "branchD", BaseRefName: "branchC", Status: "open"},
		},
	}

	changes := Reconcile(s, discovered)
	ApplyChanges(s, discovered, changes)

	if len(s.Nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(s.Nodes))
	}

	expected := []struct {
		branch string
		status string
	}{
		{"branchA", "merged"},
		{"branchB", "merged"},
		{"branchC", "open"},
		{"branchD", "open"},
	}
	for i, want := range expected {
		if s.Nodes[i].Branch != want.branch || s.Nodes[i].Status != want.status {
			t.Errorf("node %d: expected %s (%s), got %s (%s)",
				i, want.branch, want.status, s.Nodes[i].Branch, s.Nodes[i].Status)
		}
	}

	// Verify local-only fields preserved for merged nodes
	if s.Nodes[0].BaseTip != "aaa" {
		t.Errorf("merged node BaseTip not preserved: got %q", s.Nodes[0].BaseTip)
	}
}

// assertHasChange verifies that changes contains a change with the given kind, branch, and notable flag.
func assertHasChange(t *testing.T, changes []ReconcileChange, kind, branch string, notable bool) {
	t.Helper()
	for _, c := range changes {
		if c.Kind == kind && (branch == "" || c.Branch == branch) && c.Notable == notable {
			return
		}
	}
	t.Errorf("expected change kind=%q branch=%q notable=%v not found in %v", kind, branch, notable, changes)
}

// TestApplyChangesPreservesWorktreeMode verifies that ApplyChanges does not
// clobber the Stack.Worktree flag or any Node.WorktreePath values that were
// already stored locally. New nodes from discovery should have empty WorktreePath.
func TestApplyChangesPreservesWorktreeMode(t *testing.T) {
	local := &Stack{
		StackID:  "feat",
		Base:     "main",
		Worktree: true,
		Nodes: []Node{
			{Branch: "feat/a", PR: 10, Status: "open", WorktreePath: "/tmp/worktrees/feat-a"},
			{Branch: "feat/b", PR: 11, Status: "open", WorktreePath: "/tmp/worktrees/feat-b"},
		},
	}

	// Discovered stack reflects a status update on feat/a and a brand-new branch feat/c.
	discovered := DiscoveredStack{
		Base: "main",
		Chains: []PRRecord{
			{Number: 10, HeadRefName: "feat/a", BaseRefName: "main", Status: "merged"},
			{Number: 11, HeadRefName: "feat/b", BaseRefName: "feat/a", Status: "open"},
			{Number: 12, HeadRefName: "feat/c", BaseRefName: "feat/b", Status: "open"},
		},
	}

	changes := Reconcile(local, discovered)
	ApplyChanges(local, discovered, changes)

	// Stack-level Worktree flag must survive.
	if !local.Worktree {
		t.Errorf("Stack.Worktree flag was cleared by ApplyChanges")
	}

	// Helper: find a node by branch.
	findNode := func(branch string) *Node {
		for i := range local.Nodes {
			if local.Nodes[i].Branch == branch {
				return &local.Nodes[i]
			}
		}
		return nil
	}

	// Existing nodes must keep their WorktreePath.
	if n := findNode("feat/a"); n == nil {
		t.Error("feat/a node missing after ApplyChanges")
	} else if n.WorktreePath != "/tmp/worktrees/feat-a" {
		t.Errorf("feat/a WorktreePath changed: got %q, want %q", n.WorktreePath, "/tmp/worktrees/feat-a")
	}

	if n := findNode("feat/b"); n == nil {
		t.Error("feat/b node missing after ApplyChanges")
	} else if n.WorktreePath != "/tmp/worktrees/feat-b" {
		t.Errorf("feat/b WorktreePath changed: got %q, want %q", n.WorktreePath, "/tmp/worktrees/feat-b")
	}

	// New node from discovery must have an empty WorktreePath.
	if n := findNode("feat/c"); n == nil {
		t.Error("feat/c node not created by ApplyChanges")
	} else if n.WorktreePath != "" {
		t.Errorf("feat/c new node has unexpected WorktreePath %q, want empty", n.WorktreePath)
	}
}
