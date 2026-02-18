package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
)

func TestDocPath(t *testing.T) {
	tests := []struct {
		name   string
		root   string
		branch string
		want   string
	}{
		{
			name:   "simple branch name",
			root:   "/repo",
			branch: "feature-x",
			want:   filepath.Join("/repo", ".sdf", "context", "feature-x.md"),
		},
		{
			name:   "branch with slash",
			root:   "/repo",
			branch: "auth/db-schema",
			want:   filepath.Join("/repo", ".sdf", "context", "auth", "db-schema.md"),
		},
		{
			name:   "deeply nested branch with slashes",
			root:   "/repo",
			branch: "team/auth/db-schema",
			want:   filepath.Join("/repo", ".sdf", "context", "team", "auth", "db-schema.md"),
		},
		{
			name:   "root with trailing slash",
			root:   "/repo/",
			branch: "feature",
			want:   filepath.Join("/repo", ".sdf", "context", "feature.md"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DocPath(tt.root, tt.branch)
			if got != tt.want {
				t.Errorf("DocPath(%q, %q) = %q, want %q", tt.root, tt.branch, got, tt.want)
			}
		})
	}
}

func TestCreateStub(t *testing.T) {
	t.Run("creates file with correct content", func(t *testing.T) {
		root := t.TempDir()
		branch := "feature-x"
		parent := "main"

		if err := CreateStub(root, branch, parent); err != nil {
			t.Fatalf("CreateStub() error = %v", err)
		}

		path := DocPath(root, branch)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read created file: %v", err)
		}

		content := string(data)

		// Verify the branch name appears as the heading
		if !strings.Contains(content, "# "+branch) {
			t.Errorf("stub does not contain branch heading %q", "# "+branch)
		}

		// Verify the parent branch is referenced
		if !strings.Contains(content, parent) {
			t.Errorf("stub does not reference parent branch %q", parent)
		}

		// Verify standard sections exist
		for _, section := range []string{
			"## Intent",
			"## Constraints from upstream",
			"## Decisions made here",
			"## Open questions / known debt",
		} {
			if !strings.Contains(content, section) {
				t.Errorf("stub missing section %q", section)
			}
		}
	})

	t.Run("creates subdirectories for branch with slash", func(t *testing.T) {
		root := t.TempDir()
		branch := "auth/db-schema"
		parent := "main"

		if err := CreateStub(root, branch, parent); err != nil {
			t.Fatalf("CreateStub() error = %v", err)
		}

		path := DocPath(root, branch)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file at %s to exist", path)
		}

		// Verify the subdirectory was created
		subdir := filepath.Join(root, ".sdf", "context", "auth")
		info, err := os.Stat(subdir)
		if err != nil {
			t.Fatalf("subdirectory %s not created: %v", subdir, err)
		}
		if !info.IsDir() {
			t.Errorf("expected %s to be a directory", subdir)
		}
	})

	t.Run("file has correct permissions", func(t *testing.T) {
		root := t.TempDir()
		if err := CreateStub(root, "feat", "main"); err != nil {
			t.Fatalf("CreateStub() error = %v", err)
		}

		path := DocPath(root, "feat")
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat error: %v", err)
		}

		// os.WriteFile with 0644 -- check the permission bits
		perm := info.Mode().Perm()
		if perm != 0644 {
			t.Errorf("file permissions = %o, want 0644", perm)
		}
	})
}

