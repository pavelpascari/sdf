package cmd

import (
	"os"
	"strings"
	"testing"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/testutil"
	"github.com/spf13/cobra"
)

func TestRunFeedback_PostsIssueComment(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.FakeBin(t, dir, "gh", map[string]string{
		"issue comment": "",
	})
	testutil.SetBinary(t, &ghpkg.Binary, fake)

	t.Setenv("SDF_FEEDBACK_ISSUE", "153")

	c := &cobra.Command{}
	c.Flags().Int("score", -1, "")
	c.Flags().String("note", "", "")
	_ = c.Flags().Set("score", "5")
	_ = c.Flags().Set("note", "Great workflow")

	if err := runFeedback(c, nil); err != nil {
		t.Fatalf("runFeedback failed: %v", err)
	}

	log := testutil.ReadLog(t, dir, "gh")
	if len(log) == 0 {
		t.Fatal("expected at least one gh invocation")
	}
	var matched string
	for _, entry := range log {
		if strings.Contains(entry, "issue comment 153 --body") {
			matched = entry
			break
		}
	}
	if matched == "" {
		t.Fatalf("expected issue comment invocation, got: %v", log)
	}
	if !strings.Contains(matched, "Feedback score: 5/5") {
		t.Fatalf("expected score in gh body, got: %s", matched)
	}
}

func TestFeedbackIssueNumber_DefaultAndInvalid(t *testing.T) {
	_ = os.Unsetenv("SDF_FEEDBACK_ISSUE")
	if got := feedbackIssueNumber(); got != defaultFeedbackIssue {
		t.Fatalf("default feedback issue = %d, want %d", got, defaultFeedbackIssue)
	}

	t.Setenv("SDF_FEEDBACK_ISSUE", "abc")
	if got := feedbackIssueNumber(); got != -1 {
		t.Fatalf("invalid env should return -1, got %d", got)
	}
}

func TestBuildFeedbackComment(t *testing.T) {
	comment := buildFeedbackComment(4, "Nice UX", "0.3.7")
	if !strings.Contains(comment, "Feedback score: 4/5") {
		t.Fatalf("missing score in comment: %q", comment)
	}
	if !strings.Contains(comment, "Version: 0.3.7") {
		t.Fatalf("missing version in comment: %q", comment)
	}
	if !strings.Contains(comment, "Comment:\nNice UX") {
		t.Fatalf("missing note in comment: %q", comment)
	}
}
