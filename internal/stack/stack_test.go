package stack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ------------------------------------------------------------
// helpers
// ------------------------------------------------------------

// makeStack returns a Stack with the given base and branch names.
// All nodes get status "open" by default.
func makeStack(base string, branches ...string) *Stack {
	nodes := make([]Node, len(branches))
	for i, b := range branches {
		nodes[i] = Node{Branch: b, Status: "open"}
	}
	return &Stack{
		StackID: "test-stack",
		Base:    base,
		Nodes:   nodes,
	}
}

// setupStackDir creates a .sdf directory inside root so that Save can write to it.
func setupStackDir(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, SDFDir), 0755); err != nil {
		t.Fatalf("setupStackDir: %v", err)
	}
}

// ------------------------------------------------------------
// FindNode
// ------------------------------------------------------------

func TestFindNode(t *testing.T) {
	tests := []struct {
		name      string
		branches  []string
		lookup    string
		wantNil   bool
		wantBranch string
	}{
		{
			name:       "found first node",
			branches:   []string{"feat-a", "feat-b", "feat-c"},
			lookup:     "feat-a",
			wantBranch: "feat-a",
		},
		{
			name:       "found middle node",
			branches:   []string{"feat-a", "feat-b", "feat-c"},
			lookup:     "feat-b",
			wantBranch: "feat-b",
		},
		{
			name:       "found last node",
			branches:   []string{"feat-a", "feat-b", "feat-c"},
			lookup:     "feat-c",
			wantBranch: "feat-c",
		},
		{
			name:     "not found",
			branches: []string{"feat-a", "feat-b"},
			lookup:   "feat-z",
			wantNil:  true,
		},
		{
			name:     "empty nodes",
			branches: []string{},
			lookup:   "anything",
			wantNil:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := makeStack("main", tc.branches...)
			got := s.FindNode(tc.lookup)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected node %q, got nil", tc.wantBranch)
			}
			if got.Branch != tc.wantBranch {
				t.Fatalf("expected branch %q, got %q", tc.wantBranch, got.Branch)
			}
		})
	}
}

func TestFindNode_ReturnsMutablePointer(t *testing.T) {
	s := makeStack("main", "feat-a", "feat-b")
	node := s.FindNode("feat-a")
	if node == nil {
		t.Fatal("expected non-nil node")
	}
	// Mutate through the returned pointer.
	node.PR = 42
	node.Status = "merged"

	// Verify the mutation is reflected in the stack's slice.
	if s.Nodes[0].PR != 42 {
		t.Fatalf("expected PR 42, got %d", s.Nodes[0].PR)
	}
	if s.Nodes[0].Status != "merged" {
		t.Fatalf("expected status %q, got %q", "merged", s.Nodes[0].Status)
	}
}

// ------------------------------------------------------------
// NodeIndex
// ------------------------------------------------------------

func TestNodeIndex(t *testing.T) {
	tests := []struct {
		name     string
		branches []string
		lookup   string
		want     int
	}{
		{
			name:     "first element",
			branches: []string{"feat-a", "feat-b", "feat-c"},
			lookup:   "feat-a",
			want:     0,
		},
		{
			name:     "middle element",
			branches: []string{"feat-a", "feat-b", "feat-c"},
			lookup:   "feat-b",
			want:     1,
		},
		{
			name:     "last element",
			branches: []string{"feat-a", "feat-b", "feat-c"},
			lookup:   "feat-c",
			want:     2,
		},
		{
			name:     "not found",
			branches: []string{"feat-a", "feat-b"},
			lookup:   "feat-z",
			want:     -1,
		},
		{
			name:     "empty nodes",
			branches: []string{},
			lookup:   "anything",
			want:     -1,
		},
		{
			name:     "single node found",
			branches: []string{"only"},
			lookup:   "only",
			want:     0,
		},
		{
			name:     "single node not found",
			branches: []string{"only"},
			lookup:   "other",
			want:     -1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := makeStack("main", tc.branches...)
			got := s.NodeIndex(tc.lookup)
			if got != tc.want {
				t.Fatalf("NodeIndex(%q) = %d, want %d", tc.lookup, got, tc.want)
			}
		})
	}
}

// ------------------------------------------------------------
// ParentBranch
// ------------------------------------------------------------

