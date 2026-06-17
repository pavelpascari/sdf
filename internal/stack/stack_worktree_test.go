// Package stack manages the .sdf/stacks/*.json files and stack topology.
package stack

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStackWorktreeFieldsRoundTrip(t *testing.T) {
	s := &Stack{
		StackID:  "feat",
		Base:     "main",
		Worktree: true,
		Nodes: []Node{
			{Branch: "feat/a", Status: "open", BaseTip: "abc", WorktreePath: "/tmp/wt/feat-a"},
		},
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"worktree":true`) {
		t.Errorf("expected worktree flag in JSON, got %s", data)
	}
	if !strings.Contains(string(data), `"worktree_path":"/tmp/wt/feat-a"`) {
		t.Errorf("expected worktree_path in JSON, got %s", data)
	}

	var back Stack
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !back.Worktree || back.Nodes[0].WorktreePath != "/tmp/wt/feat-a" {
		t.Errorf("round-trip lost worktree fields: %+v", back)
	}
}

func TestStackWorktreeOmittedWhenFalse(t *testing.T) {
	data, _ := json.Marshal(&Stack{StackID: "x", Base: "main", Nodes: []Node{{Branch: "x", Status: "open"}}})
	if strings.Contains(string(data), "worktree") {
		t.Errorf("worktree fields should be omitted when empty, got %s", data)
	}
}
