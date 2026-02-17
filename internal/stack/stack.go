// Package stack manages the .sdf/stack.json file and stack topology.
package stack

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Node represents a single branch in the stack.
type Node struct {
	Branch  string `json:"branch"`
	PR      int    `json:"pr,omitempty"`
	Status  string `json:"status"` // "open", "merged", "draft"
	BaseTip string `json:"base_tip,omitempty"`
}

// Stack represents the full stack topology persisted in stack.json.
type Stack struct {
	StackID string `json:"stack_id"`
	Base    string `json:"base"`
	Nodes   []Node `json:"nodes"`
}

// SDFDir is the name of the sdf metadata directory.
const SDFDir = ".sdf"

// StackFile is the filename for the stack topology.
const StackFile = "stack.json"

// LocalFile is the filename for ephemeral local state.
const LocalFile = "local.json"

// FindRoot walks up from the current directory to find the repo root
// containing a .sdf directory.
func FindRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine working directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, SDFDir, StackFile)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("not inside an sdf stack (no .sdf/stack.json found)")
		}
		dir = parent
	}
}

// Load reads the stack from the .sdf/stack.json in the given repo root.
func Load(root string) (*Stack, error) {
	path := filepath.Join(root, SDFDir, StackFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	var s Stack
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("cannot parse %s: %w", path, err)
	}
	return &s, nil
}

// Save writes the stack back to .sdf/stack.json.
func Save(root string, s *Stack) error {
	path := filepath.Join(root, SDFDir, StackFile)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal stack: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

// FindNode returns the node for the given branch name, or nil.
func (s *Stack) FindNode(branch string) *Node {
	for i := range s.Nodes {
		if s.Nodes[i].Branch == branch {
			return &s.Nodes[i]
		}
	}
	return nil
}

// NodeIndex returns the index of the node for the given branch, or -1.
func (s *Stack) NodeIndex(branch string) int {
	for i, n := range s.Nodes {
		if n.Branch == branch {
			return i
		}
	}
	return -1
}

// ParentBranch returns the branch that the given branch is based on.
// For the first node, this is the stack base (e.g. "main").
func (s *Stack) ParentBranch(branch string) string {
	idx := s.NodeIndex(branch)
	if idx <= 0 {
		return s.Base
	}
	return s.Nodes[idx-1].Branch
}

// Init creates the .sdf directory and writes an initial stack.json.
func Init(root, stackID, base string) error {
	sdfDir := filepath.Join(root, SDFDir)
	if err := os.MkdirAll(sdfDir, 0755); err != nil {
		return fmt.Errorf("cannot create %s: %w", sdfDir, err)
	}

	contextDir := filepath.Join(sdfDir, "context")
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		return fmt.Errorf("cannot create %s: %w", contextDir, err)
	}

	s := &Stack{
		StackID: stackID,
		Base:    base,
		Nodes:   []Node{},
	}
	if err := Save(root, s); err != nil {
		return err
	}

	// Create .sdf/local.json (gitignored)
	localPath := filepath.Join(sdfDir, LocalFile)
	if err := os.WriteFile(localPath, []byte("{}\n"), 0644); err != nil {
		return fmt.Errorf("cannot create %s: %w", localPath, err)
	}

	return nil
}
