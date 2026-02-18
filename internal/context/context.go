// Package context manages context documents for stack branches.
package context

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pavelpascari/sdf/internal/stack"
)

// contextDir returns the path to the context directory.
func contextDir(root string) string {
	return filepath.Join(root, stack.SDFDir, "context")
}

// DocPath returns the path to the context document for a branch.
func DocPath(root, branch string) string {
	// Replace slashes in branch names with directory separators
	return filepath.Join(contextDir(root), branch+".md")
}

// CreateStub creates a stub context document for a new branch.
func CreateStub(root, branch, parentBranch string) error {
	path := DocPath(root, branch)

	// Ensure parent directory exists (for branches like "auth/db-schema")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("cannot create context directory: %w", err)
	}

	stub := fmt.Sprintf(`# %s

## Intent
<!-- Describe the purpose of this branch -->

## Constraints from upstream
<!-- What does this branch depend on from %s? -->

## Decisions made here
<!-- Key design decisions in this branch -->

## Open questions / known debt
<!-- Anything unresolved -->
`, branch, parentBranch)

	return os.WriteFile(path, []byte(stub), 0644)
}

// Read returns the content of a branch's context document.
func Read(root, branch string) (string, error) {
	path := DocPath(root, branch)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("cannot read context doc %s: %w", path, err)
	}
	return string(data), nil
}

// Exists returns true if a context document exists for the branch.
func Exists(root, branch string) bool {
	_, err := os.Stat(DocPath(root, branch))
	return err == nil
}

// Assemble walks the stack from base to the given branch and concatenates
// all ancestor context docs, producing a unified view.
func Assemble(root string, s *stack.Stack, branch string) (string, error) {
	idx := s.NodeIndex(branch)
	if idx < 0 {
		return "", fmt.Errorf("branch %s not found in stack", branch)
	}

	var parts []string
	for i := 0; i <= idx; i++ {
		node := s.Nodes[i]
		content, err := Read(root, node.Branch)
		if err != nil {
			return "", err
		}
		if content == "" {
			continue
		}

		marker := ""
		if node.Branch == branch {
			marker = " ← current"
		}

		part := fmt.Sprintf("[%s%s]\n%s", node.Branch, marker, content)
		parts = append(parts, part)
	}

	header := fmt.Sprintf("=== STACK: %s ===\n\n", s.StackID)
	return header + strings.Join(parts, "\n---\n\n"), nil
}

// BuildConflictPrompt constructs the conflict resolution prompt for Claude.
func BuildConflictPrompt(stackContext, upstreamSummary, branchIntent string, conflictedFiles map[string]string) string {
	var b strings.Builder

	b.WriteString("You are resolving conflicts during a stack rebase.\n\n")
	b.WriteString("=== STACK CONTEXT ===\n")
	b.WriteString(stackContext)
	b.WriteString("\n\n")

	if upstreamSummary != "" {
		b.WriteString("=== UPSTREAM CHANGE SUMMARY ===\n")
		b.WriteString(upstreamSummary)
		b.WriteString("\n\n")
	}

	if branchIntent != "" {
		b.WriteString("=== CURRENT BRANCH INTENT ===\n")
		b.WriteString(branchIntent)
		b.WriteString("\n\n")
	}

	b.WriteString("=== CONFLICTS ===\n")
	for filename, content := range conflictedFiles {
		fmt.Fprintf(&b, "File: %s\n```\n%s\n```\n\n", filename, content)
	}

	b.WriteString("Resolve all conflicts. For each file output the complete resolved content in a\n")
	b.WriteString("fenced code block with the filename: ```<lang> <filename>\n")

	return b.String()
}
