package cmd

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/testutil"
)

// propertyTestRepo creates a git repo with N branches in a stack,
// each with a commit, for property-based testing of computeSyncPlan.
// Returns the repo dir and the stack.
func propertyTestRepo(t *testing.T, n int) (string, *stack.Stack) {
	t.Helper()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

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

	git("init", "-b", "main")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "Test")
	git("config", "commit.gpgsign", "false")

	os.MkdirAll(filepath.Join(dir, ".sdf", "context"), 0755)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0644)
	git("add", "README.md")
	git("commit", "-m", "initial")

	s := &stack.Stack{
		StackID: "prop-test",
		Base:    "main",
		Nodes:   make([]stack.Node, n),
	}

	prevTip := git("rev-parse", "HEAD")

	for i := 0; i < n; i++ {
		branch := fmt.Sprintf("branch-%d", i)
		git("checkout", "-b", branch)
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("file-%d.txt", i)), []byte(fmt.Sprintf("content-%d\n", i)), 0644)
		git("add", fmt.Sprintf("file-%d.txt", i))
		git("commit", "-m", fmt.Sprintf("commit on %s", branch))

		s.Nodes[i] = stack.Node{
			Branch:  branch,
			Status:  "open",
			BaseTip: prevTip,
		}
		prevTip = git("rev-parse", "HEAD")
	}

	if err := stack.Save(dir, s); err != nil {
		t.Fatal(err)
	}
	git("add", ".sdf")
	git("commit", "-m", "sdf: init stack")

	return dir, s
}

// --- Property: No rebase action targets a merged branch ---

func TestProperty_NoRebaseOnMergedBranch(t *testing.T) {
	// Disable gh so PR actions don't appear
	testutil.SetBinary(t, &ghpkg.Binary, "/nonexistent/gh")

	for trial := 0; trial < 20; trial++ {
		t.Run(fmt.Sprintf("trial-%d", trial), func(t *testing.T) {
			n := 3 + rand.Intn(5) // 3-7 branches
			_, s := propertyTestRepo(t, n)

			// Randomly mark some branches as merged
			for i := range s.Nodes {
				if rand.Float64() < 0.4 {
					s.Nodes[i].Status = "merged"
				}
			}

			plan := computeSyncPlan(s, nil)

			for _, a := range plan {
				if a.kind == "rebase" {
					node := s.FindNode(a.branch)
					if node != nil && node.Status == "merged" {
						t.Errorf("rebase action targets merged branch %s", a.branch)
					}
				}
				if a.kind == "push" {
					node := s.FindNode(a.branch)
					if node != nil && node.Status == "merged" {
						t.Errorf("push action targets merged branch %s", a.branch)
					}
				}
			}
		})
	}
}

// --- Property: Rebase order respects topology (parent before child) ---

func TestProperty_RebaseOrderRespectsTopology(t *testing.T) {
	testutil.SetBinary(t, &ghpkg.Binary, "/nonexistent/gh")

	for trial := 0; trial < 20; trial++ {
		t.Run(fmt.Sprintf("trial-%d", trial), func(t *testing.T) {
			n := 3 + rand.Intn(5)
			dir, s := propertyTestRepo(t, n)

			// Make main stale to trigger full cascade
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

			lastBranch := s.Nodes[len(s.Nodes)-1].Branch
			git("checkout", "main")
			os.WriteFile(filepath.Join(dir, "stale.txt"), []byte("stale\n"), 0644)
			git("add", "stale.txt")
			git("commit", "-m", "make main stale")
			git("checkout", lastBranch)

			plan := computeSyncPlan(s, nil)

			rebases := filterActions(plan, "rebase")

			// Verify order: if branch-i and branch-j are both rebased,
			// and i < j in the stack, then i must appear before j in the plan.
			rebaseOrder := make(map[string]int) // branch → position in rebases
			for idx, a := range rebases {
				rebaseOrder[a.branch] = idx
			}

			for i := 0; i < len(s.Nodes); i++ {
				for j := i + 1; j < len(s.Nodes); j++ {
					posI, hasI := rebaseOrder[s.Nodes[i].Branch]
					posJ, hasJ := rebaseOrder[s.Nodes[j].Branch]
					if hasI && hasJ && posI > posJ {
						t.Errorf("topology violation: %s (stack pos %d) rebased after %s (stack pos %d)",
							s.Nodes[i].Branch, i, s.Nodes[j].Branch, j)
					}
				}
			}
		})
	}
}

// --- Property: Every rebase has a corresponding push ---

func TestProperty_RebaseAlwaysPairedWithPush(t *testing.T) {
	testutil.SetBinary(t, &ghpkg.Binary, "/nonexistent/gh")

	for trial := 0; trial < 20; trial++ {
		t.Run(fmt.Sprintf("trial-%d", trial), func(t *testing.T) {
			n := 3 + rand.Intn(5)
			dir, s := propertyTestRepo(t, n)

			// Make some branches stale to generate rebases
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

			lastBranch := s.Nodes[len(s.Nodes)-1].Branch
			git("checkout", "main")
			os.WriteFile(filepath.Join(dir, "stale.txt"), []byte("stale\n"), 0644)
			git("add", "stale.txt")
			git("commit", "-m", "stale")
			git("checkout", lastBranch)

			// Randomly mark some as merged
			for i := range s.Nodes {
				if rand.Float64() < 0.3 {
					s.Nodes[i].Status = "merged"
				}
			}

			plan := computeSyncPlan(s, nil)

			rebasedBranches := make(map[string]bool)
			pushedBranches := make(map[string]bool)

			for _, a := range plan {
				if a.kind == "rebase" {
					rebasedBranches[a.branch] = true
				}
				if a.kind == "push" {
					pushedBranches[a.branch] = true
				}
			}

			for branch := range rebasedBranches {
				if !pushedBranches[branch] {
					t.Errorf("branch %s has rebase but no push", branch)
				}
			}
		})
	}
}

// --- Property: skip-merged only for merged nodes ---

func TestProperty_SkipMergedOnlyForMergedNodes(t *testing.T) {
	testutil.SetBinary(t, &ghpkg.Binary, "/nonexistent/gh")

	for trial := 0; trial < 20; trial++ {
		t.Run(fmt.Sprintf("trial-%d", trial), func(t *testing.T) {
			n := 3 + rand.Intn(5)
			_, s := propertyTestRepo(t, n)

			for i := range s.Nodes {
				if rand.Float64() < 0.5 {
					s.Nodes[i].Status = "merged"
				}
			}

			plan := computeSyncPlan(s, nil)

			mergedBranches := make(map[string]bool)
			for _, node := range s.Nodes {
				if node.Status == "merged" {
					mergedBranches[node.Branch] = true
				}
			}

			for _, a := range plan {
				if a.kind == "skip-merged" && !mergedBranches[a.branch] {
					t.Errorf("skip-merged for non-merged branch %s", a.branch)
				}
			}

			// Also check all merged nodes produce a skip-merged
			skippedBranches := make(map[string]bool)
			for _, a := range plan {
				if a.kind == "skip-merged" {
					skippedBranches[a.branch] = true
				}
			}
			for branch := range mergedBranches {
				if !skippedBranches[branch] {
					t.Errorf("merged branch %s has no skip-merged action", branch)
				}
			}
		})
	}
}