func TestParentBranch(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		branches []string
		lookup   string
		want     string
	}{
		{
			name:     "first node returns base",
			base:     "main",
			branches: []string{"feat-a", "feat-b", "feat-c"},
			lookup:   "feat-a",
			want:     "main",
		},
		{
			name:     "second node returns first branch",
			base:     "main",
			branches: []string{"feat-a", "feat-b", "feat-c"},
			lookup:   "feat-b",
			want:     "feat-a",
		},
		{
			name:     "third node returns second branch",
			base:     "main",
			branches: []string{"feat-a", "feat-b", "feat-c"},
			lookup:   "feat-c",
			want:     "feat-b",
		},
		{
			name:     "not found returns base",
			base:     "develop",
			branches: []string{"feat-a", "feat-b"},
			lookup:   "feat-z",
			want:     "develop",
		},
		{
			name:     "empty nodes returns base",
			base:     "trunk",
			branches: []string{},
			lookup:   "anything",
			want:     "trunk",
		},
		{
			name:     "single node returns base",
			base:     "main",
			branches: []string{"only"},
			lookup:   "only",
			want:     "main",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := makeStack(tc.base, tc.branches...)
			got := s.ParentBranch(tc.lookup)
			if got != tc.want {
				t.Fatalf("ParentBranch(%q) = %q, want %q", tc.lookup, got, tc.want)
			}
		})
	}
}

// ------------------------------------------------------------
// Save / Load roundtrip
// ------------------------------------------------------------

