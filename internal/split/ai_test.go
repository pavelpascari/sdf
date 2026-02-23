package split

import (
	"fmt"
	"strings"
	"testing"
)

func TestBuildPrompt(t *testing.T) {
	prompt := BuildPrompt("feature/big-change", "main")

	if !strings.Contains(prompt, "feature/big-change") {
		t.Error("prompt should contain source branch")
	}
	if !strings.Contains(prompt, "main") {
		t.Error("prompt should contain base branch")
	}
	if !strings.Contains(prompt, "git diff") {
		t.Error("prompt should instruct Claude to use git diff")
	}
	if !strings.Contains(prompt, "yaml") {
		t.Error("prompt should show expected YAML format")
	}
	if !strings.Contains(prompt, "layers:") {
		t.Error("prompt should show layers structure")
	}
}

func TestBuildRetryPrompt(t *testing.T) {
	errs := []error{
		fmt.Errorf("file %q is not assigned to any layer", "missing.go"),
		fmt.Errorf("file %q appears in both %q and %q", "dup.go", "a", "b"),
	}

	prompt := BuildRetryPrompt(errs)

	if !strings.Contains(prompt, "missing.go") {
		t.Error("retry prompt should mention missing file")
	}
	if !strings.Contains(prompt, "dup.go") {
		t.Error("retry prompt should mention duplicate file")
	}
}
