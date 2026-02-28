package render

import "fmt"

const esc = "\x1b"

// El returns the ANSI escape sequence to erase the entire current line.
func El() string { return esc + "[2K" }

// HideCursor returns the ANSI escape sequence to hide the cursor.
func HideCursor() string { return esc + "[?25l" }

// ShowCursor returns the ANSI escape sequence to show the cursor.
func ShowCursor() string { return esc + "[?25h" }

// CursorUp returns the ANSI escape sequence to move the cursor up by n rows.
func CursorUp(n int) string { return fmt.Sprintf("%s[%dA", esc, n) }
