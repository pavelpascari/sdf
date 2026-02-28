package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	claudepkg "github.com/pavelpascari/sdf/internal/claude"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/render"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/ui"
)

// MoveResult is the structured output of sdf move when --json is used.
type MoveResult struct {
	Source  string   `json:"source"`
	Target  string   `json:"target"`
	Commits []string `json:"commits"`
	Error   string   `json:"error,omitempty"`
}

var moveCmd = &cobra.Command{
	Use:   "move <commit>...",
	Short: "Move commits from current branch to parent",
	Long: `Cherry-picks listed commits onto the parent branch, strips them from the
current branch via rebase, and cascade-rebases downstream branches.`,
	Example: `  sdf move abc1234                   # move one commit
  sdf move abc1234 def5678           # move multiple commits`,
	Annotations: map[string]string{"category": "stack"},
	Args:        cobra.MinimumNArgs(1),
	RunE:        runMoveCmd,
}

func init() {
	rootCmd.AddCommand(moveCmd)
	moveCmd.Flags().Bool("json", false, "output result as JSON")
}

func runMoveCmd(cmd *cobra.Command, args []string) error {
	jsonFlag, _ := cmd.Flags().GetBool("json")
	return runMoveLogic(args, jsonFlag)
}

// RunMove is a compatibility wrapper for callers that use the old interface.
func RunMove(args []string) error {
	rootCmd.SetArgs(append([]string{"move"}, args...))
	return rootCmd.Execute()
}

