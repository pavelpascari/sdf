package render

import "testing"

func TestAnsiSequences(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"El", El(), "\x1b[2K"},
		{"HideCursor", HideCursor(), "\x1b[?25l"},
		{"ShowCursor", ShowCursor(), "\x1b[?25h"},
		{"CursorUp(5)", CursorUp(5), "\x1b[5A"},
		{"CursorUp(1)", CursorUp(1), "\x1b[1A"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}
