// Package stack manages the .sdf/stacks/*.json files and stack topology.
package stack

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Node represents a single branch in the stack.
type Node struct {
	Branch  string `json:"branch"`
	PR      int    `json:"pr,omitempty"`
	Status  string `json:"status"` // "open", "merged", "draft"
	BaseTip string `json:"base_tip,omitempty"`
	NavHash string `json:"nav_hash,omitempty"`
}

// Stack represents the full stack topology persisted in a stack JSON file.
type Stack struct {
	StackID string `json:"stack_id"`
	Base    string `json:"base"`
	Nodes   []Node `json:"nodes"`
}

// SDFDir is the name of the sdf metadata directory.
const SDFDir = ".sdf"

// StacksDir is the subdirectory for per-stack JSON files.
const StacksDir = "stacks"

// StackFile is the legacy filename for a single-stack layout.
const StackFile = "stack.json"

// LocalFile is the filename for ephemeral local state.
const LocalFile = "local.json"

// LocalState is the root structure of .sdf/local.json.
// Each subsystem owns its own field — they don't clobber each other.
type LocalState struct {
	SyncProgress *SyncProgress `json:"sync_progress,omitempty"`
}

// SyncProgress tracks a paused sync so `sdf sync --continue` can resume.
type SyncProgress struct {
	PausedAt       string `json:"paused_at"`       // branch that had conflicts
	ResumeIndex    int    `json:"resume_index"`    // index in Nodes to resume from
	OriginalBranch string `json:"original_branch"` // branch to restore when done
	ParentTip      string `json:"parent_tip"`      // the parent tip we were rebasing onto
}

// LoadLocal reads .sdf/local.json, returning an empty state if it doesn't exist.
func LoadLocal(root string) (*LocalState, error) {
	path := filepath.Join(root, SDFDir, LocalFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &LocalState{}, nil
		}
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	var ls LocalState
	if err := json.Unmarshal(data, &ls); err != nil {
		// Corrupted file — start fresh
		return &LocalState{}, nil
	}
	return &ls, nil
}

// SaveLocal writes .sdf/local.json.
func SaveLocal(root string, ls *LocalState) error {
	path := filepath.Join(root, SDFDir, LocalFile)
	data, err := json.MarshalIndent(ls, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal local state: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

// FindRoot walks up from the current directory to find the repo root
// containing a .sdf directory (with either stacks/ or legacy stack.json).
func FindRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine working directory: %w", err)
	}

	for {
		sdfDir := filepath.Join(dir, SDFDir)
		if info, err := os.Stat(sdfDir); err == nil && info.IsDir() {
			// New multi-stack layout
			if _, err := os.Stat(filepath.Join(sdfDir, StacksDir)); err == nil {
				return dir, nil
			}
			// Legacy single-stack layout
			if _, err := os.Stat(filepath.Join(sdfDir, StackFile)); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("not inside an sdf stack (no .sdf/ found)")
		}
		dir = parent
	}
}

// MigrateIfNeeded moves a legacy .sdf/stack.json to .sdf/stacks/<id>.json.
// It is safe to call multiple times; it is a no-op if already migrated.
func MigrateIfNeeded(root string) error {
	stacksDir := filepath.Join(root, SDFDir, StacksDir)
	if _, err := os.Stat(stacksDir); err == nil {
		return nil // Already new format
	}

	oldPath := filepath.Join(root, SDFDir, StackFile)
	data, err := os.ReadFile(oldPath)
	if err != nil {
		return nil // No old file either — fresh repo
	}

	var s Stack
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("cannot parse legacy %s: %w", oldPath, err)
	}

	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		return fmt.Errorf("cannot create %s: %w", stacksDir, err)
	}

	if err := Save(root, &s); err != nil {
		return err
	}

	if err := os.Remove(oldPath); err != nil {
		return fmt.Errorf("cannot remove legacy %s: %w", oldPath, err)
	}

	return nil
}