func TestSaveAndLoad_Roundtrip(t *testing.T) {
	root := t.TempDir()
	setupStackDir(t, root)

	original := &Stack{
		StackID: "abc-123",
		Base:    "main",
		Nodes: []Node{
			{Branch: "feat-a", PR: 10, Status: "open", BaseTip: "aaa111"},
			{Branch: "feat-b", PR: 0, Status: "draft"},
			{Branch: "feat-c", PR: 20, Status: "merged", BaseTip: "ccc333"},
		},
	}

	if err := Save(root, original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Compare top-level fields.
	if loaded.StackID != original.StackID {
		t.Errorf("StackID: got %q, want %q", loaded.StackID, original.StackID)
	}
	if loaded.Base != original.Base {
		t.Errorf("Base: got %q, want %q", loaded.Base, original.Base)
	}
	if len(loaded.Nodes) != len(original.Nodes) {
		t.Fatalf("Nodes length: got %d, want %d", len(loaded.Nodes), len(original.Nodes))
	}

	// Compare each node.
	for i, want := range original.Nodes {
		got := loaded.Nodes[i]
		if got.Branch != want.Branch {
			t.Errorf("Nodes[%d].Branch: got %q, want %q", i, got.Branch, want.Branch)
		}
		if got.PR != want.PR {
			t.Errorf("Nodes[%d].PR: got %d, want %d", i, got.PR, want.PR)
		}
		if got.Status != want.Status {
			t.Errorf("Nodes[%d].Status: got %q, want %q", i, got.Status, want.Status)
		}
		if got.BaseTip != want.BaseTip {
			t.Errorf("Nodes[%d].BaseTip: got %q, want %q", i, got.BaseTip, want.BaseTip)
		}
	}
}

func TestSaveAndLoad_EmptyNodes(t *testing.T) {
	root := t.TempDir()
	setupStackDir(t, root)

	original := &Stack{
		StackID: "empty-stack",
		Base:    "develop",
		Nodes:   []Node{},
	}

	if err := Save(root, original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(loaded.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(loaded.Nodes))
	}
}

func TestSave_WritesValidJSON(t *testing.T) {
	root := t.TempDir()
	setupStackDir(t, root)

	s := makeStack("main", "feat-a")
	s.Nodes[0].PR = 5
	s.Nodes[0].BaseTip = "abc"

	if err := Save(root, s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(root, SDFDir, StackFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Must be valid JSON.
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("written file is not valid JSON: %v", err)
	}

	// File should end with a newline.
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("expected file to end with a trailing newline")
	}
}

func TestSave_OmitsZeroPR(t *testing.T) {
	root := t.TempDir()
	setupStackDir(t, root)

	s := makeStack("main", "feat-a")
	// PR is 0 and BaseTip is empty -- both should be omitted.
	s.Nodes[0].PR = 0
	s.Nodes[0].BaseTip = ""

	if err := Save(root, s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(root, SDFDir, StackFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Unmarshal into a generic structure to check field presence.
	var raw struct {
		Nodes []map[string]interface{} `json:"nodes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(raw.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(raw.Nodes))
	}
	if _, ok := raw.Nodes[0]["pr"]; ok {
		t.Error("expected pr field to be omitted when zero")
	}
	if _, ok := raw.Nodes[0]["base_tip"]; ok {
		t.Error("expected base_tip field to be omitted when empty")
	}
}

func TestLoad_NonexistentFile(t *testing.T) {
	root := t.TempDir()
	// No .sdf directory at all.
	_, err := Load(root)
	if err == nil {
		t.Fatal("expected error when loading from nonexistent file, got nil")
	}
}

func TestLoad_MalformedJSON(t *testing.T) {
	root := t.TempDir()
	setupStackDir(t, root)

	path := filepath.Join(root, SDFDir, StackFile)
	if err := os.WriteFile(path, []byte("this is not json{{{"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := Load(root)
	if err == nil {
		t.Fatal("expected error when loading malformed JSON, got nil")
	}
}

func TestLoad_EmptyFile(t *testing.T) {
	root := t.TempDir()
	setupStackDir(t, root)

	path := filepath.Join(root, SDFDir, StackFile)
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := Load(root)
	if err == nil {
		t.Fatal("expected error when loading empty file, got nil")
	}
}

func TestSave_NonexistentDirectory(t *testing.T) {
	root := t.TempDir()
	// Do NOT create .sdf -- Save should fail.
	s := makeStack("main", "feat-a")
	err := Save(root, s)
	if err == nil {
		t.Fatal("expected error when saving to nonexistent directory, got nil")
	}
}

// ------------------------------------------------------------
// Init
// ------------------------------------------------------------

func TestInit(t *testing.T) {
	root := t.TempDir()

	stackID := "my-stack-id"
	base := "main"

	if err := Init(root, stackID, base); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// .sdf directory should exist.
	sdfDir := filepath.Join(root, SDFDir)
	info, err := os.Stat(sdfDir)
	if err != nil {
		t.Fatalf(".sdf dir does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal(".sdf is not a directory")
	}

	// .sdf/context directory should exist.
	contextDir := filepath.Join(sdfDir, "context")
	info, err = os.Stat(contextDir)
	if err != nil {
		t.Fatalf(".sdf/context dir does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal(".sdf/context is not a directory")
	}

	// .sdf/stack.json should exist and contain valid content.
	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load after Init: %v", err)
	}
	if loaded.StackID != stackID {
		t.Errorf("StackID: got %q, want %q", loaded.StackID, stackID)
	}
	if loaded.Base != base {
		t.Errorf("Base: got %q, want %q", loaded.Base, base)
	}
	if len(loaded.Nodes) != 0 {
		t.Errorf("Nodes: expected empty slice, got %d nodes", len(loaded.Nodes))
	}

	// .sdf/local.json should exist and contain "{}".
	localPath := filepath.Join(sdfDir, LocalFile)
	localData, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("local.json does not exist: %v", err)
	}
	if string(localData) != "{}\n" {
		t.Errorf("local.json content: got %q, want %q", string(localData), "{}\n")
	}
}

func TestInit_Idempotent(t *testing.T) {
	root := t.TempDir()

	// Call Init twice; the second call should not fail.
	if err := Init(root, "stack-1", "main"); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	if err := Init(root, "stack-2", "develop"); err != nil {
		t.Fatalf("second Init: %v", err)
	}

	// The second call should overwrite the stack.json.
	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load after second Init: %v", err)
	}
	if loaded.StackID != "stack-2" {
		t.Errorf("StackID: got %q, want %q", loaded.StackID, "stack-2")
	}
	if loaded.Base != "develop" {
		t.Errorf("Base: got %q, want %q", loaded.Base, "develop")
	}
}

// ------------------------------------------------------------
// FindRoot
// ------------------------------------------------------------

func TestFindRoot_InSDFRoot(t *testing.T) {
	root := t.TempDir()
	if err := Init(root, "test", "main"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Change into the root directory.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	found, err := FindRoot()
	if err != nil {
		t.Fatalf("FindRoot: %v", err)
	}
	if found != root {
		t.Errorf("FindRoot() = %q, want %q", found, root)
	}
}

func TestFindRoot_InSubdirectory(t *testing.T) {
	root := t.TempDir()
	if err := Init(root, "test", "main"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a nested subdirectory.
	subdir := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	if err := os.Chdir(subdir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	found, err := FindRoot()
	if err != nil {
		t.Fatalf("FindRoot: %v", err)
	}
	if found != root {
		t.Errorf("FindRoot() = %q, want %q", found, root)
	}
}

func TestFindRoot_NoSDFDir(t *testing.T) {
	root := t.TempDir()
	// No Init -- no .sdf directory.

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	_, err = FindRoot()
	if err == nil {
		t.Fatal("expected error when no .sdf directory exists, got nil")
	}
}

// ------------------------------------------------------------
// Save then mutate then Save again
// ------------------------------------------------------------

func TestSaveAndLoad_MutateAndResave(t *testing.T) {
	root := t.TempDir()
	setupStackDir(t, root)

	s := makeStack("main", "feat-a")
	if err := Save(root, s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Load, mutate, save again.
	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	loaded.Nodes = append(loaded.Nodes, Node{Branch: "feat-b", PR: 7, Status: "draft"})
	if err := Save(root, loaded); err != nil {
		t.Fatalf("Save after mutation: %v", err)
	}

	// Reload and verify.
	reloaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load after resave: %v", err)
	}
	if len(reloaded.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(reloaded.Nodes))
	}
	if reloaded.Nodes[1].Branch != "feat-b" {
		t.Errorf("Nodes[1].Branch: got %q, want %q", reloaded.Nodes[1].Branch, "feat-b")
	}
	if reloaded.Nodes[1].PR != 7 {
		t.Errorf("Nodes[1].PR: got %d, want %d", reloaded.Nodes[1].PR, 7)
	}
}
