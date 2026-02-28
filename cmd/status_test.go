package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStatusResultJSON(t *testing.T) {
	result := StatusResult{
		Stack:         "my-feature",
		Base:          "main",
		CurrentBranch: "feat-a",
		Nodes: []StatusNodeResult{
			{Branch: "feat-a", PR: 42, Status: "open", SyncState: "in_sync", CommitsAhead: 3, IsCurrent: true},
			{Branch: "feat-b", PR: 43, Status: "open", SyncState: "needs_sync", CommitsAhead: 1},
		},
		NeedsSync:     []string{"feat-b"},
		DriftWarnings: []string{"PR #44 base changed"},
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var roundtrip StatusResult
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if roundtrip.Stack != "my-feature" {
		t.Errorf("stack = %q, want %q", roundtrip.Stack, "my-feature")
	}
	if len(roundtrip.Nodes) != 2 {
		t.Fatalf("nodes len = %d, want 2", len(roundtrip.Nodes))
	}
	if roundtrip.Nodes[0].CommitsAhead != 3 {
		t.Errorf("commits_ahead = %d, want 3", roundtrip.Nodes[0].CommitsAhead)
	}
	if !roundtrip.Nodes[0].IsCurrent {
		t.Error("is_current should be true for feat-a")
	}
	if roundtrip.Nodes[1].SyncState != "needs_sync" {
		t.Errorf("sync_state = %q, want %q", roundtrip.Nodes[1].SyncState, "needs_sync")
	}
}

func TestStatusResultJSON_EmptyNodes(t *testing.T) {
	result := StatusResult{
		Stack:         "empty",
		Base:          "main",
		CurrentBranch: "",
		Nodes:         []StatusNodeResult{},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	s := string(data)
	// Nodes should be [] not null
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}
	var roundtrip StatusResult
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if roundtrip.Nodes == nil {
		t.Error("nodes should be empty slice, not nil")
	}
	// omitempty fields should be absent
	for _, absent := range []string{"needs_sync", "drift_warnings"} {
		if strings.Contains(s, "\""+absent+"\"") {
			t.Errorf("expected %q to be omitted", absent)
		}
	}
}
