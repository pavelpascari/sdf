package render

import "testing"

func TestAnsiSequences(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"Cup(3,1)", Cup(3, 1), "\x1b[3;1H"},
		{"Cup(1,1)", Cup(1, 1), "\x1b[1;1H"},
		{"Cup(10,20)", Cup(10, 20), "\x1b[10;20H"},
		{"El", El(), "\x1b[2K"},
		{"HideCursor", HideCursor(), "\x1b[?25l"},
		{"ShowCursor", ShowCursor(), "\x1b[?25h"},
		{"CursorUp(5)", CursorUp(5), "\x1b[5A"},
		{"CursorUp(1)", CursorUp(1), "\x1b[1A"},
		{"CursorDown(2)", CursorDown(2), "\x1b[2B"},
		{"CursorDown(10)", CursorDown(10), "\x1b[10B"},
		{"CarriageReturn", CarriageReturn(), "\r"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}