func TestRead(t *testing.T) {
	t.Run("returns content of existing file", func(t *testing.T) {
		root := t.TempDir()
		branch := "feature-x"
		want := "some context content\n"

		// Set up the directory and file
		dir := filepath.Join(root, ".sdf", "context")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("MkdirAll error: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, branch+".md"), []byte(want), 0644); err != nil {
			t.Fatalf("WriteFile error: %v", err)
		}

		got, err := Read(root, branch)
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		if got != want {
			t.Errorf("Read() = %q, want %q", got, want)
		}
	})

	t.Run("returns empty string for missing file without error", func(t *testing.T) {
		root := t.TempDir()

		got, err := Read(root, "nonexistent")
		if err != nil {
			t.Fatalf("Read() error = %v, want nil", err)
		}
		if got != "" {
			t.Errorf("Read() = %q, want empty string", got)
		}
	})

	t.Run("reads file created by CreateStub", func(t *testing.T) {
		root := t.TempDir()
		branch := "feature-y"
		parent := "main"

		if err := CreateStub(root, branch, parent); err != nil {
			t.Fatalf("CreateStub() error = %v", err)
		}

		got, err := Read(root, branch)
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		if got == "" {
			t.Error("Read() returned empty string for file created by CreateStub")
		}
		if !strings.Contains(got, "# "+branch) {
			t.Errorf("Read() content does not contain expected heading")
		}
	})

	t.Run("reads file for branch with slash", func(t *testing.T) {
		root := t.TempDir()
		branch := "auth/db-schema"
		want := "nested branch content\n"

		dir := filepath.Join(root, ".sdf", "context", "auth")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("MkdirAll error: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "db-schema.md"), []byte(want), 0644); err != nil {
			t.Fatalf("WriteFile error: %v", err)
		}

		got, err := Read(root, branch)
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		if got != want {
			t.Errorf("Read() = %q, want %q", got, want)
		}
	})
}

func TestExists(t *testing.T) {
	t.Run("returns true when file exists", func(t *testing.T) {
		root := t.TempDir()
		branch := "feature-x"

		dir := filepath.Join(root, ".sdf", "context")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("MkdirAll error: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, branch+".md"), []byte("content"), 0644); err != nil {
			t.Fatalf("WriteFile error: %v", err)
		}

		if !Exists(root, branch) {
			t.Error("Exists() = false, want true")
		}
	})

	t.Run("returns false when file does not exist", func(t *testing.T) {
		root := t.TempDir()

		if Exists(root, "nonexistent") {
			t.Error("Exists() = true, want false")
		}
	})

	t.Run("returns true after CreateStub", func(t *testing.T) {
		root := t.TempDir()
		branch := "feature-z"

		if Exists(root, branch) {
			t.Fatal("Exists() = true before CreateStub, want false")
		}

		if err := CreateStub(root, branch, "main"); err != nil {
			t.Fatalf("CreateStub() error = %v", err)
		}

		if !Exists(root, branch) {
			t.Error("Exists() = false after CreateStub, want true")
		}
	})
}

// setupContextFile is a test helper that creates a context doc file for
// the given branch within the temp directory structure.
func setupContextFile(t *testing.T, root, branch, content string) {
	t.Helper()
	path := DocPath(root, branch)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
}

