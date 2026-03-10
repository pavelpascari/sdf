package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/spf13/cobra"
)

func TestCompleteStackNames(t *testing.T) {
	dir := newTestRepo(t)

	// Create two stacks
	if err := stack.Init(dir, "feature-a", "main"); err != nil {
		t.Fatal(err)
	}
	if err := stack.Init(dir, "feature-b", "main"); err != nil {
		t.Fatal(err)
	}

	names, directive := completeStackNames(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp, got %d", directive)
	}

	if len(names) != 2 {
		t.Fatalf("expected 2 stack names, got %d: %v", len(names), names)
	}

	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["feature-a"] || !found["feature-b"] {
		t.Errorf("expected feature-a and feature-b, got %v", names)
	}
}

func TestCompleteStackBranches(t *testing.T) {
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

	// Create local branches to be discovered by completion.
	git("checkout", "-b", "my-feature/db-schema")
	git("checkout", "-b", "my-feature/api")
	git("checkout", "main")

	// Create a stack with two branches
	s := &stack.Stack{
		StackID: "my-feature",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "my-feature/db-schema", Status: "open"},
			{Branch: "my-feature/api", Status: "open"},
			{Branch: "my-feature/merged-one", Status: "merged"},
		},
	}
	if err := stack.Init(dir, "my-feature", "main"); err != nil {
		t.Fatal(err)
	}
	if err := stack.Save(dir, s); err != nil {
		t.Fatal(err)
	}

	branches, directive := completeStackBranches(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp, got %d", directive)
	}

	// Should only include non-merged branches
	if len(branches) != 2 {
		t.Fatalf("expected 2 branches, got %d: %v", len(branches), branches)
	}

	found := map[string]bool{}
	for _, b := range branches {
		found[b] = true
	}
	if !found["my-feature/db-schema"] || !found["my-feature/api"] {
		t.Errorf("expected db-schema and api branches, got %v", branches)
	}
	if found["my-feature/merged-one"] {
		t.Error("merged branches should not be suggested")
	}
}

func TestCompleteStackBranches_ExcludesDeletedBranches(t *testing.T) {
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

	git("checkout", "-b", "my-feature/live")
	git("checkout", "main")
	git("branch", "-D", "my-feature/live")

	s := &stack.Stack{
		StackID: "my-feature",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "my-feature/live", Status: "open"},
		},
	}
	if err := stack.Init(dir, "my-feature", "main"); err != nil {
		t.Fatal(err)
	}
	if err := stack.Save(dir, s); err != nil {
		t.Fatal(err)
	}

	branches, _ := completeStackBranches(nil, nil, "")
	if len(branches) != 0 {
		t.Fatalf("expected no completions for deleted branch, got %v", branches)
	}
}

func TestCompleteStackBranches_NoArgsAfterFirst(t *testing.T) {
	// When one arg is already provided, should return no suggestions
	branches, _ := completeStackBranches(nil, []string{"some-branch"}, "")
	if branches != nil {
		t.Errorf("expected nil completions after first arg, got %v", branches)
	}
}

func TestCompleteConfigKeys(t *testing.T) {
	// First arg: should return config keys
	keys, directive := completeConfigKeys(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp, got %d", directive)
	}
	if len(keys) != len(configKeys) {
		t.Errorf("expected %d config keys, got %d", len(configKeys), len(keys))
	}

	// Second arg for bool key: should return true/false
	values, _ := completeConfigKeys(nil, []string{"branch_prefix.enabled"}, "")
	if len(values) != 2 {
		t.Fatalf("expected 2 values for bool key, got %d: %v", len(values), values)
	}
	found := map[string]bool{}
	for _, v := range values {
		found[v] = true
	}
	if !found["true"] || !found["false"] {
		t.Errorf("expected true/false, got %v", values)
	}

	// Second arg for separator key: should return / and -
	values, _ = completeConfigKeys(nil, []string{"branch_prefix.separator"}, "")
	if len(values) != 2 {
		t.Fatalf("expected 2 values for separator key, got %d: %v", len(values), values)
	}

	// Second arg for string key with no predefined values
	values, _ = completeConfigKeys(nil, []string{"branch_prefix.scope"}, "")
	if values != nil {
		t.Errorf("expected nil for free-text key, got %v", values)
	}
}

