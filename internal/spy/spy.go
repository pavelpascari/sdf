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
	Actor      string   `json:"actor"`
	Binary     string   `json:"binary"`
	Args       []string `json:"args"`
	Stdout     string   `json:"stdout"`
	ExitCode   int      `json:"exit_code"`
	Timestamp  string   `json:"timestamp"`
	DurationNs int64    `json:"duration_ns"`
}

// Recorder captures invocations to a JSONL file.
// Safe for concurrent use. A nil Recorder is valid (no-op).
type Recorder struct {
	mu     sync.Mutex
	file   *os.File
	name   string // actor identity (e.g., "git_testing", "sdf")
	binary string // tool being spied on (e.g., "git", "sdf")
}

// NewRecorder creates a recorder where the actor name equals the binary name.
// File is written to dir/<name>.jsonl.
// Returns nil if dir is empty (recording disabled).
func NewRecorder(dir, name string) *Recorder {
	if dir == "" {
		return nil
	}
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, name+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil
	}
	return &Recorder{file: f, name: name, binary: name}
}

// NewRecorderFor creates a recorder with distinct actor name and binary.
// File is written to dir/<binary>_<name>.jsonl. The actor and binary fields
// in each JSON entry reflect the recorder's identity and the tool being spied on.
// Returns nil if dir is empty (recording disabled).
func NewRecorderFor(dir, name, binary string) *Recorder {
	if dir == "" {
		return nil
	}
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, binary+"_"+name+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil
	}
	return &Recorder{file: f, name: name, binary: binary}
}

// Name returns the recorder's actor name. Returns "" on nil receiver.
func (r *Recorder) Name() string {
	if r == nil {
		return ""
	}
	return r.name
}

// Binary returns the tool this recorder spies on. Returns "" on nil receiver.
func (r *Recorder) Binary() string {
	if r == nil {
		return ""
	}
	return r.binary
}

// Record appends an invocation to the log. No-op on nil receiver.
// Uses the recorder's name as actor and its binary as the tool.
func (r *Recorder) Record(args []string, stdout string, exitCode int, elapsed time.Duration) {
	r.RecordAs(r.name, r.binary, args, stdout, exitCode, elapsed)
}

// RecordAs appends an invocation with explicit actor and binary names.
// Use this for combined logs (e.g., full.jsonl) where one recorder captures
// invocations from multiple tools and actors. No-op on nil receiver.
func (r *Recorder) RecordAs(actor, binary string, args []string, stdout string, exitCode int, elapsed time.Duration) {
	if r == nil {
		return
	}
	inv := Invocation{
		Actor:      actor,
		Binary:     binary,
		Args:       args,
		Stdout:     stdout,
		ExitCode:   exitCode,
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		DurationNs: elapsed.Nanoseconds(),
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
