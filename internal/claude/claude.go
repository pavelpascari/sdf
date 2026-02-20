// Package claude provides shell-out helpers for the Claude CLI.
package claude

import (
	"bufio"
	"encoding/json"
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

// RunPromptStreaming sends a prompt to Claude using stream-json output format,
// displaying text tokens in real-time via display writer while capturing the
// full response. Each JSON event is flushed line-by-line so tokens appear
// immediately rather than buffering until completion.
func RunPromptStreaming(name, prompt string, display io.Writer) (string, error) {
	cmd := exec.Command("claude", "-p", "--output-format", "stream-json", prompt)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("claude %s: %w", name, err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("claude %s: %w", name, err)
	}

	var result string
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Text string `json:"text"`
			} `json:"delta"`
			Result string `json:"result"`
		}

		if json.Unmarshal(line, &event) != nil {
			continue
		}

		// Display streaming text deltas as they arrive
		if event.Delta.Text != "" {
			display.Write([]byte(event.Delta.Text))
		}

		// Capture the final result text
		if event.Result != "" {
			result = event.Result
		}
	}

	if err := cmd.Wait(); err != nil {
		return result, fmt.Errorf("claude %s: failed", name)
	}

	return strings.TrimSpace(result), nil
}

// SanitizeSessionName produces a safe session name from a branch name.
func SanitizeSessionName(prefix, branch string) string {
	name := prefix + "-" + branch
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, " ", "-")
	return name
}
