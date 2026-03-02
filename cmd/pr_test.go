package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/testutil"
)

func TestRunPR_EmptyBranchAheadOfBase(t *testing.T) {
	dir := newTestRepo(t)

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
	}

	git("checkout", "-b", "test-stack/empty")

	if err := stack.Init(dir, "test-stack", "main"); err != nil {
		t.Fatal(err)
	}
	s, err := stack.LoadStack(dir, "test-stack")
	if err != nil {
		t.Fatal(err)
	}
	revParse := exec.Command("git", "rev-parse", "main")
	revParse.Dir = dir
	mainTip, err := revParse.Output()
	if err != nil {
		t.Fatal(err)
	}
	s.Nodes = []stack.Node{
		{Branch: "test-stack/empty", Status: "open", BaseTip: strings.TrimSpace(string(mainTip))},
	}
	if err := stack.Save(dir, s); err != nil {
		t.Fatal(err)
	}

	fakeDir := t.TempDir()
	fake := testutil.GHFakeBin(t, fakeDir)
	testutil.SetBinary(t, &ghpkg.Binary, fake)

	err = RunPR(nil)
	if err == nil {
		t.Fatal("expected no-commits-ahead error")
	}
	if !strings.Contains(err.Error(), "has no commits ahead of main") {
		t.Fatalf("unexpected error: %v", err)
	}

	log := testutil.ReadLog(t, fakeDir, "gh")
	if len(log) != 0 {
		t.Fatalf("expected no gh calls for empty branch, got %v", log)
	}
}

func TestLoadPRTemplate_FromRootFile(t *testing.T) {
	dir := newTestRepo(t)
	path := filepath.Join(dir, "PULL_REQUEST_TEMPLATE.md")
	if err := os.WriteFile(path, []byte("Template body"), 0644); err != nil {
		t.Fatal(err)
	}

	tmpl := loadPRTemplate(dir)
	if !strings.Contains(tmpl, "Template body") {
		t.Fatalf("unexpected template content: %q", tmpl)
	}
}

func TestLoadPRTemplate_FromGithubDir(t *testing.T) {
	dir := newTestRepo(t)

	// .github/pull_request_template.md takes precedence
	ghDir := filepath.Join(dir, ".github")
	os.MkdirAll(ghDir, 0755)
	os.WriteFile(filepath.Join(ghDir, "pull_request_template.md"), []byte("GitHub template"), 0644)

	tmpl := loadPRTemplate(dir)
	if !strings.Contains(tmpl, "GitHub template") {
		t.Fatalf("unexpected template content: %q", tmpl)
	}
}

func TestLoadPRTemplate_FromTemplateDirectory(t *testing.T) {
	dir := newTestRepo(t)
	templateDir := filepath.Join(dir, ".github", "PULL_REQUEST_TEMPLATE")
	os.MkdirAll(templateDir, 0755)
	os.WriteFile(filepath.Join(templateDir, "feature.md"), []byte("Feature template"), 0644)

	tmpl := loadPRTemplate(dir)
	if !strings.Contains(tmpl, "Feature template") {
		t.Fatalf("unexpected template content: %q", tmpl)
	}
}

func TestLoadPRTemplate_Precedence(t *testing.T) {
	dir := newTestRepo(t)

	// Create both .github/pull_request_template.md and root PULL_REQUEST_TEMPLATE.md
	ghDir := filepath.Join(dir, ".github")
	os.MkdirAll(ghDir, 0755)
	os.WriteFile(filepath.Join(ghDir, "pull_request_template.md"), []byte("github dir wins"), 0644)
	os.WriteFile(filepath.Join(dir, "PULL_REQUEST_TEMPLATE.md"), []byte("root loses"), 0644)

	tmpl := loadPRTemplate(dir)
	if !strings.Contains(tmpl, "github dir wins") {
		t.Fatalf("expected .github/ to take precedence, got: %q", tmpl)
	}
}

func TestLoadPRTemplate_Missing(t *testing.T) {
	dir := newTestRepo(t)

	tmpl := loadPRTemplate(dir)
	if tmpl != "" {
		t.Fatalf("expected empty template, got: %q", tmpl)
	}
}