func TestCompleteMergeMethods(t *testing.T) {
	methods, directive := completeMergeMethods(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp, got %d", directive)
	}
	if len(methods) != 3 {
		t.Fatalf("expected 3 methods, got %d: %v", len(methods), methods)
	}
	found := map[string]bool{}
	for _, m := range methods {
		found[m] = true
	}
	if !found["squash"] || !found["merge"] || !found["rebase"] {
		t.Errorf("expected squash/merge/rebase, got %v", methods)
	}
}

func TestDetectShell(t *testing.T) {
	tests := []struct {
		env  string
		want string
	}{
		{"/bin/bash", "bash"},
		{"/usr/bin/zsh", "zsh"},
		{"/usr/local/bin/fish", "fish"},
		{"/bin/zsh", "zsh"},
		{"", ""},
	}

	for _, tt := range tests {
		orig := os.Getenv("SHELL")
		os.Setenv("SHELL", tt.env)
		got := detectShell()
		os.Setenv("SHELL", orig)

		if got != tt.want {
			t.Errorf("detectShell() with SHELL=%q: got %q, want %q", tt.env, got, tt.want)
		}
	}
}

func TestCompletionBashOutput(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	if err := rootCmd.GenBashCompletionV2(&buf, true); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "bash") && !strings.Contains(output, "__sdf") && !strings.Contains(output, "complete") {
		t.Error("bash completion output does not look like a bash completion script")
	}
}

func TestCompletionZshOutput(t *testing.T) {
	var buf bytes.Buffer
	if err := rootCmd.GenZshCompletion(&buf); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "compdef") && !strings.Contains(output, "zsh") {
		t.Error("zsh completion output does not look like a zsh completion script")
	}
}

func TestCompletionFishOutput(t *testing.T) {
	var buf bytes.Buffer
	if err := rootCmd.GenFishCompletion(&buf, true); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "complete") {
		t.Error("fish completion output does not look like a fish completion script")
	}
}

func TestInstallBashCompletion(t *testing.T) {
	// Use a temp dir as HOME
	tmpHome := t.TempDir()
	orig := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", orig) })

	if err := installBashCompletion(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(tmpHome, ".local", "share", "bash-completion", "completions", "sdf")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("completion file not created: %v", err)
	}
	if len(data) == 0 {
		t.Error("completion file is empty")
	}
}

func TestInstallFishCompletion(t *testing.T) {
	tmpHome := t.TempDir()
	orig := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", orig) })

	if err := installFishCompletion(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(tmpHome, ".config", "fish", "completions", "sdf.fish")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("completion file not created: %v", err)
	}
	if len(data) == 0 {
		t.Error("completion file is empty")
	}
}

func TestInstallZshCompletion(t *testing.T) {
	tmpHome := t.TempDir()
	orig := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", orig) })

	if err := installZshCompletion(); err != nil {
		t.Fatal(err)
	}

	// Check completion file was created
	path := filepath.Join(tmpHome, ".zsh", "completions", "_sdf")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("completion file not created: %v", err)
	}
	if len(data) == 0 {
		t.Error("completion file is empty")
	}

	// Check .zshrc was updated with fpath
	zshrc := filepath.Join(tmpHome, ".zshrc")
	rcData, err := os.ReadFile(zshrc)
	if err != nil {
		t.Fatalf("zshrc not created: %v", err)
	}
	if !strings.Contains(string(rcData), "fpath=") {
		t.Error("zshrc does not contain fpath entry")
	}
	if !strings.Contains(string(rcData), "compinit") {
		t.Error("zshrc does not contain compinit")
	}
}

func TestInstallZshCompletion_ExistingFpath(t *testing.T) {
	tmpHome := t.TempDir()
	orig := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", orig) })

	// Pre-create .zshrc with fpath already configured
	dir := filepath.Join(tmpHome, ".zsh", "completions")
	existing := "# existing config\nfpath=(" + dir + " $fpath)\nautoload -Uz compinit && compinit\n"
	if err := os.WriteFile(filepath.Join(tmpHome, ".zshrc"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := installZshCompletion(); err != nil {
		t.Fatal(err)
	}

	// .zshrc should not be duplicated
	rcData, _ := os.ReadFile(filepath.Join(tmpHome, ".zshrc"))
	count := strings.Count(string(rcData), "fpath=")
	if count != 1 {
		t.Errorf("expected 1 fpath entry, found %d", count)
	}
}

func TestCompleteGitBranches(t *testing.T) {
	newTestRepo(t)

	branches, directive := completeGitBranches(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected ShellCompDirectiveNoFileComp, got %d", directive)
	}

	// Should have at least "main"
	found := false
	for _, b := range branches {
		if b == "main" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected main branch in completions, got %v", branches)
	}
}
