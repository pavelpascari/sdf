package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
)

// registerTestRepo sets up a temporary git repository that simulates
// pre-existing branches (as if a developer already has a chain of branches
// but hasn't used sdf yet). The repo has:
//
//	main ← feat/schema [s1] ← feat/api [a1] ← feat/ui [u1]
//
// No .sdf directory exists. The caller is chdir'd into the repo.
func registerTestRepo(t *testing.T) (repoDir string, shas map[string]string) {
	t.Helper()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		os.Chdir(origDir)
	})

	shas = make(map[string]string)

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

	writeFile := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Initialize repo
	git("init", "-b", "main")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "Test")
	git("config", "commit.gpgsign", "false")

	// Initial commit on main
	writeFile("README.md", "# test\n")
	git("add", "README.md")
	git("commit", "-m", "initial")
	shas["main"] = git("rev-parse", "HEAD")

	// Create feat/schema with 1 commit
	git("checkout", "-b", "feat/schema")
	writeFile("schema.sql", "CREATE TABLE users (id INT);\n")
	git("add", "schema.sql")
	git("commit", "-m", "add schema")
	shas["s1"] = git("rev-parse", "HEAD")

	// Create feat/api based on feat/schema
	git("checkout", "-b", "feat/api")
	writeFile("api.go", "package api\n")
	git("add", "api.go")
	git("commit", "-m", "add api")
	shas["a1"] = git("rev-parse", "HEAD")

	// Create feat/ui based on feat/api
	git("checkout", "-b", "feat/ui")
	writeFile("ui.html", "<html></html>\n")
	git("add", "ui.html")
	git("commit", "-m", "add ui")
	shas["u1"] = git("rev-parse", "HEAD")

	// Stay on feat/ui
	return dir, shas
}

func TestRegisterStack_ThreeBranchChain(t *testing.T) {
	dir, shas := registerTestRepo(t)

	// Simulate what RunRegister would do after discovering PRs:
	// pass the discovered stack directly to RegisterStack.
	ds := stack.DiscoveredStack{
		Base: "main",
		Chains: []stack.PRRecord{
			{Number: 10, HeadRefName: "feat/schema", BaseRefName: "main"},
			{Number: 11, HeadRefName: "feat/api", BaseRefName: "feat/schema"},
			{Number: 12, HeadRefName: "feat/ui", BaseRefName: "feat/api"},
		},
	}

	if err := RegisterStack(dir, "feat", ds); err != nil {
		t.Fatalf("RegisterStack failed: %v", err)
	}

	// Verify .sdf/stack.json was created
	s, err := stack.Load(dir)
	if err != nil {
		t.Fatalf("cannot load stack after register: %v", err)
	}

	// Verify stack metadata
	if s.StackID != "feat" {
		t.Errorf("expected stack ID 'feat', got %q", s.StackID)
	}
	if s.Base != "main" {
		t.Errorf("expected base 'main', got %q", s.Base)
	}

	// Verify node count
	if len(s.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(s.Nodes))
	}

	// Verify node order and branch names
	expectedBranches := []string{"feat/schema", "feat/api", "feat/ui"}
	for i, want := range expectedBranches {
		if s.Nodes[i].Branch != want {
			t.Errorf("node[%d] branch: got %q, want %q", i, s.Nodes[i].Branch, want)
		}
	}

	// Verify PR numbers are preserved
	expectedPRs := []int{10, 11, 12}
	for i, want := range expectedPRs {
		if s.Nodes[i].PR != want {
			t.Errorf("node[%d] PR: got %d, want %d", i, s.Nodes[i].PR, want)
		}
	}

	// Verify all statuses are "open"
	for i, node := range s.Nodes {
		if node.Status != "open" {
			t.Errorf("node[%d] status: got %q, want 'open'", i, node.Status)
		}
	}

	// Verify BaseTip tracking: first node should track main's tip
	if s.Nodes[0].BaseTip != shas["main"] {
		t.Errorf("node[0] BaseTip: got %q, want main SHA %q", s.Nodes[0].BaseTip, shas["main"])
	}

	// Second node should track feat/schema's tip
	if s.Nodes[1].BaseTip != shas["s1"] {
		t.Errorf("node[1] BaseTip: got %q, want feat/schema SHA %q", s.Nodes[1].BaseTip, shas["s1"])
	}

	// Third node should track feat/api's tip
	if s.Nodes[2].BaseTip != shas["a1"] {
		t.Errorf("node[2] BaseTip: got %q, want feat/api SHA %q", s.Nodes[2].BaseTip, shas["a1"])
	}
}

func TestRegisterStack_DoesNotCommitSDFDirectory(t *testing.T) {
	dir, _ := registerTestRepo(t)

	ds := stack.DiscoveredStack{
		Base: "main",
		Chains: []stack.PRRecord{
			{Number: 1, HeadRefName: "feat/schema", BaseRefName: "main"},
			{Number: 2, HeadRefName: "feat/api", BaseRefName: "feat/schema"},
		},
	}

	if err := RegisterStack(dir, "test-stack", ds); err != nil {
		t.Fatalf("RegisterStack failed: %v", err)
	}

	// .sdf/ should exist on disk
	if _, err := os.Stat(filepath.Join(dir, ".sdf", "stacks", "test-stack.json")); err != nil {
		t.Error("stack file should exist on disk after registration")
	}

	// .sdf/ should NOT be committed (it's local-only state)
	log, _ := exec.Command("git", "-C", dir, "log", "--oneline", "-1").CombinedOutput()
	if strings.Contains(string(log), "register") {
		t.Errorf("register should not create a commit, but latest commit is: %s", string(log))
	}
}

