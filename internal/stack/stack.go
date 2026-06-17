// Package stack manages the .sdf/stacks/*.json files and stack topology.
package stack

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
)

// Node represents a single branch in the stack.
type Node struct {
	Branch       string `json:"branch"`
	PR           int    `json:"pr,omitempty"`
	Status       string `json:"status"` // "open", "merged", "closed"
	BaseTip      string `json:"base_tip,omitempty"`
	NavHash      string `json:"nav_hash,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"` // absolute path; worktree mode only
}

// Stack represents the full stack topology persisted in a stack JSON file.
type Stack struct {
	StackID  string `json:"stack_id"`
	Base     string `json:"base"`
	Nodes    []Node `json:"nodes"`
	Worktree bool   `json:"worktree,omitempty"` // per-stack worktree mode opt-in
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
	SyncProgress    *SyncProgress     `json:"sync_progress,omitempty"`
	SplitSessions   map[string]string `json:"split_sessions,omitempty"` // stack_name → session_id
	RestackProgress *RestackProgress  `json:"restack_progress,omitempty"`
}

// RestackProgress tracks a restack operation so --continue can resume
// and --abort can restore branches to their pre-restack state.
type RestackProgress struct {
	StackID        string            `json:"stack_id"`
	OriginalBranch string            `json:"original_branch"` // branch user was on
	OriginalNodes  []Node            `json:"original_nodes"`  // node order + BaseTips before restack
	BranchSHAs     map[string]string `json:"branch_shas"`     // branch → SHA before restack
	Plan           []RestackAction   `json:"plan"`            // planned rebases
	ResumeIndex    int               `json:"resume_index"`    // index in Plan to resume from
}

// RestackAction is the JSON-serializable version of a planned rebase.
type RestackAction struct {
	Branch    string `json:"branch"`
	NewParent string `json:"new_parent"`
	OldParent string `json:"old_parent"`
}

// SyncProgress tracks a paused sync so `sdf sync --continue` can resume.
type SyncProgress struct {
	PausedAt       string `json:"paused_at"`               // branch that had conflicts
	ResumeIndex    int    `json:"resume_index"`            // index in Nodes to resume from
	OriginalBranch string `json:"original_branch"`         // branch to restore when done
	ParentTip      string `json:"parent_tip"`              // the parent tip we were rebasing onto
	WorktreePath   string `json:"worktree_path,omitempty"` // set when the paused rebase ran in a worktree
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

// SaveLocal writes .sdf/local.json atomically using a temp file in the same
// directory followed by os.Rename, so readers always see a complete file.
func SaveLocal(root string, ls *LocalState) error {
	sdfDir := filepath.Join(root, SDFDir)
	data, err := json.MarshalIndent(ls, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal local state: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(sdfDir, LocalFile)
	tmp, err := os.CreateTemp(sdfDir, ".local.*.tmp")
	if err != nil {
		return fmt.Errorf("cannot create temp local file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("cannot write temp local file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("cannot rename temp local file: %w", err)
	}
	return nil
}

// FindRoot walks up from the current directory to find the repo root
// containing a .sdf directory (with either stacks/ or legacy stack.json).
// When running inside a linked worktree (where .sdf/ is absent because it is
// gitignored), it falls back to the main worktree via git-common-dir.
func FindRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine working directory: %w", err)
	}

	for {
		if root, ok := sdfRootAt(dir); ok {
			return root, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Fallback: we may be inside a linked worktree whose checkout has no .sdf/
	// (it is gitignored). The single .sdf/ lives in the main worktree.
	if mainRoot, mErr := gitpkg.MainWorktreeRoot(); mErr == nil {
		if root, ok := sdfRootAt(mainRoot); ok {
			return root, nil
		}
	}

	return "", errors.New("not inside an sdf stack (no .sdf/ found)")
}

// sdfRootAt reports whether dir holds an .sdf/ directory in either layout.
func sdfRootAt(dir string) (string, bool) {
	sdfDir := filepath.Join(dir, SDFDir)
	info, err := os.Stat(sdfDir)
	if err != nil || !info.IsDir() {
		return "", false
	}
	if _, err := os.Stat(filepath.Join(sdfDir, StacksDir)); err == nil {
		return dir, true
	}
	if _, err := os.Stat(filepath.Join(sdfDir, StackFile)); err == nil {
		return dir, true
	}
	return "", false
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

// Save writes the stack to .sdf/stacks/<stack_id>.json atomically using a
// temp file in the same directory followed by os.Rename, so readers always
// see a complete file.
func Save(root string, s *Stack) error {
	stacksDir := filepath.Join(root, SDFDir, StacksDir)
	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		return fmt.Errorf("cannot create %s: %w", stacksDir, err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal stack: %w", err)
	}
	data = append(data, '\n')
	path := StackPath(root, s.StackID)
	tmp, err := os.CreateTemp(stacksDir, "."+s.StackID+".*.tmp")
	if err != nil {
		return fmt.Errorf("cannot create temp stack file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("cannot write temp stack file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("cannot rename temp stack file: %w", err)
	}
	return nil
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
	// Skip over merged and closed nodes — they are no longer active in the chain
	for j := idx - 1; j >= 0; j-- {
		if s.Nodes[j].Status != "merged" && s.Nodes[j].Status != "closed" {
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
