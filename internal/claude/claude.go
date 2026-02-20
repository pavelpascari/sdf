// Package claude provides shell-out helpers for the Claude CLI.
package claude

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Available returns true if the claude CLI is installed and accessible.
func Available() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

// Version returns the claude CLI version string.
func Version() (string, error) {
	cmd := exec.Command("claude", "--version")
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		return output, fmt.Errorf("claude --version: %s", output)
	}
	return output, nil
}

// RunPrompt sends a prompt to Claude in print mode and returns the response.
// The sessionName is unused by the current CLI but kept for call-site clarity.
func RunPrompt(sessionName, prompt string) (string, error) {
	cmd := exec.Command("claude", "-p", prompt)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		return output, fmt.Errorf("claude %s: %s", sessionName, output)
	}
	return output, nil
}

// RunPromptStreaming sends a prompt to Claude and streams the output to display
// while also capturing the full response. The streamed output is shown in real-time
// so the user can watch the description being generated.
func RunPromptStreaming(name, prompt string, display io.Writer) (string, error) {
	cmd := exec.Command("claude", "-p", prompt)
	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(display, &buf)
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	output := strings.TrimSpace(buf.String())
	if err != nil {
		return output, fmt.Errorf("claude %s: %s", name, output)
	}
	return output, nil
}

// SanitizeSessionName produces a safe session name from a branch name.
func SanitizeSessionName(prefix, branch string) string {
	name := prefix + "-" + branch
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, " ", "-")
	return name
}
