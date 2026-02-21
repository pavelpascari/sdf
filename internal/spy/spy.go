// Package spy provides invocation recording for external binaries.
// Used during E2E tests to capture real API responses from gh and claude,
// then cross-validate that fake binaries produce structurally compatible output.
package spy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Invocation records a single call to an external binary.
type Invocation struct {
	Binary    string   `json:"binary"`
	Args      []string `json:"args"`
	Stdout    string   `json:"stdout"`
	ExitCode  int      `json:"exit_code"`
	Timestamp string   `json:"timestamp"`
}

// Recorder captures invocations to a JSONL file.
// Safe for concurrent use. A nil Recorder is valid (no-op).
type Recorder struct {
	mu   sync.Mutex
	file *os.File
	name string
}

// NewRecorder creates a recorder that appends to dir/<binary>.jsonl.
// Returns nil if dir is empty (recording disabled).
func NewRecorder(dir, binary string) *Recorder {
	if dir == "" {
		return nil
	}
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, binary+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil
	}
	return &Recorder{file: f, name: binary}
}

// Record appends an invocation to the log. No-op on nil receiver.
func (r *Recorder) Record(args []string, stdout string, exitCode int) {
	if r == nil {
		return
	}
	inv := Invocation{
		Binary:    r.name,
		Args:      args,
		Stdout:    stdout,
		ExitCode:  exitCode,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(inv)
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.file.Write(append(data, '\n'))
}

// Close flushes and closes the recording file. No-op on nil receiver.
func (r *Recorder) Close() {
	if r == nil {
		return
	}
	r.file.Close()
}
