package stack

import "testing"

func TestParentBranch_SkipsClosedNode(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "a", Status: "open"},
			{Branch: "b", Status: "closed"},
			{Branch: "c", Status: "open"},
		},
	}
	parent := s.ParentBranch("c")
	if parent != "a" {
		t.Errorf("expected parent of c to be a (skipping closed b), got %s", parent)
	}
}

func TestParentBranch_SkipsConsecutiveClosedNodes(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "a", Status: "open"},
			{Branch: "b", Status: "closed"},
			{Branch: "c", Status: "closed"},
			{Branch: "d", Status: "open"},
		},
	}
	parent := s.ParentBranch("d")
	if parent != "a" {
		t.Errorf("expected parent of d to be a (skipping closed b,c), got %s", parent)
	}
}

func TestParentBranch_FirstNodeClosedReturnsBase(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "a", Status: "closed"},
			{Branch: "b", Status: "open"},
		},
	}
	parent := s.ParentBranch("b")
	if parent != "main" {
		t.Errorf("expected parent of b to be main (skipping closed a), got %s", parent)
	}
}

func TestParentBranch_SkipsMergedAndClosed(t *testing.T) {
	s := &Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []Node{
			{Branch: "a", Status: "open"},
			{Branch: "b", Status: "merged"},
			{Branch: "c", Status: "closed"},
			{Branch: "d", Status: "open"},
		},
	}
	parent := s.ParentBranch("d")
	if parent != "a" {
		t.Errorf("expected parent of d to be a (skipping merged b, closed c), got %s", parent)
	}
}
