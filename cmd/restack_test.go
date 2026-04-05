package cmd

import (
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
)

func TestReorderNodes_MoveMiddleToFirst(t *testing.T) {
	nodes := []stack.Node{
		{Branch: "a", Status: "open"},
		{Branch: "b", Status: "open"},
		{Branch: "c", Status: "open"},
		{Branch: "d", Status: "open"},
	}
	result := reorderNodes(nodes, "c", "a")
	expected := []string{"a", "c", "b", "d"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d nodes, got %d", len(expected), len(result))
	}
	for i, name := range expected {
		if result[i].Branch != name {
			t.Errorf("position %d: expected %s, got %s", i, name, result[i].Branch)
		}
	}
}

func TestReorderNodes_MoveToBase(t *testing.T) {
	nodes := []stack.Node{
		{Branch: "a", Status: "open"},
		{Branch: "b", Status: "open"},
		{Branch: "c", Status: "open"},
	}
	result := reorderNodes(nodes, "c", "")
	expected := []string{"c", "a", "b"}
	for i, name := range expected {
		if result[i].Branch != name {
			t.Errorf("position %d: expected %s, got %s", i, name, result[i].Branch)
		}
	}
}

func TestReorderNodes_MoveForward(t *testing.T) {
	nodes := []stack.Node{
		{Branch: "a", Status: "open"},
		{Branch: "b", Status: "open"},
		{Branch: "c", Status: "open"},
		{Branch: "d", Status: "open"},
	}
	result := reorderNodes(nodes, "a", "c")
	expected := []string{"b", "c", "a", "d"}
	for i, name := range expected {
		if result[i].Branch != name {
			t.Errorf("position %d: expected %s, got %s", i, name, result[i].Branch)
		}
	}
}

func TestReorderNodes_PreservesFields(t *testing.T) {
	nodes := []stack.Node{
		{Branch: "a", PR: 1, Status: "open", BaseTip: "aaa"},
		{Branch: "b", PR: 2, Status: "open", BaseTip: "bbb"},
		{Branch: "c", PR: 3, Status: "open", BaseTip: "ccc"},
	}
	result := reorderNodes(nodes, "c", "a")
	if result[1].Branch != "c" || result[1].PR != 3 || result[1].BaseTip != "ccc" {
		t.Errorf("moved node lost fields: %+v", result[1])
	}
}

func TestReorderNodes_AdjacentSwap(t *testing.T) {
	nodes := []stack.Node{
		{Branch: "a", Status: "open"},
		{Branch: "b", Status: "open"},
		{Branch: "c", Status: "open"},
	}
	result := reorderNodes(nodes, "b", "c")
	expected := []string{"a", "c", "b"}
	for i, name := range expected {
		if result[i].Branch != name {
			t.Errorf("position %d: expected %s, got %s", i, name, result[i].Branch)
		}
	}
}

func TestComputeRestackPlan_IdentifiesAffected(t *testing.T) {
	s := &stack.Stack{
		StackID: "test", Base: "main",
		Nodes: []stack.Node{
			{Branch: "a", Status: "open"},
			{Branch: "b", Status: "open"},
			{Branch: "c", Status: "open"},
			{Branch: "d", Status: "open"},
		},
	}
	newNodes := reorderNodes(s.Nodes, "c", "a")
	affected := computeRestackPlan(s, newNodes)
	if len(affected) != 3 {
		t.Fatalf("expected 3 affected branches, got %d", len(affected))
	}
	expects := map[string]string{"c": "a", "b": "c", "d": "b"}
	for _, a := range affected {
		want, ok := expects[a.Branch]
		if !ok {
			t.Errorf("unexpected affected branch: %s", a.Branch)
			continue
		}
		if a.NewParent != want {
			t.Errorf("branch %s: expected new parent %s, got %s", a.Branch, want, a.NewParent)
		}
	}
}

func TestComputeRestackPlan_NoOpWhenSamePosition(t *testing.T) {
	s := &stack.Stack{
		StackID: "test", Base: "main",
		Nodes: []stack.Node{
			{Branch: "a", Status: "open"},
			{Branch: "b", Status: "open"},
			{Branch: "c", Status: "open"},
		},
	}
	newNodes := reorderNodes(s.Nodes, "b", "a")
	affected := computeRestackPlan(s, newNodes)
	if len(affected) != 0 {
		t.Errorf("expected 0 affected branches for no-op, got %d", len(affected))
	}
}

func TestComputeRestackPlan_SkipsMergedNodes(t *testing.T) {
	s := &stack.Stack{
		StackID: "test", Base: "main",
		Nodes: []stack.Node{
			{Branch: "a", Status: "open"},
			{Branch: "b", Status: "merged"},
			{Branch: "c", Status: "open"},
			{Branch: "d", Status: "open"},
		},
	}
	newNodes := reorderNodes(s.Nodes, "d", "a")
	affected := computeRestackPlan(s, newNodes)
	for _, a := range affected {
		if a.Branch == "b" {
			t.Error("merged branch b should not be in affected list")
		}
	}
}
