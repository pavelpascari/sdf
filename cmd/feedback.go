package cmd

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/ui"
	"github.com/spf13/cobra"
)

const defaultFeedbackIssue = 153

var feedbackCmd = &cobra.Command{
	Use:         "feedback",
	Short:       "Send product feedback to the project issue tracker",
	Annotations: map[string]string{"category": "utility"},
	RunE:        runFeedback,
}

func init() {
	rootCmd.AddCommand(feedbackCmd)
	feedbackCmd.Flags().Int("score", -1, "satisfaction score from 0 to 5")
	feedbackCmd.Flags().String("note", "", "optional feedback note")
}

func runFeedback(cmd *cobra.Command, args []string) error {
	if !ghpkg.Available() {
		return fmt.Errorf("gh CLI is required to send feedback — install it from https://cli.github.com")
	}

	issueNumber := feedbackIssueNumber()
	if issueNumber <= 0 {
		return fmt.Errorf("invalid feedback issue number: %d", issueNumber)
	}

	scoreFlag, _ := cmd.Flags().GetInt("score")
	note, _ := cmd.Flags().GetString("note")

	score := scoreFlag
	if score == -1 {
		options := []huh.Option[string]{
			huh.NewOption("5 - Excellent", "5"),
			huh.NewOption("4 - Good", "4"),
			huh.NewOption("3 - OK", "3"),
			huh.NewOption("2 - Needs work", "2"),
			huh.NewOption("1 - Poor", "1"),
			huh.NewOption("0 - Unusable", "0"),
		}
		choice := ui.Select("How satisfied are you with sdf?", options)
		if choice == "" {
			fmt.Println("Aborted.")
			return nil
		}
		parsed, err := strconv.Atoi(choice)
		if err != nil {
			return fmt.Errorf("invalid score selected: %w", err)
		}
		score = parsed

		if note == "" {
			_ = huh.NewInput().
				Title("Optional feedback note").
				Placeholder("What worked well? What should improve?").
				Value(&note).
				Run()
		}
	}

	if score < 0 || score > 5 {
		return fmt.Errorf("score must be between 0 and 5")
	}

	comment := buildFeedbackComment(score, note, version)
	if err := ghpkg.IssueComment(issueNumber, comment); err != nil {
		return fmt.Errorf("cannot post feedback comment: %w", err)
	}

	fmt.Printf("%s Thanks for the feedback! Posted to issue #%d.\n", ui.SymOK, issueNumber)
	return nil
}

func feedbackIssueNumber() int {
	v := os.Getenv("SDF_FEEDBACK_ISSUE")
	if v == "" {
		return defaultFeedbackIssue
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return -1
	}
	return n
}

func buildFeedbackComment(score int, note, ver string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Feedback score: %d/5\n", score)
	fmt.Fprintf(&b, "Version: %s\n", ver)
	fmt.Fprintf(&b, "Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	if strings.TrimSpace(note) != "" {
		b.WriteString("\nComment:\n")
		b.WriteString(strings.TrimSpace(note))
		b.WriteString("\n")
	}
	return b.String()
}
