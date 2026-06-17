// cmd/worktree_helpers.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	cfgpkg "github.com/pavelpascari/sdf/internal/config"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/stack"
)

// stackLockTimeout bounds how long an sdf process waits for the stack lock.
const stackLockTimeout = 10 * time.Second

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
