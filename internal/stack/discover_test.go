package stack

import (
	"testing"
)

func TestDiscoverStacks_SimpleChain(t *testing.T) {
	// main ← A ← B ← C
	prs := []PRRecord{
		{Number: 1, HeadRefName: "feat/a", BaseRefName: "main"},
		{Number: 2, HeadRefName: "feat/b", BaseRefName: "feat/a"},
		{Number: 3, HeadRefName: "feat/c", BaseRefName: "feat/b"},
	}

	stacks := DiscoverStacks(prs, "main")

	if len(stacks) != 1 {
		t.Fatalf("expected 1 stack, got %d", len(stacks))
	}

	s := stacks[0]
	if s.Base != "main" {
		t.Errorf("expected base 'main', got %q", s.Base)
	}
	if len(s.Chains) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(s.Chains))
	}
	if s.Chains[0].HeadRefName != "feat/a" {
		t.Errorf("expected first node 'feat/a', got %q", s.Chains[0].HeadRefName)
	}
	if s.Chains[1].HeadRefName != "feat/b" {
		t.Errorf("expected second node 'feat/b', got %q", s.Chains[1].HeadRefName)
	}
	if s.Chains[2].HeadRefName != "feat/c" {
		t.Errorf("expected third node 'feat/c', got %q", s.Chains[2].HeadRefName)
	}
}

func TestDiscoverStacks_NoChainsOnlySingles(t *testing.T) {
	// All PRs target main directly — no chains
	prs := []PRRecord{
		{Number: 1, HeadRefName: "feat/a", BaseRefName: "main"},
		{Number: 2, HeadRefName: "feat/b", BaseRefName: "main"},
		{Number: 3, HeadRefName: "fix/c", BaseRefName: "main"},
	}

	stacks := DiscoverStacks(prs, "main")

	if len(stacks) != 0 {
		t.Fatalf("expected 0 stacks (no chains >= 2), got %d", len(stacks))
	}
}

func TestDiscoverStacks_TwoChain(t *testing.T) {
	// main ← A ← B (a stack of 2)
	prs := []PRRecord{
		{Number: 1, HeadRefName: "feat/a", BaseRefName: "main"},
		{Number: 2, HeadRefName: "feat/b", BaseRefName: "feat/a"},
	}

	stacks := DiscoverStacks(prs, "main")

	if len(stacks) != 1 {
		t.Fatalf("expected 1 stack, got %d", len(stacks))
	}
	if len(stacks[0].Chains) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(stacks[0].Chains))
	}
}

func TestDiscoverStacks_BranchingFork(t *testing.T) {
	// main ← A ← B
	//           ← C (fork from A)
	// This produces two stacks: [A, B] and [A, C]
	prs := []PRRecord{
		{Number: 1, HeadRefName: "feat/a", BaseRefName: "main"},
		{Number: 2, HeadRefName: "feat/b", BaseRefName: "feat/a"},
		{Number: 3, HeadRefName: "feat/c", BaseRefName: "feat/a"},
	}

	stacks := DiscoverStacks(prs, "main")

	if len(stacks) != 2 {
		t.Fatalf("expected 2 stacks (fork at A), got %d", len(stacks))
	}

	// Both stacks should start with feat/a
	for _, s := range stacks {
		if s.Chains[0].HeadRefName != "feat/a" {
			t.Errorf("expected first node to be 'feat/a', got %q", s.Chains[0].HeadRefName)
		}
	}
}

func TestDiscoverStacks_MultipleIndependentStacks(t *testing.T) {
	// main ← A ← B (stack 1)
	// main ← X ← Y (stack 2)
	prs := []PRRecord{
		{Number: 1, HeadRefName: "users/schema", BaseRefName: "main"},
		{Number: 2, HeadRefName: "users/repo", BaseRefName: "users/schema"},
		{Number: 3, HeadRefName: "auth/db", BaseRefName: "main"},
		{Number: 4, HeadRefName: "auth/api", BaseRefName: "auth/db"},
	}

	stacks := DiscoverStacks(prs, "main")

	if len(stacks) != 2 {
		t.Fatalf("expected 2 stacks, got %d", len(stacks))
	}
}

func TestDiscoverStacks_DifferentBase(t *testing.T) {
	// develop ← A ← B
	prs := []PRRecord{
		{Number: 1, HeadRefName: "feat/a", BaseRefName: "develop"},
		{Number: 2, HeadRefName: "feat/b", BaseRefName: "feat/a"},
	}

	stacks := DiscoverStacks(prs, "main")

	// Should still find the stack because "develop" isn't anyone's head
	// and isn't the default branch, so it's treated as an alternative root.
	if len(stacks) != 1 {
		t.Fatalf("expected 1 stack with alternative base, got %d", len(stacks))
	}
	if stacks[0].Base != "develop" {
		t.Errorf("expected base 'develop', got %q", stacks[0].Base)
	}
}

func TestDiscoverStacks_MixedChainAndSingles(t *testing.T) {
	// main ← A ← B ← C (stack)
	// main ← D (single, not a stack)
	// main ← E (single, not a stack)
	prs := []PRRecord{
		{Number: 1, HeadRefName: "feat/a", BaseRefName: "main"},
		{Number: 2, HeadRefName: "feat/b", BaseRefName: "feat/a"},
		{Number: 3, HeadRefName: "feat/c", BaseRefName: "feat/b"},
		{Number: 4, HeadRefName: "fix/d", BaseRefName: "main"},
		{Number: 5, HeadRefName: "fix/e", BaseRefName: "main"},
	}

	stacks := DiscoverStacks(prs, "main")

	if len(stacks) != 1 {
		t.Fatalf("expected 1 stack (the chain), got %d", len(stacks))
	}
	if len(stacks[0].Chains) != 3 {
		t.Fatalf("expected 3 nodes in the chain, got %d", len(stacks[0].Chains))
	}
}

func TestDiscoverStacks_EmptyInput(t *testing.T) {
	stacks := DiscoverStacks(nil, "main")
	if len(stacks) != 0 {
		t.Fatalf("expected 0 stacks for nil input, got %d", len(stacks))
	}

	stacks = DiscoverStacks([]PRRecord{}, "main")
	if len(stacks) != 0 {
		t.Fatalf("expected 0 stacks for empty input, got %d", len(stacks))
	}
}

func TestDiscoverStacks_PRNumbersPreserved(t *testing.T) {
	prs := []PRRecord{
		{Number: 42, HeadRefName: "feat/a", BaseRefName: "main"},
		{Number: 43, HeadRefName: "feat/b", BaseRefName: "feat/a"},
	}

	stacks := DiscoverStacks(prs, "main")

	if len(stacks) != 1 {
		t.Fatalf("expected 1 stack, got %d", len(stacks))
	}
	if stacks[0].Chains[0].Number != 42 {
		t.Errorf("expected PR #42, got #%d", stacks[0].Chains[0].Number)
	}
	if stacks[0].Chains[1].Number != 43 {
		t.Errorf("expected PR #43, got #%d", stacks[0].Chains[1].Number)
	}
}
