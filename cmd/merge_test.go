package cmd

import (
	"encoding/json"
	"errors"
	"strings"
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

func TestFormatMergeError_BranchProtectionSuggestsAuto(t *testing.T) {
	err := errors.New("Pull request #91 is not mergeable: the base branch policy prohibits the merge")
	msg := formatMergeError(err, false, "main")
	if !strings.Contains(msg, "sdf merge --auto") {
		t.Fatalf("expected auto-merge guidance, got: %s", msg)
	}
}

func TestFormatMergeError_AuthHint(t *testing.T) {
	err := errors.New("authentication required")
	msg := formatMergeError(err, false, "main")
	if !strings.Contains(msg, "gh auth login") {
		t.Fatalf("expected auth guidance, got: %s", msg)
	}
}

func TestFormatMergeError_BranchProtectionWithAutoAlready(t *testing.T) {
	err := errors.New("Pull request #91 is not mergeable: the base branch policy prohibits the merge")
	msg := formatMergeError(err, true, "main")
	// When --auto was already used, don't suggest it again.
	if strings.Contains(msg, "sdf merge --auto") {
		t.Fatalf("should not suggest --auto when already used, got: %s", msg)
	}
	if !strings.Contains(msg, "merge blocked by branch protection") {
		t.Fatalf("expected branch protection message, got: %s", msg)
	}
}

func TestFormatMergeError_NetworkError(t *testing.T) {
	err := errors.New("connection refused")
	msg := formatMergeError(err, false, "main")
	if !strings.Contains(msg, "network error") {
		t.Fatalf("expected network error guidance, got: %s", msg)
	}
}

func TestFormatMergeError_UnknownError(t *testing.T) {
	err := errors.New("something unexpected happened")
	msg := formatMergeError(err, false, "main")
	if !strings.Contains(msg, "merge failed:") {
		t.Fatalf("expected generic merge failed, got: %s", msg)
	}
	if strings.Contains(msg, "sdf merge --auto") || strings.Contains(msg, "gh auth login") {
		t.Fatalf("should not contain specific guidance for unknown error, got: %s", msg)
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

func TestMergeResultJSON(t *testing.T) {
	result := MergeResult{
		Stack:        "my-feature",
		MergedPR:     42,
		MergedBranch: "feat-a",
		Method:       "squash",
		Base:         "main",
		Retargeted:   43,
		Remaining:    2,
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var roundtrip MergeResult
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if roundtrip.MergedPR != 42 {
		t.Errorf("merged_pr = %d, want 42", roundtrip.MergedPR)
	}
	if roundtrip.Retargeted != 43 {
		t.Errorf("retargeted_pr = %d, want 43", roundtrip.Retargeted)
	}
	if roundtrip.Method != "squash" {
		t.Errorf("method = %q, want %q", roundtrip.Method, "squash")
	}
}

func TestMergeResultJSON_OmitsEmpty(t *testing.T) {
	result := MergeResult{
		Stack:        "my-feature",
		MergedPR:     42,
		MergedBranch: "feat-a",
		Method:       "squash",
		Base:         "main",
		Remaining:    0,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	s := string(data)
	if contains(s, "retargeted_pr") {
		t.Error("retargeted_pr should be omitted when zero")
	}
	if contains(s, "error") {
		t.Error("error should be omitted when empty")
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
