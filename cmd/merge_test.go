package cmd

import (
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
)

func TestFindHeadPR_FirstOpen(t *testing.T) {
	s := &stack.Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "branchA", PR: 10, Status: "open"},
			{Branch: "branchB", PR: 11, Status: "open"},
		},
	}

	node, err := findHeadPR(s)
	if err != nil {
		t.Fatal(err)
	}
	if node.Branch != "branchA" {
		t.Errorf("expected branchA, got %s", node.Branch)
	}
	if node.PR != 10 {
		t.Errorf("expected PR 10, got %d", node.PR)
	}
}

func TestFindHeadPR_SkipsMerged(t *testing.T) {
	s := &stack.Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "branchA", PR: 10, Status: "merged"},
			{Branch: "branchB", PR: 11, Status: "merged"},
			{Branch: "branchC", PR: 12, Status: "open"},
		},
	}

	node, err := findHeadPR(s)
	if err != nil {
		t.Fatal(err)
	}
	if node.Branch != "branchC" {
		t.Errorf("expected branchC (first open), got %s", node.Branch)
	}
	if node.PR != 12 {
		t.Errorf("expected PR 12, got %d", node.PR)
	}
}

func TestFindHeadPR_AllMerged(t *testing.T) {
	s := &stack.Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "branchA", PR: 10, Status: "merged"},
			{Branch: "branchB", PR: 11, Status: "merged"},
		},
	}

	_, err := findHeadPR(s)
	if err == nil {
		t.Fatal("expected error when all PRs are merged")
	}
}

func TestFindHeadPR_NoPR(t *testing.T) {
	s := &stack.Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "branchA", Status: "open"},
		},
	}

	_, err := findHeadPR(s)
	if err == nil {
		t.Fatal("expected error when head branch has no PR")
	}
}

func TestFindHeadPR_NoPRAfterMerged(t *testing.T) {
	s := &stack.Stack{
		StackID: "test",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "branchA", PR: 10, Status: "merged"},
			{Branch: "branchB", Status: "open"},
		},
	}

	_, err := findHeadPR(s)
	if err == nil {
		t.Fatal("expected error when first open branch has no PR")
	}
}

func TestCountOpen(t *testing.T) {
	tests := []struct {
		name  string
		nodes []stack.Node
		want  int
	}{
		{
			name:  "all open",
			nodes: []stack.Node{{Status: "open"}, {Status: "open"}, {Status: "open"}},
			want:  3,
		},
		{
			name:  "mixed",
			nodes: []stack.Node{{Status: "merged"}, {Status: "open"}, {Status: "merged"}},
			want:  1,
		},
		{
			name:  "all merged",
			nodes: []stack.Node{{Status: "merged"}, {Status: "merged"}},
			want:  0,
		},
		{
			name:  "empty",
			nodes: nil,
			want:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &stack.Stack{Nodes: tc.nodes}
			got := countOpen(s)
			if got != tc.want {
				t.Errorf("countOpen() = %d, want %d", got, tc.want)
			}
		})
	}
}
