package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
)

func TestStatusResultJSON(t *testing.T) {
	result := StatusResult{
		Stack:         "my-feature",
		Base:          "main",
		CurrentBranch: "feat-a",
		Nodes: []StatusNodeResult{
			{Branch: "feat-a", PR: 42, Status: "open", SyncState: "in_sync", CommitsAhead: 3, CommitLog: []string{"abc1234 add users table"}, IsCurrent: true, CIStatus: "pass", ReviewStatus: "approved", Mergeable: "mergeable"},
			{Branch: "feat-b", PR: 43, Status: "open", SyncState: "needs_sync", CommitsAhead: 1, CIStatus: "fail", ReviewStatus: "changes_requested", Mergeable: "conflicting"},
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
	if roundtrip.Nodes[0].CIStatus != "pass" {
		t.Errorf("ci_status = %q, want %q", roundtrip.Nodes[0].CIStatus, "pass")
	}
	if roundtrip.Nodes[1].CIStatus != "fail" {
		t.Errorf("ci_status = %q, want %q", roundtrip.Nodes[1].CIStatus, "fail")
	}
	if roundtrip.Nodes[0].ReviewStatus != "approved" {
		t.Errorf("review_status = %q, want %q", roundtrip.Nodes[0].ReviewStatus, "approved")
	}
	if roundtrip.Nodes[1].Mergeable != "conflicting" {
		t.Errorf("mergeable = %q, want %q", roundtrip.Nodes[1].Mergeable, "conflicting")
	}
	if len(roundtrip.Nodes[0].CommitLog) != 1 {
		t.Fatalf("commit_log len = %d, want 1", len(roundtrip.Nodes[0].CommitLog))
	}
}

func TestFormatMergeability(t *testing.T) {
	tests := []struct {
		name string
		nr   StatusNodeResult
		want string // substring to check (empty = expect empty result)
	}{
		{name: "no PR", nr: StatusNodeResult{Status: "open"}, want: ""},
		{name: "merged", nr: StatusNodeResult{PR: 1, Status: "merged"}, want: ""},
		{name: "draft", nr: StatusNodeResult{PR: 1, Status: "open", IsDraft: true}, want: "draft"},
		{name: "conflicting", nr: StatusNodeResult{PR: 1, Status: "open", Mergeable: "conflicting"}, want: "conflict"},
		{name: "CI pass + approved", nr: StatusNodeResult{PR: 1, Status: "open", CIStatus: "pass", ReviewStatus: "approved"}, want: "[CI:"},
		{name: "CI fail badge", nr: StatusNodeResult{PR: 1, Status: "open", CIStatus: "fail", ReviewStatus: "approved"}, want: "[CI:"},
		{name: "review changes requested", nr: StatusNodeResult{PR: 1, Status: "open", CIStatus: "pass", ReviewStatus: "changes_requested"}, want: "[R:"},
		{name: "pending CI badge", nr: StatusNodeResult{PR: 1, Status: "open", CIStatus: "pending"}, want: "[CI:"},
		{name: "review required badge", nr: StatusNodeResult{PR: 1, Status: "open", CIStatus: "pass", ReviewStatus: "review_required"}, want: "[R:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatMergeability(tt.nr)
			if tt.want == "" && got != "" {
				t.Errorf("expected empty, got %q", got)
			}
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Errorf("expected %q to contain %q", got, tt.want)
			}
		})
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

func TestStatusBaseDriftHint(t *testing.T) {
	dir := syncTestRepo(t)

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Record old main tip (same as branchA's BaseTip)
	oldMainTip := s.Nodes[0].BaseTip

	// Advance main
	git("checkout", "main")
	os.WriteFile(filepath.Join(dir, "drift.txt"), []byte("drift\n"), 0644)
	git("add", "drift.txt")
	git("commit", "-m", "unrelated on main")
	git("checkout", "branchC")

	currentMainTip := git("rev-parse", "main")
	if oldMainTip == currentMainTip {
		t.Fatal("test setup error: main should have advanced")
	}

	hint := detectBaseDrift(s, oldMainTip, currentMainTip)
	if hint == "" {
		t.Fatal("expected drift hint")
	}
	if !strings.Contains(hint, "--full") {
		t.Error("hint should suggest --full")
	}
}

func TestStatusFullShowsBaseDriftAsNeedsSync(t *testing.T) {
	dir := syncTestRepo(t)

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	// Advance main
	git("checkout", "main")
	os.WriteFile(filepath.Join(dir, "drift.txt"), []byte("drift\n"), 0644)
	git("add", "drift.txt")
	git("commit", "-m", "unrelated on main")
	git("checkout", "branchC")

	// With --full, the current main tip is used → branchA's BaseTip is stale
	currentMainTip := git("rev-parse", "main")
	if currentMainTip == s.Nodes[0].BaseTip {
		t.Fatal("test setup error: main tip should differ from branchA BaseTip")
	}

	// computeSyncPlan with fromHead=true should detect the drift
	opts := &syncOptions{fromHead: true}
	plan := computeSyncPlan(s, opts)
	rebases := filterActions(plan, "rebase")
	if len(rebases) == 0 {
		t.Error("expected rebases with --full when base drifted")
	}
}
