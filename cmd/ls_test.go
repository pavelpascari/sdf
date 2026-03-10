package cmd

import (
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
)

func TestSummarizeStackStatus(t *testing.T) {
	tests := []struct {
		name       string
		nodes      []stack.Node
		wantStatus string
		wantMerged int
	}{
		{
			name:       "empty",
			nodes:      nil,
			wantStatus: "active",
			wantMerged: 0,
		},
		{
			name:       "active",
			nodes:      []stack.Node{{Status: "open"}, {Status: "draft"}},
			wantStatus: "active",
			wantMerged: 0,
		},
		{
			name:       "fully merged",
			nodes:      []stack.Node{{Status: "merged"}, {Status: "closed"}},
			wantStatus: "completed",
			wantMerged: 2,
		},
		{
			name:       "partial",
			nodes:      []stack.Node{{Status: "merged"}, {Status: "open"}, {Status: "open"}},
			wantStatus: "partial",
			wantMerged: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &stack.Stack{Nodes: tc.nodes}
			status, merged := summarizeStackStatus(s)
			if status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", status, tc.wantStatus)
			}
			if merged != tc.wantMerged {
				t.Fatalf("merged = %d, want %d", merged, tc.wantMerged)
			}
		})
	}
}
