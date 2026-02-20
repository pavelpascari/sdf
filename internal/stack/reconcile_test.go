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
