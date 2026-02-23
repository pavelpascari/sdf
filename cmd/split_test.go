package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

// resetSplitFlags resets cobra flag state between test runs.
// Cobra/pflag does not reset flag values between Execute() calls,
// so flags set in one test (e.g., --dry-run) persist into the next.
func resetSplitFlags() {
	splitCmd.Flags().Set("dry-run", "false")
	splitCmd.Flags().Set("yes", "false")
	splitCmd.Flags().Set("stack", "")
	splitCmd.Flags().Set("base", "")
	splitCmd.Flags().Set("parts", "0")
	splitCmd.Flags().Set("no-push", "false")
}

// splitTestRepo sets up a temporary git repo with 6 commits on "big-feature"
// touching 3 different directories: internal/git (2), internal/stack (2), cmd (2).
// HEAD is left on big-feature.
func splitTestRepo(t *testing.T) string {
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
		full := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(full), 0755)
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Initialize repo
	git("init", "-b", "main")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "Test")
	git("config", "commit.gpgsign", "false")

	writeFile("README.md", "# test\n")
	git("add", ".")
	git("commit", "-m", "initial")

	// Create feature branch
	git("checkout", "-b", "big-feature")

	// 2 commits in internal/git
	writeFile("internal/git/helpers.go", "package git\nfunc Help() {}\n")
	git("add", ".")
	git("commit", "-m", "add git helpers")

	writeFile("internal/git/utils.go", "package git\nfunc Util() {}\n")
	git("add", ".")
	git("commit", "-m", "add git utils")

	// 2 commits in internal/stack
	writeFile("internal/stack/topology.go", "package stack\nfunc Topo() {}\n")
	git("add", ".")
	git("commit", "-m", "add stack topology")

	writeFile("internal/stack/persist.go", "package stack\nfunc Persist() {}\n")
	git("add", ".")
	git("commit", "-m", "add stack persistence")

	// 2 commits in cmd
	writeFile("cmd/split.go", "package cmd\nfunc Split() {}\n")
	git("add", ".")
	git("commit", "-m", "add split command")

	writeFile("cmd/status.go", "package cmd\nfunc Status() {}\n")
	git("add", ".")
	git("commit", "-m", "add status command")

	return dir
}

func TestAnalyzeBranch(t *testing.T) {
	splitTestRepo(t)

	commits, err := analyzeBranch("main", "big-feature")
	if err != nil {
		t.Fatalf("analyzeBranch: %v", err)
	}

	if len(commits) != 6 {
		t.Fatalf("expected 6 commits, got %d", len(commits))
	}

	// First commit should be "add git helpers" (oldest first)
	if commits[0].Subject != "add git helpers" {
		t.Errorf("first commit subject: got %q, want %q", commits[0].Subject, "add git helpers")
	}

	// Last commit should be "add status command"
	if commits[5].Subject != "add status command" {
		t.Errorf("last commit subject: got %q, want %q", commits[5].Subject, "add status command")
	}

	// First commit should have one file in internal/git
	if len(commits[0].Files) != 1 || commits[0].Files[0] != "internal/git/helpers.go" {
		t.Errorf("first commit files: got %v", commits[0].Files)
	}
}

func TestAutoGroup_ThreeDirectories(t *testing.T) {
	splitTestRepo(t)

	commits, err := analyzeBranch("main", "big-feature")
	if err != nil {
		t.Fatalf("analyzeBranch: %v", err)
	}

	groups := autoGroup(commits)

	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}

	// Group 1: internal/git (2 commits)
	if len(groups[0].Commits) != 2 {
		t.Errorf("group 0: expected 2 commits, got %d", len(groups[0].Commits))
	}
	if groups[0].Title != "git" {
		t.Errorf("group 0 title: got %q, want %q", groups[0].Title, "git")
	}

	// Group 2: internal/stack (2 commits)
	if len(groups[1].Commits) != 2 {
		t.Errorf("group 1: expected 2 commits, got %d", len(groups[1].Commits))
	}
	if groups[1].Title != "stack" {
		t.Errorf("group 1 title: got %q, want %q", groups[1].Title, "stack")
	}

	// Group 3: cmd (2 commits)
	if len(groups[2].Commits) != 2 {
		t.Errorf("group 2: expected 2 commits, got %d", len(groups[2].Commits))
	}
	if groups[2].Title != "cmd" {
		t.Errorf("group 2 title: got %q, want %q", groups[2].Title, "cmd")
	}
}

