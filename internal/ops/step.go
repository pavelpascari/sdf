package ops

// Step kind constants.
const (
	KindGitFetchAll     = "git-fetch-all"
	KindGitFastForward  = "git-fast-forward"
	KindGitRevParse     = "git-rev-parse"
	KindGitIsAncestor   = "git-is-ancestor"
	KindGitCreateBranch = "git-create-branch"
	KindGitCheckout     = "git-checkout"
	KindGitRebase       = "git-rebase"
	KindGitCherryPick   = "git-cherry-pick"
	KindGitPush         = "git-push"
	KindGitPushNew      = "git-push-new"
	KindGitResetHard    = "git-reset-hard"

	KindGHPRList     = "gh-pr-list"
	KindGHPRCreate   = "gh-pr-create"
	KindGHPREditBase = "gh-pr-edit-base"
	KindGHPRMerge    = "gh-pr-merge"
	KindGHPRView     = "gh-pr-view"

	KindReconcilePRs   = "reconcile-prs"
	KindReorderNodes   = "reorder-nodes"
	KindUpdateStackNav = "update-stack-nav"
	KindRenderStatus   = "render-status"
)

// Step phase constants.
const (
	PhasePreMutation = "pre-mutation"
	PhaseMutation    = "mutation"
	PhaseCommit      = "commit"
	PhasePostCommit  = "post-commit"
)

// Step status constants.
const (
	StatusPending    = "pending"
	StatusInProgress = "in-progress"
	StatusDone       = "done"
	StatusConflict   = "conflict"
	StatusSkipped    = "skipped"
	StatusFailed     = "failed"
)

// Step represents a single operation in an sdf command pipeline.
type Step struct {
	ID      string            `json:"id"`
	Kind    string            `json:"kind"`
	Phase   string            `json:"phase"`
	Inputs  map[string]Value  `json:"inputs"`
	Outputs map[string]string `json:"outputs,omitempty"`
	Status  string            `json:"status"`
	Error   string            `json:"error,omitempty"`
}

// Value is either a literal string or a reference to an upstream step's output.
type Value struct {
	Literal string `json:"literal,omitempty"`
	Ref     string `json:"ref,omitempty"`
}

// Lit creates a Value with a literal string.
func Lit(s string) Value {
	return Value{Literal: s}
}

// Ref creates a Value referencing an upstream step output.
// Format: "step-id.output-name"
func Ref(ref string) Value {
	return Value{Ref: ref}
}
