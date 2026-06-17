// cmd/worktree_helpers_test.go
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	"github.com/pavelpascari/sdf/internal/stack"
)

// wtTestRepo creates an sdf repo (main + one commit + .sdf) and returns its root.
func wtTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@t.com")
	run("config", "user.name", "T")
	os.WriteFile(filepath.Join(root, "f.txt"), []byte("x\n"), 0644)
	run("add", ".")
	run("commit", "-m", "init")
	if err := stack.Init(root, "feat", "main"); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	return root
}

func TestAddAndRemoveWorktreeForNode(t *testing.T) {
	root := wtTestRepo(t)
	cfg := cfgpkg.Defaults()
	node := &stack.Node{Branch: "feat/a", Status: "open"}

	if err := addWorktreeForNode(cfg, root, node, "main"); err != nil {
		t.Fatalf("addWorktreeForNode: %v", err)
	}
	want := cfg.WorktreePathFor(root, "feat/a")
	if node.WorktreePath != want {
		t.Errorf("WorktreePath = %q, want %q", node.WorktreePath, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("worktree dir not created: %v", err)
	}

	if err := removeWorktreeForNode(root, node, false); err != nil {
		t.Fatalf("removeWorktreeForNode: %v", err)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Errorf("worktree dir should be removed")
	}
}

func TestCurrentWorktreeNode(t *testing.T) {
	s := &stack.Stack{StackID: "feat", Base: "main", Worktree: true, Nodes: []stack.Node{
		{Branch: "feat/a", WorktreePath: "/tmp/wt/feat-a"},
		{Branch: "feat/b"},
	}}
	if n := currentWorktreeNode(s, "feat/a"); n == nil || n.Branch != "feat/a" {
		t.Errorf("expected feat/a node, got %v", n)
	}
	if n := currentWorktreeNode(s, "feat/b"); n != nil {
		t.Errorf("feat/b has no worktree path; expected nil")
	}
	if n := currentWorktreeNode(s, "main"); n != nil {
		t.Errorf("main is not a node; expected nil")
	}
}

func TestBranchWorktreeDir(t *testing.T) {
	s := &stack.Stack{StackID: "feat", Base: "main", Worktree: true, Nodes: []stack.Node{
		{Branch: "feat/a", WorktreePath: "/tmp/wt/feat-a"},
		{Branch: "feat/b"},
	}}
	if got := branchWorktreeDir(s, "feat/a"); got != "/tmp/wt/feat-a" {
		t.Errorf("branchWorktreeDir(feat/a) = %q, want /tmp/wt/feat-a", got)
	}
	if got := branchWorktreeDir(s, "feat/b"); got != "" {
		t.Errorf("branchWorktreeDir(feat/b) = %q, want empty", got)
	}
	if got := branchWorktreeDir(s, "main"); got != "" {
		t.Errorf("branchWorktreeDir(main) = %q, want empty (not a node)", got)
	}
}