func TestAssemble(t *testing.T) {
	t.Run("single node stack", func(t *testing.T) {
		root := t.TempDir()
		s := &stack.Stack{
			StackID: "test-stack",
			Base:    "main",
			Nodes: []stack.Node{
				{Branch: "feature-a", Status: "open"},
			},
		}

		setupContextFile(t, root, "feature-a", "Feature A context\n")

		got, err := Assemble(root, s, "feature-a")
		if err != nil {
			t.Fatalf("Assemble() error = %v", err)
		}

		// Verify stack header
		if !strings.HasPrefix(got, "=== STACK: test-stack ===\n\n") {
			t.Errorf("missing or incorrect stack header, got:\n%s", got)
		}

		// Verify current marker on the only (and target) branch
		if !strings.Contains(got, "[feature-a \u2190 current]") {
			t.Errorf("missing current marker, got:\n%s", got)
		}

		// Verify content is included
		if !strings.Contains(got, "Feature A context") {
			t.Errorf("missing branch content, got:\n%s", got)
		}
	})

	t.Run("multiple nodes with current marker only on target", func(t *testing.T) {
		root := t.TempDir()
		s := &stack.Stack{
			StackID: "multi-stack",
			Base:    "main",
			Nodes: []stack.Node{
				{Branch: "feature-a", Status: "open"},
				{Branch: "feature-b", Status: "open"},
				{Branch: "feature-c", Status: "open"},
			},
		}

		setupContextFile(t, root, "feature-a", "Context A\n")
		setupContextFile(t, root, "feature-b", "Context B\n")
		setupContextFile(t, root, "feature-c", "Context C\n")

		got, err := Assemble(root, s, "feature-c")
		if err != nil {
			t.Fatalf("Assemble() error = %v", err)
		}

		// feature-a and feature-b should NOT have the current marker
		if strings.Contains(got, "[feature-a \u2190 current]") {
			t.Error("feature-a should not have current marker")
		}
		if strings.Contains(got, "[feature-b \u2190 current]") {
			t.Error("feature-b should not have current marker")
		}

		// feature-c should have the current marker
		if !strings.Contains(got, "[feature-c \u2190 current]") {
			t.Error("feature-c should have current marker")
		}

		// All content should be present
		if !strings.Contains(got, "Context A") {
			t.Error("missing Context A")
		}
		if !strings.Contains(got, "Context B") {
			t.Error("missing Context B")
		}
		if !strings.Contains(got, "Context C") {
			t.Error("missing Context C")
		}

		// Verify separator between parts
		if !strings.Contains(got, "\n---\n\n") {
			t.Error("missing separator between parts")
		}
	})

	t.Run("target in middle of stack only includes ancestors", func(t *testing.T) {
		root := t.TempDir()
		s := &stack.Stack{
			StackID: "mid-stack",
			Base:    "main",
			Nodes: []stack.Node{
				{Branch: "feature-a", Status: "open"},
				{Branch: "feature-b", Status: "open"},
				{Branch: "feature-c", Status: "open"},
			},
		}

		setupContextFile(t, root, "feature-a", "Context A\n")
		setupContextFile(t, root, "feature-b", "Context B\n")
		setupContextFile(t, root, "feature-c", "Context C\n")

		got, err := Assemble(root, s, "feature-b")
		if err != nil {
			t.Fatalf("Assemble() error = %v", err)
		}

		// feature-a and feature-b should be included
		if !strings.Contains(got, "Context A") {
			t.Error("missing Context A (ancestor)")
		}
		if !strings.Contains(got, "Context B") {
			t.Error("missing Context B (target)")
		}

		// feature-c should NOT be included (it comes after the target)
		if strings.Contains(got, "Context C") {
			t.Error("Context C should not be included (comes after target)")
		}

		// Only feature-b has the current marker
		if !strings.Contains(got, "[feature-b \u2190 current]") {
			t.Error("feature-b should have current marker")
		}
	})

	t.Run("branch not in stack returns error", func(t *testing.T) {
		root := t.TempDir()
		s := &stack.Stack{
			StackID: "test-stack",
			Base:    "main",
			Nodes: []stack.Node{
				{Branch: "feature-a", Status: "open"},
			},
		}

		_, err := Assemble(root, s, "nonexistent-branch")
		if err == nil {
			t.Fatal("Assemble() expected error for branch not in stack, got nil")
		}
		if !strings.Contains(err.Error(), "nonexistent-branch") {
			t.Errorf("error should mention branch name, got: %v", err)
		}
		if !strings.Contains(err.Error(), "not found in stack") {
			t.Errorf("error should mention 'not found in stack', got: %v", err)
		}
	})

	t.Run("node with no context doc is skipped", func(t *testing.T) {
		root := t.TempDir()
		s := &stack.Stack{
			StackID: "skip-stack",
			Base:    "main",
			Nodes: []stack.Node{
				{Branch: "feature-a", Status: "open"},
				{Branch: "feature-b", Status: "open"},
				{Branch: "feature-c", Status: "open"},
			},
		}

		// Only create context for feature-a and feature-c; skip feature-b
		setupContextFile(t, root, "feature-a", "Context A\n")
		// feature-b intentionally has no context file
		setupContextFile(t, root, "feature-c", "Context C\n")

		got, err := Assemble(root, s, "feature-c")
		if err != nil {
			t.Fatalf("Assemble() error = %v", err)
		}

		if !strings.Contains(got, "Context A") {
			t.Error("missing Context A")
		}
		if !strings.Contains(got, "Context C") {
			t.Error("missing Context C")
		}

		// feature-b should not appear at all since it has no context
		if strings.Contains(got, "[feature-b") {
			t.Error("feature-b should be skipped (no context doc)")
		}
	})

	t.Run("stack header format", func(t *testing.T) {
		root := t.TempDir()
		stackID := "my-cool-stack-123"
		s := &stack.Stack{
			StackID: stackID,
			Base:    "main",
			Nodes: []stack.Node{
				{Branch: "feat", Status: "open"},
			},
		}

		setupContextFile(t, root, "feat", "content\n")

		got, err := Assemble(root, s, "feat")
		if err != nil {
			t.Fatalf("Assemble() error = %v", err)
		}

		expectedHeader := "=== STACK: " + stackID + " ===\n\n"
		if !strings.HasPrefix(got, expectedHeader) {
			t.Errorf("expected header %q, got prefix: %q", expectedHeader, got[:min(len(got), len(expectedHeader)+10)])
		}
	})

	t.Run("all nodes missing context returns header only", func(t *testing.T) {
		root := t.TempDir()
		s := &stack.Stack{
			StackID: "empty-stack",
			Base:    "main",
			Nodes: []stack.Node{
				{Branch: "feature-a", Status: "open"},
			},
		}

		// No context files created at all

		got, err := Assemble(root, s, "feature-a")
		if err != nil {
			t.Fatalf("Assemble() error = %v", err)
		}

		expected := "=== STACK: empty-stack ===\n\n"
		if got != expected {
			t.Errorf("Assemble() = %q, want %q", got, expected)
		}
	})
}

