// cmd/merge_worktree_lock_test.go
//
// Verifies that the worktree reconcile-save in runMergeLogic is routed through
// stack.WithLock so a concurrent downstream BaseTip update is not clobbered.
//
// The test does NOT exercise the full runMergeLogic (that requires gh CLI);
// instead it directly tests the locking invariant that the new production code
// relies on.
package cmd

import (
	"sync"
	"testing"
	"time"

	"github.com/pavelpascari/sdf/internal/stack"
)

// TestLockedReconcileDoesNotClobberConcurrentBaseTipBump verifies the locking
// invariant: when BOTH the merge-reconcile and the concurrent downstream sync
// use WithLock (the new production behavior), the second lock reloads a fresh
// copy of the stack, so neither mutation clobbers the other.
//
// Phase 1 documents the bug: the old unlocked path (load → mutate → Save)
// clobbers a concurrent WithLock save. Phase 2 validates the fix.
func TestLockedReconcileDoesNotClobberConcurrentBaseTipBump(t *testing.T) {
	root := bareRepoWithClone(t)

	// Build a two-node worktree stack: nodeA (head) and nodeB (downstream).
	if _, err := runNewCore("feat", "main", "feat/a", false, true); err != nil {
		t.Fatalf("runNewCore feat/a: %v", err)
	}
	if err := RunBranch([]string{"feat/b", "--no-prefix"}); err != nil {
		t.Fatalf("RunBranch feat/b: %v", err)
	}

	// Confirm initial state.
	s0, err := stack.LoadStack(root, "feat")
	if err != nil {
		t.Fatal(err)
	}
	if len(s0.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(s0.Nodes))
	}

	// Phase 1: demonstrate the race with the OLD approach.
	// Op A (old-style unlocked): load → mark feat/a merged → wait → save.
	// Op B (new WithLock):       wait for A to load → bump feat/b BaseTip → save.
	// Expected with unlocked save: A's save overwrites B's BaseTip → CLOBBER.

	// Use a channel pair to choreograph the exact interleaving:
	//   1. A loads the stack.
	//   2. A signals aLoaded.
	//   3. B acquires lock, bumps BaseTip, saves, signals bDone.
	//   4. A waits for bDone, then saves its stale snapshot.
	aLoaded := make(chan struct{})
	bDone := make(chan struct{})

	var wg sync.WaitGroup
	var opAErr, opBErr error

	// Op A: old unlocked path — deliberately interleaved to expose the bug.
	wg.Add(1)
	go func() {
		defer wg.Done()
		s, err := stack.LoadStack(root, "feat")
		if err != nil {
			opAErr = err
			close(aLoaded)
			return
		}
		// Mark feat/a merged on the stale snapshot.
		n := s.FindNode("feat/a")
		if n != nil {
			n.Status = "merged"
		}
		// Tell B we've loaded (but NOT yet saved).
		close(aLoaded)
		// Wait for B to do its WithLock save before A saves.
		<-bDone
		// A saves its stale snapshot — overwrites B's BaseTip in the OLD path.
		opAErr = stack.Save(root, s)
	}()

	// Op B: new WithLock path — bumps downstream BaseTip.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(bDone)
		// Wait until A has loaded (ensuring the stale snapshot is in memory).
		<-aLoaded
		opBErr = stack.WithLock(root, "feat", func(ls *stack.Stack) error {
			n := ls.FindNode("feat/b")
			if n == nil {
				return nil
			}
			n.BaseTip = "deadbeef"
			return nil
		})
	}()

	wg.Wait()

	if opAErr != nil {
		t.Fatalf("Op A: %v", opAErr)
	}
	if opBErr != nil {
		t.Fatalf("Op B: %v", opBErr)
	}

	// With the OLD unlocked approach A's save ran after B's save, so it should
	// have overwritten B's BaseTip. This is the bug we're fixing.
	afterUnlocked, err := stack.LoadStack(root, "feat")
	if err != nil {
		t.Fatalf("load after unlocked race: %v", err)
	}
	nodeA := afterUnlocked.FindNode("feat/a")
	nodeB := afterUnlocked.FindNode("feat/b")

	// Document the bug: the old path clobbers B's BaseTip.
	// (If this assertion trips it means the old path happened to not clobber
	// due to timing — the test is inherently probabilistic for the buggy path,
	// but the choreography above (aLoaded + bDone channels) ensures the exact
	// interleaving that exposes the bug.)
	if nodeA.Status != "merged" {
		t.Errorf("Phase 1: feat/a.Status should be merged, got %q", nodeA.Status)
	}
	// In the buggy old path, B's BaseTip is clobbered to "".
	// We document this as expected behavior of the OLD path.
	unlockedClobbered := nodeB.BaseTip != "deadbeef"

	// Phase 2: demonstrate correctness of the NEW locked path.
	// Reset the stack to initial state.
	initial := &stack.Stack{
		StackID:  "feat",
		Base:     "main",
		Worktree: true,
		Nodes: []stack.Node{
			{Branch: "feat/a", Status: "open", WorktreePath: s0.Nodes[0].WorktreePath},
			{Branch: "feat/b", Status: "open", WorktreePath: s0.Nodes[1].WorktreePath},
		},
	}
	if err := stack.Save(root, initial); err != nil {
		t.Fatalf("reset stack: %v", err)
	}

	// Op A (NEW path): WithLock → mark feat/a merged.
	// Op B: WithLock → bump feat/b BaseTip.
	// Both use WithLock, so they serialize and neither clobbers the other.
	opAReady := make(chan struct{})
	var opANew, opBNew error

	wg.Add(1)
	go func() {
		defer wg.Done()
		opANew = stack.WithLock(root, "feat", func(ls *stack.Stack) error {
			n := ls.FindNode("feat/a")
			if n != nil {
				n.Status = "merged"
			}
			// Signal that we're inside the lock (B will queue up on AcquireLock).
			close(opAReady)
			// Small sleep to maximize overlap with B's lock-acquisition attempt.
			time.Sleep(10 * time.Millisecond)
			return nil
		})
	}()

	// Start B only after A is inside its lock so they genuinely contend.
	<-opAReady
	wg.Add(1)
	go func() {
		defer wg.Done()
		opBNew = stack.WithLock(root, "feat", func(ls *stack.Stack) error {
			n := ls.FindNode("feat/b")
			if n != nil {
				n.BaseTip = "deadbeef"
			}
			return nil
		})
	}()

	wg.Wait()

	if opANew != nil {
		t.Fatalf("Phase 2 Op A: %v", opANew)
	}
	if opBNew != nil {
		t.Fatalf("Phase 2 Op B: %v", opBNew)
	}

	final, err := stack.LoadStack(root, "feat")
	if err != nil {
		t.Fatalf("load after locked: %v", err)
	}

	finalA := final.FindNode("feat/a")
	finalB := final.FindNode("feat/b")

	if finalA == nil || finalA.Status != "merged" {
		t.Errorf("Phase 2: feat/a.Status = %q, want merged", finalA.Status)
	}
	if finalB == nil || finalB.BaseTip != "deadbeef" {
		t.Errorf("Phase 2: feat/b.BaseTip = %q, want deadbeef (clobbered by unlocked save)", finalB.BaseTip)
	}

	// Summarize: the unlocked path was racy (clobbered in our choreographed run),
	// the locked path is always safe.
	if !unlockedClobbered {
		// The race didn't manifest (can happen due to OS scheduling). The test
		// still validates the locked path is correct.  Log rather than fail.
		t.Log("NOTE: unlocked race did not manifest in this run (timing-dependent); Phase 2 locked path still validated")
	} else {
		t.Log("Phase 1 confirmed: old unlocked save clobbered concurrent BaseTip bump")
	}
}
