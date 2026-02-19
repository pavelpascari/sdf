package cmd

import (
	"fmt"
	"os"
	"os/exec"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	ctxpkg "github.com/pavelpascari/sdf/internal/context"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

func RunContext(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: sdf context <show|edit|update>")
		os.Exit(1)
	}

	subcmd := args[0]
	switch subcmd {
	case "show":
		return runContextShow()
	case "edit":
		return runContextEdit()
	case "update":
		return runContextUpdate()
	default:
		return fmt.Errorf("unknown context subcommand: %s (use show, edit, or update)", subcmd)
	}
}

func runContextShow() error {
	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	s, err := resolveStack(root, "")
	if err != nil {
		return err
	}

	branch, err := gitpkg.CurrentBranch()
	if err != nil {
		return err
	}

	if s.FindNode(branch) == nil {
		return fmt.Errorf("branch %q is not in stack %q", branch, s.StackID)
	}

	assembled, err := ctxpkg.Assemble(root, s, branch)
	if err != nil {
		return err
	}

	fmt.Print(assembled)
	return nil
}

func runContextEdit() error {
	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	s, err := resolveStack(root, "")
	if err != nil {
		return err
	}

	branch, err := gitpkg.CurrentBranch()
	if err != nil {
		return err
	}

	if s.FindNode(branch) == nil {
		return fmt.Errorf("branch %q is not in stack %q", branch, s.StackID)
	}

	docPath := ctxpkg.DocPath(root, branch)

	// Ensure doc exists
	if !ctxpkg.Exists(root, branch) {
		parent := s.ParentBranch(branch)
		if err := ctxpkg.CreateStub(root, branch, parent); err != nil {
			return fmt.Errorf("cannot create context doc: %w", err)
		}
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	cmd := exec.Command(editor, docPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}

	fmt.Printf("Context doc updated: %s\n", docPath)
	return nil
}

func runContextUpdate() error {
	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	s, err := resolveStack(root, "")
	if err != nil {
		return err
	}

	branch, err := gitpkg.CurrentBranch()
	if err != nil {
		return err
	}

	node := s.FindNode(branch)
	if node == nil {
		return fmt.Errorf("branch %q is not in stack %q", branch, s.StackID)
	}

	if !claudepkg.Available() {
		return fmt.Errorf("claude CLI is required for context update — install it first")
	}

	// Get current context
	currentCtx, _ := ctxpkg.Read(root, branch)

	// Get diff from parent
	parent := s.ParentBranch(branch)
	diff, _ := gitpkg.DiffFull(parent, branch)

	// Build prompt
	prompt := fmt.Sprintf(`You are updating the context document for branch %q in a stacked diff workflow.

=== CURRENT CONTEXT DOC ===
%s

=== DIFF FROM PARENT (%s) ===
%s

Rewrite the context document to accurately reflect the current state of this branch.
Keep the same markdown structure (Intent, Constraints from upstream, Decisions made here, Open questions).
Be concise and precise. Output ONLY the markdown content for the context doc, nothing else.`, branch, currentCtx, parent, diff)

	sessionName := claudepkg.SanitizeSessionName("context-update", branch)

	fmt.Printf("Asking Claude to update context doc for %s...\n", branch)
	output, err := claudepkg.RunPrompt(sessionName, prompt)
	if err != nil {
		return fmt.Errorf("claude failed: %w", err)
	}

	// Write updated context
	docPath := ctxpkg.DocPath(root, branch)
	if err := os.WriteFile(docPath, []byte(output+"\n"), 0644); err != nil {
		return fmt.Errorf("cannot write context doc: %w", err)
	}

	fmt.Printf("Context doc updated: %s\n", docPath)
	return nil
}