func TestRegisterStack_TwoBranchMinimum(t *testing.T) {
	dir, shas := registerTestRepo(t)

	// Register only two branches — the minimum for a stack
	ds := stack.DiscoveredStack{
		Base: "main",
		Chains: []stack.PRRecord{
			{Number: 10, HeadRefName: "feat/schema", BaseRefName: "main"},
			{Number: 11, HeadRefName: "feat/api", BaseRefName: "feat/schema"},
		},
	}

	if err := RegisterStack(dir, "minimal", ds); err != nil {
		t.Fatalf("RegisterStack failed: %v", err)
	}

	s, err := stack.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(s.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(s.Nodes))
	}
	if s.Nodes[0].Branch != "feat/schema" {
		t.Errorf("expected first branch 'feat/schema', got %q", s.Nodes[0].Branch)
	}
	if s.Nodes[1].Branch != "feat/api" {
		t.Errorf("expected second branch 'feat/api', got %q", s.Nodes[1].Branch)
	}
	if s.Nodes[0].BaseTip != shas["main"] {
		t.Errorf("first node BaseTip should be main SHA")
	}
}

func TestRegisterStack_ErrorIfSameNameAlreadyRegistered(t *testing.T) {
	dir, _ := registerTestRepo(t)

	ds := stack.DiscoveredStack{
		Base: "main",
		Chains: []stack.PRRecord{
			{Number: 1, HeadRefName: "feat/schema", BaseRefName: "main"},
			{Number: 2, HeadRefName: "feat/api", BaseRefName: "feat/schema"},
		},
	}

	// First register should succeed
	if err := RegisterStack(dir, "first", ds); err != nil {
		t.Fatalf("first RegisterStack failed: %v", err)
	}

	// Second register with the same name should fail
	err := RegisterStack(dir, "first", ds)
	if err == nil {
		t.Fatal("expected error on duplicate RegisterStack, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestRegisterStack_ParentBranchRelationships(t *testing.T) {
	dir, _ := registerTestRepo(t)

	ds := stack.DiscoveredStack{
		Base: "main",
		Chains: []stack.PRRecord{
			{Number: 10, HeadRefName: "feat/schema", BaseRefName: "main"},
			{Number: 11, HeadRefName: "feat/api", BaseRefName: "feat/schema"},
			{Number: 12, HeadRefName: "feat/ui", BaseRefName: "feat/api"},
		},
	}

	if err := RegisterStack(dir, "test", ds); err != nil {
		t.Fatalf("RegisterStack failed: %v", err)
	}

	s, err := stack.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Verify ParentBranch relationships work correctly
	if parent := s.ParentBranch("feat/schema"); parent != "main" {
		t.Errorf("parent of feat/schema should be 'main', got %q", parent)
	}
	if parent := s.ParentBranch("feat/api"); parent != "feat/schema" {
		t.Errorf("parent of feat/api should be 'feat/schema', got %q", parent)
	}
	if parent := s.ParentBranch("feat/ui"); parent != "feat/api" {
		t.Errorf("parent of feat/ui should be 'feat/api', got %q", parent)
	}
}

// --- Unit tests for helper functions ---

func TestInferStackName_CommonPrefix(t *testing.T) {
	chain := []stack.PRRecord{
		{HeadRefName: "users/db-schema"},
		{HeadRefName: "users/repository"},
		{HeadRefName: "users/controller"},
	}
	name := inferStackName(chain)
	if name != "users" {
		t.Errorf("expected 'users', got %q", name)
	}
}

func TestInferStackName_CommonPrefixWithDash(t *testing.T) {
	chain := []stack.PRRecord{
		{HeadRefName: "auth-db"},
		{HeadRefName: "auth-api"},
		{HeadRefName: "auth-ui"},
	}
	name := inferStackName(chain)
	if name != "auth" {
		t.Errorf("expected 'auth', got %q", name)
	}
}

func TestInferStackName_NoCommonPrefix(t *testing.T) {
	chain := []stack.PRRecord{
		{HeadRefName: "schema-changes"},
		{HeadRefName: "api-layer"},
	}
	name := inferStackName(chain)
	// No useful common prefix; should use the first branch name
	if name != "schema-changes" {
		t.Errorf("expected 'schema-changes', got %q", name)
	}
}

func TestInferStackName_SlashReplacedWithDash(t *testing.T) {
	chain := []stack.PRRecord{
		{HeadRefName: "feature/auth/db"},
		{HeadRefName: "feature/auth/api"},
	}
	name := inferStackName(chain)
	if name != "feature-auth" {
		t.Errorf("expected 'feature-auth', got %q", name)
	}
}

func TestInferStackName_Empty(t *testing.T) {
	name := inferStackName(nil)
	if name != "my-stack" {
		t.Errorf("expected 'my-stack', got %q", name)
	}
}

func TestCommonPrefix(t *testing.T) {
	tests := []struct {
		a, b, want string
	}{
		{"users/db", "users/api", "users/"},
		{"abc", "abd", "ab"},
		{"abc", "xyz", ""},
		{"same", "same", "same"},
		{"short", "shorter", "short"},
	}

	for _, tc := range tests {
		got := commonPrefix(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("commonPrefix(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}
