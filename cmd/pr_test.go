package cmd

import (
	"strings"
	"testing"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/testutil"
	"github.com/spf13/cobra"
)

func TestRunPR_EmptyBranchShowsFriendlyError(t *testing.T) {
	dir := newTestRepo(t)

	if err := stack.Init(dir, "my-stack", "main"); err != nil {
		t.Fatal(err)
	}

	s, err := stack.LoadStack(dir, "my-stack")
	if err != nil {
		t.Fatal(err)
	}
	s.Nodes = append(s.Nodes, stack.Node{
		Branch: "main",
		Status: "open",
	})
	if err := stack.Save(dir, s); err != nil {
		t.Fatal(err)
	}

	fake := testutil.GHFakeBin(t, t.TempDir())
	testutil.SetBinary(t, &ghpkg.Binary, fake)

	c := &cobra.Command{}
	c.Flags().String("title", "", "")
	c.Flags().Bool("json", false, "")

	err = runPR(c, nil)
	if err == nil {
		t.Fatal("expected error for empty branch")
	}
	if !strings.Contains(err.Error(), "has no commits ahead of") {
		t.Fatalf("expected friendly empty-branch message, got: %v", err)
	}
}
