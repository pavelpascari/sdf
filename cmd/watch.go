package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
)

func RunWatch(args []string) error {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	interval := fs.Duration("interval", 5*time.Minute, "how often to check (e.g. 2m, 30s)")
	once := fs.Bool("once", false, "check once and exit (no loop)")
	fs.Parse(args)

	root, err := stack.FindRoot()
	if err != nil {
		return err
	}

	fmt.Printf("sdf watch — checking every %s (ctrl-c to stop)\n", *interval)

	// Run first check immediately
	check(root)

	if *once {
		return nil
	}

	// Set up signal handling for clean shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			check(root)
		case <-sig:
			fmt.Println("\nsdf watch stopped.")
			return nil
		}
	}
}

func check(root string) {
	s, err := stack.Load(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "watch: cannot load stack: %v\n", err)
		return
	}

	if len(s.Nodes) == 0 {
		return
	}

	watchInfo := stack.WatchInfo{
		LastCheck: time.Now().Format(time.RFC3339),
	}

	// Check base branch: has origin/<base> moved past what we know locally?
	baseRef := "refs/heads/" + s.Base
	remoteSHA, err := gitpkg.LSRemoteRef(baseRef)
	if err != nil {
		fmt.Fprintf(os.Stderr, "watch: ls-remote failed for %s: %v\n", s.Base, err)
		return
	}

	localBase, _ := gitpkg.RevParse("origin/" + s.Base)

	if remoteSHA != localBase {
		_ = gitpkg.FetchBranch(s.Base)
	}

	// Now check each node
	for i, node := range s.Nodes {
		if node.Status == "merged" || node.Status == "closed" {
			continue
		}

		parent := s.Base
		if i > 0 {
			parent = s.Nodes[i-1].Branch
		}

		// For non-base parents, check if they moved on remote too
		if parent != s.Base {
			remoteParent, err := gitpkg.LSRemoteRef("refs/heads/" + parent)
			if err == nil {
				localParent, _ := gitpkg.RevParse("origin/" + parent)
				if remoteParent != localParent {
					_ = gitpkg.FetchBranch(parent)
				}
			}
		}

		currentParentTip, err := gitpkg.RevParse("origin/" + parent)
		if err != nil {
			continue
		}

		if node.BaseTip != "" && currentParentTip != node.BaseTip {
			watchInfo.Stale = append(watchInfo.Stale, stack.StaleRef{
				Branch:    node.Branch,
				Parent:    parent,
				LocalTip:  node.BaseTip,
				RemoteTip: currentParentTip,
			})
		}
	}

	// Check for newly merged PRs
	if ghpkg.Available() {
		branches := make([]string, len(s.Nodes))
		for i, n := range s.Nodes {
			branches[i] = n.Branch
		}
		prs, err := ghpkg.PRList(branches)
		if err == nil {
			for _, pr := range prs {
				node := s.FindNode(pr.HeadRefName)
				if node != nil && node.Status != "merged" && pr.State == "MERGED" {
					watchInfo.MergedPRs = append(watchInfo.MergedPRs, pr.HeadRefName)
				}
			}
		}
	}

	// Update only the Watch section of local.json (preserve SyncProgress etc.)
	local, _ := stack.LoadLocal(root)
	local.Watch = &watchInfo
	stack.SaveLocal(root, local)

	// Print summary
	now := time.Now().Format("15:04:05")
	if len(watchInfo.Stale) == 0 && len(watchInfo.MergedPRs) == 0 {
		fmt.Printf("[%s] all branches in sync\n", now)
		return
	}

	for _, sb := range watchInfo.Stale {
		fmt.Printf("[%s] %s needs rebase — %s has moved\n", now, sb.Branch, sb.Parent)
	}
	for _, b := range watchInfo.MergedPRs {
		fmt.Printf("[%s] %s was merged — run `sdf sync` to cascade\n", now, b)
	}
}