func TestAutoGroup_SingleDirectory(t *testing.T) {
	// When all commits touch the same directory, auto-group returns one group
	commits := []commitInfo{
		{SHA: "aaa", Subject: "a", Files: []string{"src/foo.go"}},
		{SHA: "bbb", Subject: "b", Files: []string{"src/bar.go"}},
		{SHA: "ccc", Subject: "c", Files: []string{"src/baz.go"}},
	}

	groups := autoGroup(commits)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0].Commits) != 3 {
		t.Errorf("group 0: expected 3 commits, got %d", len(groups[0].Commits))
	}
}

func TestEqualSplit(t *testing.T) {
	commits := make([]commitInfo, 7)
	for i := range commits {
		commits[i] = commitInfo{SHA: strings.Repeat("a", i+1), Subject: "c", Files: []string{"f.go"}}
	}

	groups := equalSplit(commits, 3)

	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}

	// 7 commits / 3 parts = 3, 2, 2
	if len(groups[0].Commits) != 3 {
		t.Errorf("group 0: expected 3 commits, got %d", len(groups[0].Commits))
	}
	if len(groups[1].Commits) != 2 {
		t.Errorf("group 1: expected 2 commits, got %d", len(groups[1].Commits))
	}
	if len(groups[2].Commits) != 2 {
		t.Errorf("group 2: expected 2 commits, got %d", len(groups[2].Commits))
	}
}

func TestSplitDryRun(t *testing.T) {
	resetSplitFlags()
	splitTestRepo(t)

	err := RunSplit([]string{"--dry-run", "--base", "main"})
	if err != nil {
		t.Fatalf("split --dry-run: %v", err)
	}

	// Verify no stack was created
	_, findErr := stack.FindRoot()
	if findErr == nil {
		t.Error("expected no .sdf directory after --dry-run, but found one")
	}

	// Verify we're still on big-feature
	current, _ := gitpkg.CurrentBranch()
	if current != "big-feature" {
		t.Errorf("expected to be on big-feature, got %s", current)
	}
}

func TestSplitExecute(t *testing.T) {
	resetSplitFlags()
	dir := splitTestRepo(t)

	err := RunSplit([]string{"-y", "--base", "main", "--stack", "split-test"})
	if err != nil {
		t.Fatalf("split: %v", err)
	}

	// Verify stack was created
	s, err := stack.LoadStack(dir, "split-test")
	if err != nil {
		t.Fatalf("cannot load stack: %v", err)
	}

	if len(s.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(s.Nodes))
	}

	if s.Base != "main" {
		t.Errorf("stack base: got %q, want %q", s.Base, "main")
	}

	// Verify branches exist
	for _, node := range s.Nodes {
		if !gitpkg.BranchExists(node.Branch) {
			t.Errorf("branch %q does not exist", node.Branch)
		}
	}

	// Verify tree identity: last stack branch should have same tree as big-feature
	lastBranch := s.Nodes[len(s.Nodes)-1].Branch
	diff, err := gitpkg.DiffFull("big-feature", lastBranch)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if diff != "" {
		t.Errorf("tree differs from original branch:\n%s", diff)
	}

	// Verify we're back on big-feature
	current, _ := gitpkg.CurrentBranch()
	if current != "big-feature" {
		t.Errorf("expected to be on big-feature, got %s", current)
	}
}

func TestSplitWithParts(t *testing.T) {
	resetSplitFlags()
	dir := splitTestRepo(t)

	err := RunSplit([]string{"-y", "--base", "main", "--stack", "parts-test", "--parts", "2"})
	if err != nil {
		t.Fatalf("split --parts 2: %v", err)
	}

	s, err := stack.LoadStack(dir, "parts-test")
	if err != nil {
		t.Fatalf("cannot load stack: %v", err)
	}

	if len(s.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(s.Nodes))
	}

	// Verify tree identity
	lastBranch := s.Nodes[len(s.Nodes)-1].Branch
	diff, err := gitpkg.DiffFull("big-feature", lastBranch)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if diff != "" {
		t.Errorf("tree differs from original branch:\n%s", diff)
	}
}

func TestSplitStackTopology(t *testing.T) {
	resetSplitFlags()
	dir := splitTestRepo(t)

	err := RunSplit([]string{"-y", "--base", "main", "--stack", "topo-test"})
	if err != nil {
		t.Fatalf("split: %v", err)
	}

	s, err := stack.LoadStack(dir, "topo-test")
	if err != nil {
		t.Fatalf("cannot load stack: %v", err)
	}

	// Verify parent chain: base → node0 → node1 → node2
	if s.ParentBranch(s.Nodes[0].Branch) != "main" {
		t.Errorf("node 0 parent: got %q, want %q", s.ParentBranch(s.Nodes[0].Branch), "main")
	}
	if s.ParentBranch(s.Nodes[1].Branch) != s.Nodes[0].Branch {
		t.Errorf("node 1 parent: got %q, want %q", s.ParentBranch(s.Nodes[1].Branch), s.Nodes[0].Branch)
	}
	if s.ParentBranch(s.Nodes[2].Branch) != s.Nodes[1].Branch {
		t.Errorf("node 2 parent: got %q, want %q", s.ParentBranch(s.Nodes[2].Branch), s.Nodes[1].Branch)
	}
}

