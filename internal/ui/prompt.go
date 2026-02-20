package ui

import "github.com/charmbracelet/huh"

// Confirm shows a Y/n confirmation prompt and returns the user's choice.
func Confirm(title string) bool {
	var yes bool
	err := huh.NewConfirm().
		Title(title).
		Affirmative("Yes").
		Negative("No").
		Value(&yes).
		Run()
	if err != nil {
		return false
	}
	return yes
}

// Select shows an interactive selector and returns the chosen value.
// Options is a slice of huh.Option created with huh.NewOption(label, value).
func Select(title string, options []huh.Option[string]) string {
	var choice string
	err := huh.NewSelect[string]().
		Title(title).
		Options(options...).
		Value(&choice).
		Run()
	if err != nil {
		return ""
	}
	return choice
}
