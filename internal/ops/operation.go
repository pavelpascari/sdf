package ops

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const sdfDir = ".sdf"
const localFile = "local.json"

// Operation represents an in-progress sdf command with its full step pipeline.
type Operation struct {
	Command        string            `json:"command"`
	StackID        string            `json:"stack_id"`
	StartedAt      time.Time         `json:"started_at"`
	OriginalBranch string            `json:"original_branch"`
	Snapshot       map[string]string `json:"snapshot,omitempty"`
	Steps          []*Step           `json:"steps"`
	CommandData    json.RawMessage   `json:"command_data,omitempty"`
}

// localState mirrors the shape of .sdf/local.json.
// We only touch the "operation" key; other fields pass through untouched.
type localState struct {
	Operation       *Operation      `json:"operation,omitempty"`
	SyncProgress    json.RawMessage `json:"sync_progress,omitempty"`
	SplitSessions   json.RawMessage `json:"split_sessions,omitempty"`
	RestackProgress json.RawMessage `json:"restack_progress,omitempty"`
}

// Load reads the current operation from .sdf/local.json.
// Returns nil, nil if no operation is in progress.
func Load(root string) (*Operation, error) {
	path := filepath.Join(root, sdfDir, localFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	var ls localState
	if err := json.Unmarshal(data, &ls); err != nil {
		return nil, nil // corrupted — treat as no operation
	}
	return ls.Operation, nil
}

// Save writes the operation to .sdf/local.json, preserving other fields.
func Save(root string, op *Operation) error {
	path := filepath.Join(root, sdfDir, localFile)

	var ls localState
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &ls)
	}

	ls.Operation = op
	data, err := json.MarshalIndent(ls, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal local state: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

// Clear removes the operation from .sdf/local.json, preserving other fields.
func Clear(root string) error {
	return Save(root, nil)
}

// FindStep returns the step with the given ID, or nil.
func (op *Operation) FindStep(id string) *Step {
	for _, s := range op.Steps {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// CurrentStep returns the first step that is not done or skipped.
func (op *Operation) CurrentStep() *Step {
	for _, s := range op.Steps {
		if s.Status != StatusDone && s.Status != StatusSkipped {
			return s
		}
	}
	return nil
}
