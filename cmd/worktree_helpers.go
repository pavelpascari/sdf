// cmd/worktree_helpers.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

// addWorktreeForNode creates the worktree for a node and records its path.
// createFrom is the ref to branch from for a new branch; pass "" to check out
// an already-existing branch.
func addWorktreeForNode(cfg cfgpkg.Config, root string, node *stack.Node, createFrom string) error {
	path := cfg.WorktreePathFor(root, node.Branch)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("cannot create worktree base dir: %w", err)
	}
	if err := gitpkg.WorktreeAdd(path, node.Branch, createFrom); err != nil {
		return fmt.Errorf("cannot create worktree for %s: %w", node.Branch, err)
	}
	node.WorktreePath = path
	return nil
}

// removeWorktreeForNode removes a node's worktree (if recorded) and clears the path.
func removeWorktreeForNode(root string, node *stack.Node, force bool) error {
	if node.WorktreePath == "" {
		return nil
	}
	if err := gitpkg.WorktreeRemove(node.WorktreePath, force); err != nil {
		return err
	}
	node.WorktreePath = ""
	return nil
}

// branchWorktreeDir returns the directory git ops for a branch must run in:
// the branch's worktree in worktree mode, or "" (the process CWD) otherwise.
func branchWorktreeDir(s *stack.Stack, branch string) string {
	if n := s.FindNode(branch); n != nil && n.WorktreePath != "" {
		return n.WorktreePath
	}
	return ""
}

// currentWorktreeNode returns the stack node for currentBranch when that node
// has a worktree path (i.e. we are operating inside its worktree). Returns nil
// when currentBranch is the base, not a node, or has no worktree.
func currentWorktreeNode(s *stack.Stack, currentBranch string) *stack.Node {
	node := s.FindNode(currentBranch)
	if node == nil || node.WorktreePath == "" {
		return nil
	}
	return node
}

// requireBranchWorktreeDir returns the branch's worktree directory, or an error
// if the stack is in worktree mode but the branch has no recorded worktree
// (which would otherwise cause a confusing `git -C ""` failure). For
// non-worktree stacks it returns "" with no error (operate in CWD).
func requireBranchWorktreeDir(s *stack.Stack, branch string) (string, error) {
	if !s.Worktree {
		return "", nil
	}
	n := s.FindNode(branch)
	if n == nil || n.WorktreePath == "" {
		return "", fmt.Errorf("branch %q has no worktree — run `sdf worktree enable` or `sdf doctor`", branch)
	}
	return n.WorktreePath, nil
}
