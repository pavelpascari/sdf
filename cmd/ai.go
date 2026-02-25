package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/pavelpascari/sdf/internal/ai"
	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	"github.com/pavelpascari/sdf/internal/ui"
)

var aiCmd = &cobra.Command{
	Use:         "ai",
	Short:       "AI assistant integration commands",
	Annotations: map[string]string{"category": "utility"},
}

var aiIntroCmd = &cobra.Command{
	Use:   "intro",
	Short: "Teach Claude about SDF and save a skill for future sessions",
	Long: `Spawns Claude as a child process, introduces SDF (rules, commands,
workflows), and asks Claude to create a skill file so it remembers how
to use SDF in future sessions. Claude's output streams to the terminal.`,
	Annotations: map[string]string{"category": "utility"},
	RunE:        runAIIntro,
}

func init() {
	rootCmd.AddCommand(aiCmd)
	aiCmd.AddCommand(aiIntroCmd)
}

func runAIIntro(cmd *cobra.Command, args []string) error {
	if !claudepkg.Available() {
		return fmt.Errorf("claude CLI is not installed (run sdf doctor)")
	}

	prompt := ai.BuildIntroPrompt()
	opts := claudepkg.PromptOptions{
		AllowedTools: []string{"Write", "Read", "Bash(mkdir *)"},
	}

	fmt.Println()
	fmt.Printf("  %s Setting up SDF skill for Claude Code...\n", ui.SymPlan)
	fmt.Println()

	_, err := claudepkg.RunPromptStreamingWithOpts("ai-intro", prompt, os.Stdout, opts)
	if err != nil {
		return fmt.Errorf("claude failed: %w", err)
	}

	fmt.Println()
	fmt.Printf("  %s Skill created. Claude Code will load SDF knowledge in future sessions.\n", ui.SymOK)
	return nil
}

