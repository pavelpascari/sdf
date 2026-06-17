// internal/stack/withlock_test.go
package stack

import (
	"sync"
	"testing"
)

func TestWithLockSerializesConcurrentAppends(t *testing.T) {
	root := sdfRepo(t) // helper from lock_test.go: makes .sdf/stacks/
	if err := Save(root, &Stack{StackID: "feat", Base: "main", Nodes: nil}); err != nil {
		t.Fatal(err)
	}
	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = WithLock(root, "feat", func(s *Stack) error {
				s.Nodes = append(s.Nodes, Node{Branch: branchName(i), Status: "open"})
				return nil
			})
		}(i)
	}
	wg.Wait()
	s, err := LoadStack(root, "feat")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Nodes) != n {
		t.Fatalf("lost updates: got %d nodes, want %d", len(s.Nodes), n)
	}
}

func TestWithLockSkipsSaveOnError(t *testing.T) {
	root := sdfRepo(t)
	_ = Save(root, &Stack{StackID: "feat", Base: "main", Nodes: []Node{{Branch: "a", Status: "open"}}})
	want := errString("boom")
	err := WithLock(root, "feat", func(s *Stack) error {
		s.Nodes = append(s.Nodes, Node{Branch: "b"})
		return want
	})
	if err == nil {
		t.Fatal("expected error from fn")
	}
	s, _ := LoadStack(root, "feat")
	if len(s.Nodes) != 1 {
		t.Errorf("save must be skipped when fn errors; got %d nodes", len(s.Nodes))
	}
}

func TestWithLocalLockNoLostUpdates(t *testing.T) {
	root := sdfRepo(t)
	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := branchName(i)
			_ = WithLocalLock(root, func(ls *LocalState) error {
				if ls.WorktreeProgress == nil {
					ls.WorktreeProgress = map[string]*SyncProgress{}
				}
				ls.WorktreeProgress[key] = &SyncProgress{PausedAt: key}
				if ls.SplitSessions == nil {
					ls.SplitSessions = map[string]string{}
				}
				ls.SplitSessions[key] = key
				return nil
			})
		}(i)
	}
	wg.Wait()
	ls, err := LoadLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ls.WorktreeProgress) != n {
		t.Fatalf("lost WorktreeProgress updates: got %d, want %d", len(ls.WorktreeProgress), n)
	}
	if len(ls.SplitSessions) != n {
		t.Fatalf("lost SplitSessions updates: got %d, want %d", len(ls.SplitSessions), n)
	}
}

func TestWithLocalLockSkipsSaveOnError(t *testing.T) {
	root := sdfRepo(t)
	_ = SaveLocal(root, &LocalState{SplitSessions: map[string]string{"keep": "1"}})
	want := errString("boom")
	err := WithLocalLock(root, func(ls *LocalState) error {
		ls.SplitSessions["clobber"] = "2"
		return want
	})
	if err == nil {
		t.Fatal("expected error from fn")
	}
	ls, _ := LoadLocal(root)
	if len(ls.SplitSessions) != 1 {
		t.Errorf("save must be skipped when fn errors; got %d sessions", len(ls.SplitSessions))
	}
}

func branchName(i int) string { return "b" + string(rune('a'+i%26)) + string(rune('a'+i/26)) }

type errString string

func (e errString) Error() string { return string(e) }
