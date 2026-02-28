package render

import "fmt"

const esc = "\x1b"

// Cup returns the ANSI escape sequence to move the cursor to (row, col).
func Cup(row, col int) string { return fmt.Sprintf("%s[%d;%dH", esc, row, col) }

// El returns the ANSI escape sequence to erase the entire current line.
func El() string { return esc + "[2K" }

// HideCursor returns the ANSI escape sequence to hide the cursor.
func HideCursor() string { return esc + "[?25l" }

// ShowCursor returns the ANSI escape sequence to show the cursor.
func ShowCursor() string { return esc + "[?25h" }

// CursorUp returns the ANSI escape sequence to move the cursor up by n rows.
func CursorUp(n int) string { return fmt.Sprintf("%s[%dA", esc, n) }

// CursorDown returns the ANSI escape sequence to move the cursor down by n rows.
func CursorDown(n int) string { return fmt.Sprintf("%s[%dB", esc, n) }

// CarriageReturn returns a carriage return character.
func CarriageReturn() string { return "\r" }
