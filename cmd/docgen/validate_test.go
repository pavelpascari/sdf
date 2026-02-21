package main_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pavelpascari/sdf/cmd"
	"github.com/spf13/cobra"
)

// TestMDXCommandReferences scans all .mdx doc files and validates that every
// `sdf <command>` reference points to a real command in the Cobra tree.
func TestMDXCommandReferences(t *testing.T) {
	root := cmd.RootCmd()
	knownCommands := collectCommandNames(root)

	// Find the docs directory relative to this test file
	docsDir := filepath.Join("..", "..", "www", "src", "content", "docs")
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		t.Skipf("cannot read docs dir %s: %v", docsDir, err)
		return
	}

	// Match `sdf <word>` inside backtick-delimited code spans
	codeSpanPattern := regexp.MustCompile("`[^`]*\\bsdf\\s+([a-z][-a-z]*)(?:\\s[^`]*)?`")

	// Match sdf <word> at the start of a line inside code blocks (``` ... ```)
	barePattern := regexp.MustCompile(`^\s*sdf\s+([a-z][-a-z]*)`)

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".mdx") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(docsDir, e.Name()))
		if err != nil {
			t.Errorf("cannot read %s: %v", e.Name(), err)
			continue
		}

		lines := strings.Split(string(data), "\n")
		inCodeBlock := false
		for lineNum, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inCodeBlock = !inCodeBlock
				continue
			}

			if inCodeBlock {
				// Inside a code block, match bare sdf commands
				matches := barePattern.FindAllStringSubmatch(line, -1)
				for _, m := range matches {
					cmdName := m[1]
					if !knownCommands[cmdName] {
						t.Errorf("%s:%d references unknown command `sdf %s`",
							e.Name(), lineNum+1, cmdName)
					}
				}
			} else {
				// Outside code blocks, match inline code spans
				matches := codeSpanPattern.FindAllStringSubmatch(line, -1)
				for _, m := range matches {
					cmdName := m[1]
					if !knownCommands[cmdName] {
						t.Errorf("%s:%d references unknown command `sdf %s`",
							e.Name(), lineNum+1, cmdName)
					}
				}
			}
		}
	}
}

func collectCommandNames(parent *cobra.Command) map[string]bool {
	names := make(map[string]bool)
	for _, c := range parent.Commands() {
		names[c.Name()] = true
		// Also collect subcommand names
		for _, sub := range c.Commands() {
			names[sub.Name()] = true
		}
	}
	// "help" is a built-in Cobra command that is always available
	names["help"] = true
	return names
}
