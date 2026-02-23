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

	exitCode := 0
	if err != nil {
		exitCode = 1
	}
	recordRun([]string{"-p", prompt}, output, exitCode)

	if err != nil {
		return output, fmt.Errorf("claude %s: %s", sessionName, output)
	}
	return output, nil
}

// StreamResult holds the output of a streaming Claude invocation.
type StreamResult struct {
	Result    string // final response text
	SessionID string // session ID for resumption
}

// RunPromptStreaming sends a prompt to Claude using stream-json output format
// with partial messages enabled, displaying text in real-time via display writer
// while capturing the full response and session ID.
func RunPromptStreaming(name, prompt string, display io.Writer) (StreamResult, error) {
	args := []string{"-p", "--verbose",
		"--output-format", "stream-json",
		"--include-partial-messages",
		prompt}
	return runStreaming(name, args, display)
}

// RunPromptStreamingResume resumes a previous session with a new prompt,
// streaming output and capturing the response.
func RunPromptStreamingResume(name, sessionID, prompt string, display io.Writer) (StreamResult, error) {
	args := []string{"--resume", sessionID,
		"-p", "--verbose",
		"--output-format", "stream-json",
		"--include-partial-messages",
		prompt}
	return runStreaming(name, args, display)
}

// runStreaming is the shared implementation for streaming Claude invocations.
func runStreaming(name string, args []string, display io.Writer) (StreamResult, error) {
	cmd := exec.Command(Binary, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return StreamResult{}, fmt.Errorf("claude %s: %w", name, err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return StreamResult{}, fmt.Errorf("claude %s: %w", name, err)
	}

	var sr StreamResult
	var displayedLen int
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event struct {
			Type      string `json:"type"`
			SessionID string `json:"session_id"`
			Message   struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
			Result string `json:"result"`
		}

		if json.Unmarshal(line, &event) != nil {
			continue
		}

		// Capture session_id from any event that has it
		if event.SessionID != "" && sr.SessionID == "" {
			sr.SessionID = event.SessionID
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
		if event.Type == "result" {
			if event.Result != "" {
				sr.Result = event.Result
			}
			if event.SessionID != "" {
				sr.SessionID = event.SessionID
			}
		}
	}

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		exitCode = 1
		recordRun(args, sr.Result, exitCode)
		return sr, fmt.Errorf("claude %s: failed", name)
	}

	sr.Result = strings.TrimSpace(sr.Result)
	recordRun(args, sr.Result, exitCode)
	return sr, nil
}

// SanitizeSessionName produces a safe session name from a branch name.
func SanitizeSessionName(prefix, branch string) string {
	name := prefix + "-" + branch
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, " ", "-")
	return name
}

// RunInteractiveResume spawns an interactive Claude session that resumes
// a previous conversation. The initialPrompt is passed as the positional
// argument so Claude starts with context. Returns nil when the user exits.
func RunInteractiveResume(sessionID, initialPrompt string) error {
	args := []string{"--resume", sessionID, initialPrompt}
	cmd := exec.Command(Binary, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