func TestDirsOverlap(t *testing.T) {
	tests := []struct {
		name   string
		a, b   []string
		expect bool
	}{
		{
			name:   "same directory",
			a:      []string{"src/foo.go"},
			b:      []string{"src/bar.go"},
			expect: true,
		},
		{
			name:   "different directories",
			a:      []string{"src/foo.go"},
			b:      []string{"cmd/bar.go"},
			expect: false,
		},
		{
			name:   "partial overlap",
			a:      []string{"src/foo.go", "lib/baz.go"},
			b:      []string{"src/bar.go", "test/qux.go"},
			expect: true,
		},
		{
			name:   "root files",
			a:      []string{"Makefile"},
			b:      []string{"README.md"},
			expect: true, // both in "."
		},
		{
			name:   "nested same",
			a:      []string{"internal/git/foo.go"},
			b:      []string{"internal/git/bar.go"},
			expect: true,
		},
		{
			name:   "nested different",
			a:      []string{"internal/git/foo.go"},
			b:      []string{"internal/stack/bar.go"},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dirsOverlap(tt.a, tt.b)
			if got != tt.expect {
				t.Errorf("dirsOverlap(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.expect)
			}
		})
	}
}

func TestDeriveTitle(t *testing.T) {
	tests := []struct {
		name    string
		commits []commitInfo
		want    string
	}{
		{
			name: "single directory",
			commits: []commitInfo{
				{Files: []string{"internal/git/foo.go", "internal/git/bar.go"}},
			},
			want: "git",
		},
		{
			name: "most common wins",
			commits: []commitInfo{
				{Files: []string{"cmd/a.go", "cmd/b.go", "internal/stack/c.go"}},
			},
			want: "cmd",
		},
		{
			name: "root files",
			commits: []commitInfo{
				{Files: []string{"Makefile", "README.md"}},
			},
			want: "root",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveTitle(tt.commits)
			if got != tt.want {
				t.Errorf("deriveTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSplitNoPush(t *testing.T) {
	resetSplitFlags()
	dir := splitTestRepo(t)

	err := RunSplit([]string{"-y", "--base", "main", "--stack", "nopush-test", "--no-push"})
	if err != nil {
		t.Fatalf("split --no-push: %v", err)
	}

	// Verify stack was created
	s, err := stack.LoadStack(dir, "nopush-test")
	if err != nil {
		t.Fatalf("cannot load stack: %v", err)
	}

	if len(s.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(s.Nodes))
	}

	// No PRs should be set (no push, no PR creation)
	for _, node := range s.Nodes {
		if node.PR != 0 {
			t.Errorf("branch %q has PR %d, expected 0", node.Branch, node.PR)
		}
	}

	// Tree identity should still hold
	lastBranch := s.Nodes[len(s.Nodes)-1].Branch
	diff, err := gitpkg.DiffFull("big-feature", lastBranch)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if diff != "" {
		t.Errorf("tree differs from original branch:\n%s", diff)
	}

	// Should be back on original branch
	current, _ := gitpkg.CurrentBranch()
	if current != "big-feature" {
		t.Errorf("expected to be on big-feature, got %s", current)
	}
}

func TestBuildSplitPRBody(t *testing.T) {
	s := &stack.Stack{
		StackID: "my-feature",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "my-feature/1-schema", Status: "open"},
			{Branch: "my-feature/2-api", Status: "open"},
			{Branch: "my-feature/3-ui", Status: "open"},
		},
	}

	body := buildSplitPRBody(s, 1, "big-feature")

	if !strings.Contains(body, "my-feature") {
		t.Error("body should contain stack name")
	}
	if !strings.Contains(body, "big-feature") {
		t.Error("body should reference original branch")
	}
	if !strings.Contains(body, "PR 2 of 3") {
		t.Errorf("body should say PR 2 of 3, got:\n%s", body)
	}
	if !strings.Contains(body, "Base: `my-feature/1-schema`") {
		t.Errorf("body should reference parent branch, got:\n%s", body)
	}
}

func TestSanitizeBranchComponent(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"git", "git"},
		{"My Feature", "my-feature"},
		{"internal/git", "internal-git"},
		{"foo_bar", "foo-bar"},
		{"", "changes"},
		{"---", "changes"},
		{"Hello World!", "hello-world"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeBranchComponent(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeBranchComponent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
