package stack

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"testing"
)

// --- Property: Stack JSON round-trips cleanly ---

func TestProperty_StackJSONRoundTrip(t *testing.T) {
	for trial := 0; trial < 50; trial++ {
		t.Run(fmt.Sprintf("trial-%d", trial), func(t *testing.T) {
			s := randomStack(trial)
			dir := t.TempDir()

			// Ensure the stacks directory exists
			if err := os.MkdirAll(dir+"/"+SDFDir+"/"+StacksDir, 0755); err != nil {
				t.Fatal(err)
			}

			if err := Save(dir, s); err != nil {
				t.Fatalf("Save failed: %v", err)
			}

			loaded, err := LoadStack(dir, s.StackID)
			if err != nil {
				t.Fatalf("LoadStack failed: %v", err)
			}

			// Marshal both to JSON for comparison (avoids pointer issues)
			origJSON, _ := json.MarshalIndent(s, "", "  ")
			loadedJSON, _ := json.MarshalIndent(loaded, "", "  ")

			if string(origJSON) != string(loadedJSON) {
				t.Errorf("round-trip mismatch:\n--- original ---\n%s\n--- loaded ---\n%s",
					string(origJSON), string(loadedJSON))
			}
		})
	}
}

// --- Property: Save is idempotent ---

func TestProperty_SaveIdempotent(t *testing.T) {
	for trial := 0; trial < 20; trial++ {
		t.Run(fmt.Sprintf("trial-%d", trial), func(t *testing.T) {
			s := randomStack(trial)
			dir := t.TempDir()
			os.MkdirAll(dir+"/"+SDFDir+"/"+StacksDir, 0755)

			if err := Save(dir, s); err != nil {
				t.Fatal(err)
			}
			first, _ := os.ReadFile(StackPath(dir, s.StackID))

			if err := Save(dir, s); err != nil {
				t.Fatal(err)
			}
			second, _ := os.ReadFile(StackPath(dir, s.StackID))

			if string(first) != string(second) {
				t.Error("Save is not idempotent — two saves produce different content")
			}
		})
	}
}

// --- Property: ParentBranch always skips merged nodes ---

func TestProperty_ParentBranchSkipsMerged(t *testing.T) {
	for trial := 0; trial < 50; trial++ {
		t.Run(fmt.Sprintf("trial-%d", trial), func(t *testing.T) {
			s := randomStack(trial)

			// Randomly mark some as merged
			for i := range s.Nodes {
				if rand.Float64() < 0.4 {
					s.Nodes[i].Status = "merged"
				}
			}

			for _, node := range s.Nodes {
				if node.Status == "merged" {
					continue
				}

				parent := s.ParentBranch(node.Branch)

				// Parent must either be:
				// 1. The stack base (no non-merged ancestors)
				// 2. A non-merged node that comes before this one
				if parent == s.Base {
					// Valid — all predecessors must be merged
					idx := s.NodeIndex(node.Branch)
					for j := 0; j < idx; j++ {
						if s.Nodes[j].Status != "merged" {
							t.Errorf("ParentBranch(%s) = %s (base), but non-merged node %s exists at index %d",
								node.Branch, parent, s.Nodes[j].Branch, j)
						}
					}
					continue
				}

				// Parent is a node — must not be merged
				parentNode := s.FindNode(parent)
				if parentNode == nil {
					t.Errorf("ParentBranch(%s) = %s, which doesn't exist in stack", node.Branch, parent)
					continue
				}
				if parentNode.Status == "merged" {
					t.Errorf("ParentBranch(%s) = %s, which is merged", node.Branch, parent)
				}

				// Parent must come before this node
				parentIdx := s.NodeIndex(parent)
				childIdx := s.NodeIndex(node.Branch)
				if parentIdx >= childIdx {
					t.Errorf("ParentBranch(%s) = %s at index %d, but child is at index %d",
						node.Branch, parent, parentIdx, childIdx)
				}
			}
		})
	}
}

// --- Property: FindNode is consistent with NodeIndex ---

func TestProperty_FindNodeConsistentWithNodeIndex(t *testing.T) {
	for trial := 0; trial < 30; trial++ {
		t.Run(fmt.Sprintf("trial-%d", trial), func(t *testing.T) {
			s := randomStack(trial)

			for _, node := range s.Nodes {
				found := s.FindNode(node.Branch)
				if found == nil {
					t.Errorf("FindNode(%s) returned nil for existing branch", node.Branch)
					continue
				}
				if found.Branch != node.Branch {
					t.Errorf("FindNode(%s).Branch = %s", node.Branch, found.Branch)
				}

				idx := s.NodeIndex(node.Branch)
				if idx < 0 {
					t.Errorf("NodeIndex(%s) = -1 for existing branch", node.Branch)
					continue
				}
				if s.Nodes[idx].Branch != node.Branch {
					t.Errorf("Nodes[NodeIndex(%s)].Branch = %s", node.Branch, s.Nodes[idx].Branch)
				}
			}

			// Non-existent branch
			if s.FindNode("nonexistent-xyz") != nil {
				t.Error("FindNode returned non-nil for nonexistent branch")
			}
			if s.NodeIndex("nonexistent-xyz") != -1 {
				t.Error("NodeIndex returned >= 0 for nonexistent branch")
			}
		})
	}
}

// --- Property: ReconcileFromPRs routine changes don't include notable flag ---

func TestProperty_ReconcileRoutineChangesNotNotable(t *testing.T) {
	for trial := 0; trial < 30; trial++ {
		t.Run(fmt.Sprintf("trial-%d", trial), func(t *testing.T) {
			s := randomStack(trial)

			// Create PRStates that match the stack (no structural changes)
			var prs []PRState
			for _, node := range s.Nodes {
				prs = append(prs, PRState{
					Number:      node.PR,
					HeadRefName: node.Branch,
					BaseRefName: s.ParentBranch(node.Branch),
					State:       nodeStatusToGHState(node.Status),
				})
			}

			changes := ReconcileFromPRs(s, prs)

			// No changes should be notable when PR state matches stack state
			for _, c := range changes {
				if c.Notable {
					t.Errorf("unexpected notable change: %s (%s)", c.Detail, c.Kind)
				}
			}
		})
	}
}

// --- helpers ---

func randomStack(seed int) *Stack {
	r := rand.New(rand.NewSource(int64(seed)))
	n := 2 + r.Intn(6) // 2-7 branches

	s := &Stack{
		StackID: fmt.Sprintf("stack-%d", seed),
		Base:    "main",
		Nodes:   make([]Node, n),
	}

	statuses := []string{"open", "merged", "open", "open", "draft"}
	for i := 0; i < n; i++ {
		prNum := 0
		if r.Float64() < 0.7 {
			prNum = 10 + i
		}
		s.Nodes[i] = Node{
			Branch:  fmt.Sprintf("branch-%d", i),
			PR:      prNum,
			Status:  statuses[r.Intn(len(statuses))],
			BaseTip: fmt.Sprintf("abc%d%d", seed, i),
		}
	}

	return s
}

func nodeStatusToGHState(status string) string {
	switch status {
	case "merged":
		return "MERGED"
	case "closed":
		return "CLOSED"
	default:
		return "OPEN"
	}
}
