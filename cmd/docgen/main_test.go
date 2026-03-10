package main

import "testing"

func TestComputeHash_DeterministicAndIgnoresHashField(t *testing.T) {
	ref := CLIReference{
		Commands: []CommandDoc{
			{Name: "status", Use: "status", Short: "Show status"},
		},
	}

	h1 := computeHash(ref)
	if h1 == "" {
		t.Fatal("expected non-empty hash")
	}

	ref.Hash = "some-other-value"
	h2 := computeHash(ref)
	if h1 != h2 {
		t.Fatalf("expected hash to ignore Hash field, got %q vs %q", h1, h2)
	}
}
