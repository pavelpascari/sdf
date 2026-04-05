package ops

import (
	"fmt"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	gitpkg "github.com/pavelpascari/sdf/internal/git"
)

// DefaultHandler dispatches step kinds to real git/gh operations.
func DefaultHandler(stepID, kind string, inputs map[string]string) (map[string]string, error) {
	switch kind {
	case KindGitRevParse:
		sha, err := gitpkg.RevParse(inputs["ref"])
		return map[string]string{"sha": sha}, err

	case KindGitRebase:
		err := gitpkg.RebaseOnto(inputs["onto"], inputs["old_base"], inputs["branch"])
		if err != nil {
			return nil, err
		}
		sha, _ := gitpkg.RevParse(inputs["branch"])
		return map[string]string{"new_sha": sha}, nil

	case KindGitPush:
		return nil, gitpkg.Push(inputs["branch"])

	case KindGitPushNew:
		return nil, gitpkg.PushNew(inputs["branch"])

	case KindGitCheckout:
		return nil, gitpkg.Checkout(inputs["branch"])

	case KindGitResetHard:
		if err := gitpkg.Checkout(inputs["branch"]); err != nil {
			return nil, err
		}
		return nil, gitpkg.ResetHard(inputs["sha"])

	case KindGHPREditBase:
		if !ghpkg.Available() {
			return nil, nil
		}
		var prNum int
		fmt.Sscanf(inputs["pr"], "%d", &prNum)
		if prNum > 0 {
			return nil, ghpkg.PREditBase(prNum, inputs["base"])
		}
		return nil, nil

	default:
		return nil, nil // unknown/internal kinds are no-ops
	}
}
