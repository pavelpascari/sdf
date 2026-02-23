// Package ui provides shared terminal styling and interactive prompts
// using the Charm ecosystem (lipgloss, huh).
package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Semantic colors for terminal output.
var (
	Green   = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // success, merged
	Cyan    = lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // active, current
	Yellow  = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // warning
	Red     = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // error, failed
	Gray    = lipgloss.NewStyle().Foreground(lipgloss.Color("8")) // muted info
	Magenta = lipgloss.NewStyle().Foreground(lipgloss.Color("5")) // PR numbers
	Bold    = lipgloss.NewStyle().Bold(true)                      // branch names
)

// Symbols with semantic styling baked in.
var (
	SymOK       = Green.Render("✓")
	SymFail     = Red.Render("✗")
	SymConflict = Yellow.Render("⚡")
	SymWarn     = Yellow.Render("⚠")
	SymPlan     = Cyan.Render("→")
)

// PR renders a PR number like "#42" in magenta.
func PR(n int) string {
	return Magenta.Render(fmt.Sprintf("#%d", n))
}

// Branch renders a branch name in bold.
func Branch(name string) string {
	return Bold.Render(name)
}
