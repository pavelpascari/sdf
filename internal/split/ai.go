package split

import (
	"fmt"
	"io"
	"strings"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
)

// MaxRetries is the number of times to retry Claude on validation failure.
const MaxRetries = 3

// AnalysisResult holds the output of a Claude analysis.
type AnalysisResult struct {
	Plan      *Plan
	SessionID string
}

// BuildPrompt constructs the instruction prompt for Claude to analyze a branch.
func BuildPrompt(fromBranch, base string) string {
	return fmt.Sprintf(`You are analyzing a git branch to split it into a stack of smaller, reviewable PRs.

SOURCE BRANCH: %s
BASE BRANCH: %s

Your task:
1. Explore the branch using git commands (git diff --stat, git log, git show, etc.)
2. Understand what changed and why
3. Group the changed files into coherent layers ordered from foundational to dependent
4. Return a YAML split plan

Rules:
- Every changed file must appear in exactly one layer
- Order layers so earlier layers don't depend on later ones
- Aim for <500 lines of diff per layer when practical
- Group test files with the production code they test
- Use short, descriptive kebab-case layer names
- A file can only belong to one layer. If a file has changes for multiple concerns, put it in the earliest layer that needs it

Return ONLY this YAML (wrapped in `+"`"+`yaml fences):

`+"```"+`yaml
layers:
  - name: <kebab-case-name>
    description: "<one-line summary of what this layer does>"
    files:
      - <path/to/file>
      - ...
`+"```", fromBranch, base)
}

// BuildRetryPrompt constructs a follow-up prompt describing validation errors.
func BuildRetryPrompt(errs []error) string {
	return ValidationSummary(errs)
}

// Analyze invokes Claude to analyze a branch and produce a split plan.
// It streams Claude's progress to the display writer, retries up to MaxRetries
// times on validation failure, and returns the validated plan with session ID.
func Analyze(fromBranch, base string, display io.Writer) (*AnalysisResult, error) {
	// Get the list of changed files for validation
	changedFiles, err := gitpkg.DiffNameOnly(base, fromBranch)
	if err != nil {
		return nil, fmt.Errorf("cannot list changed files: %w", err)
	}

	prompt := BuildPrompt(fromBranch, base)
	name := "split-analysis"

	// First attempt
	sr, err := claudepkg.RunPromptStreaming(name, prompt, display)
	if err != nil {
		return nil, fmt.Errorf("claude analysis failed: %w", err)
	}

	sessionID := sr.SessionID

	for attempt := 0; attempt <= MaxRetries; attempt++ {
		plan, parseErr := ParsePlan(sr.Result)
		if parseErr != nil {
			if attempt == MaxRetries {
				return nil, fmt.Errorf("claude returned invalid plan after %d attempts: %w", MaxRetries+1, parseErr)
			}
			retryPrompt := fmt.Sprintf("Your response could not be parsed: %s\n\nPlease return a valid YAML plan.", parseErr.Error())
			fmt.Fprintf(display, "\n⚠ Parse error, retrying (%d/%d)...\n", attempt+1, MaxRetries)
			sr, err = claudepkg.RunPromptStreamingResume(name, sessionID, retryPrompt, display)
			if err != nil {
				return nil, fmt.Errorf("claude retry failed: %w", err)
			}
			continue
		}

		validationErrs := ValidatePlan(plan, changedFiles)
		if len(validationErrs) == 0 {
			return &AnalysisResult{Plan: plan, SessionID: sessionID}, nil
		}

		if attempt == MaxRetries {
			var errMsgs []string
			for _, e := range validationErrs {
				errMsgs = append(errMsgs, e.Error())
			}
			return nil, fmt.Errorf("plan validation failed after %d attempts:\n  %s",
				MaxRetries+1, strings.Join(errMsgs, "\n  "))
		}

		retryPrompt := BuildRetryPrompt(validationErrs)
		fmt.Fprintf(display, "\n⚠ Validation failed, retrying (%d/%d)...\n", attempt+1, MaxRetries)
		sr, err = claudepkg.RunPromptStreamingResume(name, sessionID, retryPrompt, display)
		if err != nil {
			return nil, fmt.Errorf("claude retry failed: %w", err)
		}
	}

	// Unreachable, but satisfy the compiler
	return nil, fmt.Errorf("analysis failed")
}