func runMoveLogic(commits []string, jsonMode bool) error {

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	var rdr render.Renderer
	if jsonMode {
		rdr = &render.JSONRenderer{}
	}
	bus := render.NewBus(os.Stdout, os.Stderr, render.Options{Renderer: rdr})
	if !jsonMode {
		defer func() { _ = bus.Finish() }()
	}

	s, err := resolveStack(root, "")
	if err != nil {
		return err
	}

	// Working tree must be clean
	clean, err := gitpkg.IsClean()
	if err != nil {
		return fmt.Errorf("cannot check working tree status: %w", err)
	}
	if !clean {
		return fmt.Errorf("working tree is not clean — commit or stash changes before moving")
	}

	// Determine which branch we're on
	branch, err := gitpkg.CurrentBranch()
	if err != nil {
		return fmt.Errorf("cannot determine current branch: %w", err)
	}

	idx := s.NodeIndex(branch)
	if idx < 0 {
		return fmt.Errorf("branch %q is not part of stack %q", branch, s.StackID)
	}

	parent := s.ParentBranch(branch)

	// Resolve every commit to a full SHA and validate it belongs to this branch
	branchCommits, err := gitpkg.LogCommits(parent, branch)
	if err != nil {
		return fmt.Errorf("cannot list commits on %s: %w", branch, err)
	}
	if len(branchCommits) == 0 {
		return fmt.Errorf("branch %s has no commits above %s", branch, parent)
	}

	commitSet := make(map[string]bool, len(branchCommits))
	for _, c := range branchCommits {
		commitSet[c] = true
	}

	resolvedCommits := make([]string, 0, len(commits))
	for _, c := range commits {
		sha, err := gitpkg.RevParse(c)
		if err != nil {
			return fmt.Errorf("cannot resolve commit %q: %w", c, err)
		}
		if !commitSet[sha] {
			return fmt.Errorf("commit %s (%s) is not on branch %s above %s", c, short(sha), branch, parent)
		}
		resolvedCommits = append(resolvedCommits, sha)
	}

	// After moving, at least one commit must remain on the branch
	if len(resolvedCommits) >= len(branchCommits) {
		return fmt.Errorf("cannot move all %d commits — branch %s would become empty", len(branchCommits), branch)
	}

	// Compute the new rebase boundary: the latest (furthest from parent)
	// commit being moved. We'll use rebase --onto to strip everything up to
	// and including this commit from the branch.
	//
	// This works correctly when moving a contiguous prefix of the branch's
	// commits. For non-contiguous selections the caller should run multiple
	// moves or use interactive rebase.
	lastMovedIdx := -1
	for i, c := range branchCommits {
		for _, rc := range resolvedCommits {
			if c == rc {
				if i > lastMovedIdx {
					lastMovedIdx = i
				}
			}
		}
	}
	lastMovedSHA := branchCommits[lastMovedIdx]

	// Verify the selected commits are a contiguous prefix
	for i := 0; i <= lastMovedIdx; i++ {
		found := false
		for _, rc := range resolvedCommits {
			if branchCommits[i] == rc {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("commits must be a contiguous prefix of %s; commit %s would be skipped — use interactive rebase for non-contiguous moves",
				branch, short(branchCommits[i]))
		}
	}

	bus.Printf("Moving %d commit(s) from %s to %s", len(resolvedCommits), ui.Branch(branch), ui.Branch(parent))
	for _, c := range resolvedCommits {
		bus.Printf("  %s", short(c))
	}

	// --- Phase 1: cherry-pick commits onto the parent ---
	bus.Printf("\n→ cherry-picking onto %s...", parent)
	if err := gitpkg.Checkout(parent); err != nil {
		return fmt.Errorf("cannot checkout %s: %w", parent, err)
	}

	if err := gitpkg.CherryPick(resolvedCommits...); err != nil {
		// Cherry-pick conflict — abort and restore
		gitpkg.CherryPickAbort()
		gitpkg.Checkout(branch)
		return fmt.Errorf("cherry-pick onto %s failed (conflict): %w", parent, err)
	}

	newParentTip, err := gitpkg.RevParse(parent)
	if err != nil {
		return fmt.Errorf("cannot resolve new tip of %s: %w", parent, err)
	}
	bus.Printf("  %s %s tip is now %s", ui.SymOK, ui.Branch(parent), short(newParentTip))

	// --- Phase 2: strip moved commits from current branch ---
	bus.Printf("→ rebasing %s onto updated %s...", branch, parent)
	if err := gitpkg.RebaseOnto(newParentTip, lastMovedSHA, branch); err != nil {
		if conflictErr := handleMoveConflict(s, branch, err, bus); conflictErr != nil {
			gitpkg.Checkout(branch)
			return fmt.Errorf("rebase of %s failed: %w", branch, conflictErr)
		}
	}

	// Update the current node's BaseTip
	s.Nodes[idx].BaseTip = newParentTip

	// --- Phase 3: cascade rebase downstream branches ---
	for i := idx + 1; i < len(s.Nodes); i++ {
		downstream := &s.Nodes[i]
		upstreamBranch := s.Nodes[i-1].Branch

		upstreamTip, err := gitpkg.RevParse(upstreamBranch)
		if err != nil {
			continue
		}

		if downstream.BaseTip != "" && downstream.BaseTip != upstreamTip {
			bus.Printf("→ rebasing %s onto updated %s...", downstream.Branch, upstreamBranch)

			if err := gitpkg.RebaseOnto(upstreamTip, downstream.BaseTip, downstream.Branch); err != nil {
				if conflictErr := handleMoveConflict(s, downstream.Branch, err, bus); conflictErr != nil {
					// Save partial progress before failing
					stack.Save(root, s)
					gitpkg.Checkout(branch)
					return fmt.Errorf("cascade rebase of %s failed: %w", downstream.Branch, conflictErr)
				}
			}
			downstream.BaseTip = upstreamTip
		}
	}

	// --- Phase 4: persist stack state ---
	// Restore working branch before saving so the commit lands on the right branch
	gitpkg.Checkout(branch)

	if err := stack.Save(root, s); err != nil {
		return fmt.Errorf("cannot save stack: %w", err)
	}

	bus.Printf("\n%s Moved %d commit(s) from %s to %s", ui.SymOK, len(resolvedCommits), ui.Branch(branch), ui.Branch(parent))

	if jsonMode {
		result := MoveResult{
			Source:  branch,
			Target:  parent,
			Commits: resolvedCommits,
		}
		_ = bus.Finish()
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
	}
	return nil
}

// handleMoveConflict tries Claude resolution for a rebase conflict during move.
// Falls back to aborting the rebase and returning the error.
func handleMoveConflict(s *stack.Stack, branch string, rebaseErr error, bus *render.Bus) error {
	conflicted, err := gitpkg.ConflictedFiles()
	if err != nil || len(conflicted) == 0 {
		gitpkg.RebaseAbort()
		return rebaseErr
	}

	bus.Printf("  %s Conflict in %s — %d file(s)", ui.SymWarn, ui.Branch(branch), len(conflicted))

	if claudepkg.Available() {
		bus.Print("  → invoking Claude for conflict resolution...")

		parent := s.ParentBranch(branch)
		upstreamSummary, _ := gitpkg.DiffSummary(s.FindNode(branch).BaseTip, parent)

		var branchDesc string
		node := s.FindNode(branch)
		if node != nil && node.PR > 0 && ghpkg.Available() {
			branchDesc, _ = ghpkg.PRViewBody(node.PR)
		}

		conflictContents := make(map[string]string)
		for _, f := range conflicted {
			data, err := os.ReadFile(f)
			if err == nil {
				conflictContents[f] = string(data)
			}
		}

		p := buildConflictPrompt(upstreamSummary, branchDesc, conflictContents)
		sessionName := claudepkg.SanitizeSessionName("conflict", branch)

		output, err := claudepkg.RunPrompt(sessionName, p)
		if err == nil {
			if err := applyResolutions(output, conflicted); err == nil {
				if err := gitpkg.Add("."); err == nil {
					if err := gitpkg.RebaseContinue(); err == nil {
						bus.Printf("  %s Conflicts resolved by Claude", ui.SymOK)
						return nil
					}
				}
			}
		}
		bus.Warnf("  Claude resolution failed, falling back to manual resolution")
	}

	gitpkg.RebaseAbort()
	return fmt.Errorf("conflicts in %s — resolve manually and run `sdf move` again:\n  %s",
		branch, strings.Join(conflicted, "\n  "))
}

// short returns the first 10 characters of a SHA.
func short(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}
