package cmd

import (
	"os/exec"
	"strings"
	"testing"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/stack"
	"github.com/pavelpascari/sdf/internal/testutil"
)

func TestRunPR_EmptyBranchAheadOfBase(t *testing.T) {
	dir := newTestRepo(t)

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
	}

	git("checkout", "-b", "test-stack/empty")

	if err := stack.Init(dir, "test-stack", "main"); err != nil {
		t.Fatal(err)
	}
	s, err := stack.LoadStack(dir, "test-stack")
	if err != nil {
		t.Fatal(err)
	}
	revParse := exec.Command("git", "rev-parse", "main")
	revParse.Dir = dir
	mainTip, err := revParse.Output()
	if err != nil {
		t.Fatal(err)
	}
	s.Nodes = []stack.Node{
		{Branch: "test-stack/empty", Status: "open", BaseTip: strings.TrimSpace(string(mainTip))},
	}
	if err := stack.Save(dir, s); err != nil {
		t.Fatal(err)
	}

	fakeDir := t.TempDir()
	fake := testutil.GHFakeBin(t, fakeDir)
	testutil.SetBinary(t, &ghpkg.Binary, fake)

	err = RunPR(nil)
	if err == nil {
		t.Fatal("expected no-commits-ahead error")
	}
	if !strings.Contains(err.Error(), "has no commits ahead of main") {
		t.Fatalf("unexpected error: %v", err)
	}

	log := testutil.ReadLog(t, fakeDir, "gh")
	if len(log) != 0 {
		t.Fatalf("expected no gh calls for empty branch, got %v", log)
	}
}