func TestBuildConflictPrompt(t *testing.T) {
	t.Run("all fields populated", func(t *testing.T) {
		stackCtx := "stack context here"
		upstream := "upstream changes"
		intent := "branch intent"
		files := map[string]string{
			"main.go": "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>>",
		}

		got := BuildConflictPrompt(stackCtx, upstream, intent, files)

		// Verify opening line
		if !strings.Contains(got, "You are resolving conflicts during a stack rebase.") {
			t.Error("missing opening line")
		}

		// Verify stack context section
		if !strings.Contains(got, "=== STACK CONTEXT ===") {
			t.Error("missing stack context section")
		}
		if !strings.Contains(got, stackCtx) {
			t.Error("missing stack context content")
		}

		// Verify upstream summary section
		if !strings.Contains(got, "=== UPSTREAM CHANGE SUMMARY ===") {
			t.Error("missing upstream summary section")
		}
		if !strings.Contains(got, upstream) {
			t.Error("missing upstream summary content")
		}

		// Verify branch intent section
		if !strings.Contains(got, "=== CURRENT BRANCH INTENT ===") {
			t.Error("missing branch intent section")
		}
		if !strings.Contains(got, intent) {
			t.Error("missing branch intent content")
		}

		// Verify conflicts section
		if !strings.Contains(got, "=== CONFLICTS ===") {
			t.Error("missing conflicts section")
		}
		if !strings.Contains(got, "File: main.go") {
			t.Error("missing conflict filename")
		}
		if !strings.Contains(got, "<<<<<<< HEAD") {
			t.Error("missing conflict content")
		}

		// Verify closing instructions
		if !strings.Contains(got, "Resolve all conflicts") {
			t.Error("missing resolution instructions")
		}
	})

	t.Run("empty upstream summary omits section", func(t *testing.T) {
		files := map[string]string{
			"file.go": "conflict",
		}

		got := BuildConflictPrompt("ctx", "", "intent", files)

		if strings.Contains(got, "=== UPSTREAM CHANGE SUMMARY ===") {
			t.Error("upstream summary section should be omitted when empty")
		}

		// Other sections should still be present
		if !strings.Contains(got, "=== STACK CONTEXT ===") {
			t.Error("stack context section should still be present")
		}
		if !strings.Contains(got, "=== CURRENT BRANCH INTENT ===") {
			t.Error("branch intent section should still be present")
		}
	})

	t.Run("empty branch intent omits section", func(t *testing.T) {
		files := map[string]string{
			"file.go": "conflict",
		}

		got := BuildConflictPrompt("ctx", "upstream", "", files)

		if strings.Contains(got, "=== CURRENT BRANCH INTENT ===") {
			t.Error("branch intent section should be omitted when empty")
		}

		// Other sections should still be present
		if !strings.Contains(got, "=== STACK CONTEXT ===") {
			t.Error("stack context section should still be present")
		}
		if !strings.Contains(got, "=== UPSTREAM CHANGE SUMMARY ===") {
			t.Error("upstream summary section should still be present")
		}
	})

	t.Run("both optional sections omitted", func(t *testing.T) {
		files := map[string]string{
			"file.go": "conflict",
		}

		got := BuildConflictPrompt("ctx", "", "", files)

		if strings.Contains(got, "=== UPSTREAM CHANGE SUMMARY ===") {
			t.Error("upstream summary section should be omitted")
		}
		if strings.Contains(got, "=== CURRENT BRANCH INTENT ===") {
			t.Error("branch intent section should be omitted")
		}

		// Required sections still present
		if !strings.Contains(got, "=== STACK CONTEXT ===") {
			t.Error("stack context section must be present")
		}
		if !strings.Contains(got, "=== CONFLICTS ===") {
			t.Error("conflicts section must be present")
		}
	})

	t.Run("multiple conflicted files", func(t *testing.T) {
		files := map[string]string{
			"main.go":   "conflict in main",
			"util.go":   "conflict in util",
			"README.md": "conflict in readme",
		}

		got := BuildConflictPrompt("ctx", "up", "intent", files)

		// Verify all files appear in the output
		for filename, content := range files {
			if !strings.Contains(got, "File: "+filename) {
				t.Errorf("missing filename %q in output", filename)
			}
			if !strings.Contains(got, content) {
				t.Errorf("missing content for %q in output", filename)
			}
		}
	})

	t.Run("conflict file content is fenced in code blocks", func(t *testing.T) {
		files := map[string]string{
			"app.go": "package main\nfunc main() {}",
		}

		got := BuildConflictPrompt("ctx", "", "", files)

		// Verify the file content is wrapped in code fences
		if !strings.Contains(got, "File: app.go\n```\npackage main\nfunc main() {}\n```") {
			t.Errorf("file content not properly fenced, got:\n%s", got)
		}
	})

	t.Run("section ordering", func(t *testing.T) {
		files := map[string]string{"f.go": "c"}

		got := BuildConflictPrompt("ctx", "up", "intent", files)

		stackIdx := strings.Index(got, "=== STACK CONTEXT ===")
		upstreamIdx := strings.Index(got, "=== UPSTREAM CHANGE SUMMARY ===")
		intentIdx := strings.Index(got, "=== CURRENT BRANCH INTENT ===")
		conflictsIdx := strings.Index(got, "=== CONFLICTS ===")

		if stackIdx < 0 || upstreamIdx < 0 || intentIdx < 0 || conflictsIdx < 0 {
			t.Fatal("one or more sections missing from output")
		}

		if !(stackIdx < upstreamIdx && upstreamIdx < intentIdx && intentIdx < conflictsIdx) {
			t.Errorf("sections not in expected order: stack=%d, upstream=%d, intent=%d, conflicts=%d",
				stackIdx, upstreamIdx, intentIdx, conflictsIdx)
		}
	})
}
