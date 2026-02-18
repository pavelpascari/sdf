package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyResolutions(t *testing.T) {
	t.Run("well-formed output with language and filename", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "main.go")

		output := "Here is the resolved file:\n```go main.go\npackage main\n\nfunc main() {}\n```\nDone."

		err := applyResolutions(output, []string{filePath})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		got, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("failed to read written file: %v", err)
		}

		want := "package main\n\nfunc main() {}\n"
		if string(got) != want {
			t.Errorf("file content mismatch\ngot:  %q\nwant: %q", string(got), want)
		}
	})

	t.Run("single-word fence with just filename", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "config.yaml")

		output := "```config.yaml\nkey: value\nanother: thing\n```"

		err := applyResolutions(output, []string{filePath})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		got, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("failed to read written file: %v", err)
		}

		want := "key: value\nanother: thing\n"
		if string(got) != want {
			t.Errorf("file content mismatch\ngot:  %q\nwant: %q", string(got), want)
		}
	})

	t.Run("multiple files in one output", func(t *testing.T) {
		dir := t.TempDir()
		file1 := filepath.Join(dir, "a.go")
		file2 := filepath.Join(dir, "b.go")

		output := strings.Join([]string{
			"Here are the resolved files:",
			"```go a.go",
			"package a",
			"```",
			"And the second file:",
			"```go b.go",
			"package b",
			"```",
		}, "\n")

		err := applyResolutions(output, []string{file1, file2})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		got1, err := os.ReadFile(file1)
		if err != nil {
			t.Fatalf("failed to read file1: %v", err)
		}
		if string(got1) != "package a\n" {
			t.Errorf("file1 content mismatch\ngot:  %q\nwant: %q", string(got1), "package a\n")
		}

		got2, err := os.ReadFile(file2)
		if err != nil {
			t.Fatalf("failed to read file2: %v", err)
		}
		if string(got2) != "package b\n" {
			t.Errorf("file2 content mismatch\ngot:  %q\nwant: %q", string(got2), "package b\n")
		}
	})

	t.Run("no fenced blocks returns error", func(t *testing.T) {
		output := "I could not resolve the conflicts. Please fix them manually."

		err := applyResolutions(output, []string{"/tmp/anything.go"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "no resolved files found in Claude output") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("basename matching fallback", func(t *testing.T) {
		dir := t.TempDir()
		// Create nested directory structure so the conflicted path is a deep path
		nested := filepath.Join(dir, "path", "to")
		if err := os.MkdirAll(nested, 0755); err != nil {
			t.Fatalf("failed to create nested dirs: %v", err)
		}
		filePath := filepath.Join(nested, "main.go")

		// Claude output uses just the basename "main.go"
		output := "```go main.go\npackage main\n\nfunc resolved() {}\n```"

		err := applyResolutions(output, []string{filePath})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		got, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("failed to read written file: %v", err)
		}

		want := "package main\n\nfunc resolved() {}\n"
		if string(got) != want {
			t.Errorf("file content mismatch\ngot:  %q\nwant: %q", string(got), want)
		}
	})

	t.Run("missing resolution for a conflicted file", func(t *testing.T) {
		dir := t.TempDir()
		file1 := filepath.Join(dir, "found.go")
		file2 := filepath.Join(dir, "missing.go")

		// Only provide resolution for found.go
		output := "```go found.go\npackage found\n```"

		err := applyResolutions(output, []string{file1, file2})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "no resolution found for") {
			t.Errorf("unexpected error message: %v", err)
		}
		if !strings.Contains(err.Error(), "missing.go") {
			t.Errorf("error should mention the missing file, got: %v", err)
		}
	})

	t.Run("fence with no filename is ignored", func(t *testing.T) {
		// A bare ``` opener with no language or filename should not capture content.
		// Since no files get resolved, the function should return an error.
		output := "Here is some code:\n```\npackage main\n```\nThat's it."

		err := applyResolutions(output, []string{"/tmp/anything.go"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "no resolved files found in Claude output") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestApplyResolutionsTableDriven(t *testing.T) {
	// Table-driven tests for parsing edge cases that don't require file I/O
	// verification beyond checking for errors.
	tests := []struct {
		name       string
		output     string
		files      func(dir string) []string
		wantErr    bool
		errContain string
		// wantContent maps basename to expected file content (without trailing newline;
		// the function appends "\n" when writing).
		wantContent map[string]string
	}{
		{
			name:   "language plus path-like filename",
			output: "```typescript src/index.ts\nconsole.log(\"hello\");\n```",
			files: func(dir string) []string {
				return []string{filepath.Join(dir, "src", "index.ts")}
			},
			wantContent: map[string]string{
				"src/index.ts": "console.log(\"hello\");\n",
			},
		},
		{
			name:   "empty file content in fence",
			output: "```go empty.go\n```",
			files: func(dir string) []string {
				return []string{filepath.Join(dir, "empty.go")}
			},
			wantContent: map[string]string{
				"empty.go": "\n",
			},
		},
		{
			name:       "output with only bare fences and no filenames",
			output:     "```\nline 1\nline 2\n```\n```\nmore stuff\n```",
			files:      func(dir string) []string { return []string{filepath.Join(dir, "x.go")} },
			wantErr:    true,
			errContain: "no resolved files found",
		},
		{
			name: "multiple words after fence uses last as filename",
			// "```go formatted main.go" — parts are ["go", "formatted", "main.go"],
			// so currentFile = parts[len(parts)-1] = "main.go"
			output: "```go formatted main.go\npackage main\n```",
			files: func(dir string) []string {
				return []string{filepath.Join(dir, "main.go")}
			},
			wantContent: map[string]string{
				"main.go": "package main\n",
			},
		},
		{
			name:   "content with blank lines preserved",
			output: "```go app.go\npackage app\n\n// blank line above\n\nfunc init() {}\n```",
			files: func(dir string) []string {
				return []string{filepath.Join(dir, "app.go")}
			},
			wantContent: map[string]string{
				"app.go": "package app\n\n// blank line above\n\nfunc init() {}\n",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()

			// Ensure subdirectories exist for any files that need them
			files := tc.files(dir)
			for _, f := range files {
				parent := filepath.Dir(f)
				if err := os.MkdirAll(parent, 0755); err != nil {
					t.Fatalf("failed to create parent dir %s: %v", parent, err)
				}
			}

			err := applyResolutions(tc.output, files)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.errContain != "" && !strings.Contains(err.Error(), tc.errContain) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.errContain)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for basename, wantContent := range tc.wantContent {
				fullPath := filepath.Join(dir, basename)
				got, err := os.ReadFile(fullPath)
				if err != nil {
					t.Fatalf("failed to read %s: %v", fullPath, err)
				}
				if string(got) != wantContent {
					t.Errorf("content mismatch for %s\ngot:  %q\nwant: %q", basename, string(got), wantContent)
				}
			}
		})
	}
}