// StackPath returns the absolute path to a stack's JSON file.
func StackPath(root string, stackID string) string {
	return filepath.Join(root, SDFDir, StacksDir, stackID+".json")
}

// StackRelPath returns the repo-relative path for a stack's JSON file,
// suitable for git add.
func StackRelPath(s *Stack) string {
	return filepath.Join(SDFDir, StacksDir, s.StackID+".json")
}

// LoadStack reads a specific stack by name from .sdf/stacks/<name>.json.
func LoadStack(root, name string) (*Stack, error) {
	path := StackPath(root, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read stack %q: %w", name, err)
	}
	var s Stack
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("cannot parse stack %q: %w", name, err)
	}
	return &s, nil
}

// Load reads the stack for backward compatibility. It works when there is
// exactly one stack in the new layout, or when the legacy stack.json exists.
// Prefer LoadStack() or LoadByBranch() for multi-stack repos.
func Load(root string) (*Stack, error) {
	// Try new multi-stack layout first
	names, err := ListStacks(root)
	if err == nil && len(names) == 1 {
		return LoadStack(root, names[0])
	}
	if err == nil && len(names) > 1 {
		return nil, fmt.Errorf("multiple stacks found; use LoadStack() or LoadByBranch()")
	}

	// Fall back to legacy single-stack layout
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

// LoadAll reads all stacks from .sdf/stacks/.
func LoadAll(root string) ([]*Stack, error) {
	names, err := ListStacks(root)
	if err != nil {
		return nil, err
	}
	var stacks []*Stack
	for _, name := range names {
		s, err := LoadStack(root, name)
		if err != nil {
			return nil, err
		}
		stacks = append(stacks, s)
	}
	return stacks, nil
}

// LoadByBranch finds the stack that contains the given branch.
func LoadByBranch(root, branch string) (*Stack, error) {
	stacks, err := LoadAll(root)
	if err != nil {
		return nil, err
	}
	for _, s := range stacks {
		if s.FindNode(branch) != nil {
			return s, nil
		}
	}
	return nil, fmt.Errorf("branch %q is not in any stack", branch)
}

// ListStacks returns the names of all stacks in .sdf/stacks/.
func ListStacks(root string) ([]string, error) {
	stacksDir := filepath.Join(root, SDFDir, StacksDir)
	entries, err := os.ReadDir(stacksDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	return names, nil
}

// Save writes the stack to .sdf/stacks/<stack_id>.json.
func Save(root string, s *Stack) error {
	stacksDir := filepath.Join(root, SDFDir, StacksDir)
	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		return fmt.Errorf("cannot create %s: %w", stacksDir, err)
	}

	path := StackPath(root, s.StackID)
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
	// Skip over merged nodes — their changes are already in the base branch
	for j := idx - 1; j >= 0; j-- {
		if s.Nodes[j].Status != "merged" {
			return s.Nodes[j].Branch
		}
	}
	return s.Base
}

// Init creates the .sdf directory structure and writes an initial stack file.
func Init(root, stackID, base string) error {
	sdfDir := filepath.Join(root, SDFDir)
	if err := os.MkdirAll(sdfDir, 0755); err != nil {
		return fmt.Errorf("cannot create %s: %w", sdfDir, err)
	}

	stacksDir := filepath.Join(sdfDir, StacksDir)
	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		return fmt.Errorf("cannot create %s: %w", stacksDir, err)
	}

	s := &Stack{
		StackID: stackID,
		Base:    base,
		Nodes:   []Node{},
	}
	if err := Save(root, s); err != nil {
		return err
	}

	// Create .sdf/local.json if it doesn't exist (gitignored)
	localPath := filepath.Join(sdfDir, LocalFile)
	if _, err := os.Stat(localPath); err != nil {
		if writeErr := os.WriteFile(localPath, []byte("{}\n"), 0644); writeErr != nil {
			return fmt.Errorf("cannot create %s: %w", localPath, writeErr)
		}
	}

	return nil
}
