package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLSResultMarksWorktreeStacks(t *testing.T) {
	// Build the result struct directly to assert the field exists and serializes.
	r := LSResult{Stacks: []LSStack{{Name: "feat", Nodes: 1, Status: "active", Worktree: true}}}
	data, _ := json.Marshal(r)
	if !strings.Contains(string(data), `"worktree":true`) {
		t.Errorf("expected worktree flag in ls JSON, got %s", data)
	}
}
