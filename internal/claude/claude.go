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

// Binary is the name (or path) of the claude executable.
// Tests can override this to point at a fake binary.
var Binary = "claude"

// Available returns true if the claude CLI is installed and accessible.
func Available() bool {
	_, err := exec.LookPath(Binary)
	return err == nil
}

// Version returns the claude CLI version string.
func Version() (string, error) {
	cmd := exec.Command(Binary, "--version")
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
	cmd := exec.Command(Binary, "-p", prompt)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		return output, fmt.Errorf("claude %s: %s", sessionName, output)
	}
	return output, nil
}

// RunPromptStreaming sends a prompt to Claude using stream-json output format
// with partial messages enabled, displaying text in real-time via display writer
// while capturing the full response.
//
// The stream-json format emits JSON events line-by-line. With --include-partial-messages,
// "assistant" events arrive incrementally with growing content. We display only the
// new text since the last event. The "result" event carries the final complete text.
func RunPromptStreaming(name, prompt string, display io.Writer) (string, error) {
	cmd := exec.Command(Binary, "-p", "--verbose",
		"--output-format", "stream-json",
		"--include-partial-messages",
		prompt)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("claude %s: %w", name, err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("claude %s: %w", name, err)
	}

	var result string
	var displayedLen int
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
			Result string `json:"result"`
		}

		if json.Unmarshal(line, &event) != nil {
			continue
		}

		// Display incremental text from partial assistant messages
		if event.Type == "assistant" && len(event.Message.Content) > 0 {
			text := event.Message.Content[0].Text
			if len(text) > displayedLen {
				display.Write([]byte(text[displayedLen:]))
				displayedLen = len(text)
			}
		}

		// Capture the final result text
		if event.Type == "result" && event.Result != "" {
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
