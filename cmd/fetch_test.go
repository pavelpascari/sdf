package cmd

import (
	"testing"

	ghpkg "github.com/pavelpascari/sdf/internal/gh"
	"github.com/pavelpascari/sdf/internal/testutil"
)

func TestDiscoverStackFromBranch_WalksBaseChain(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.FakeBin(t, dir, "gh", map[string]string{
		"pr view feature/top":     `{"number":103,"headRefName":"feature/top","state":"OPEN","baseRefName":"feature/mid","url":"https://github.com/test/repo/pull/103"}`,
		"pr view feature/mid":     `{"number":102,"headRefName":"feature/mid","state":"OPEN","baseRefName":"feature/base","url":"https://github.com/test/repo/pull/102"}`,
		"pr view feature/base":    `{"number":101,"headRefName":"feature/base","state":"OPEN","baseRefName":"main","url":"https://github.com/test/repo/pull/101"}`,
		"pr view main":            "",
		"version":                 "gh version 2.50.0",
		"pr list --author @me":    "[]",
		"pr list --state all":     "[]",
		"pr list --state open":    "[]",
		"pr create --title":       "",
		"pr edit --base":          "",
		"pr merge --squash":       "",
		"release view --latest":   "",
		"repo view --json":        "",
		"issue list --state open": "",
	})
	testutil.SetBinary(t, &ghpkg.Binary, fake)

	ds, err := discoverStackFromBranch("feature/top")
	if err != nil {
		t.Fatalf("discoverStackFromBranch failed: %v", err)
	}

	if ds.Base != "main" {
		t.Fatalf("base = %q, want %q", ds.Base, "main")
	}
	if len(ds.Chains) != 3 {
		t.Fatalf("expected 3 chain nodes, got %d", len(ds.Chains))
	}
	if ds.Chains[0].HeadRefName != "feature/base" ||
		ds.Chains[1].HeadRefName != "feature/mid" ||
		ds.Chains[2].HeadRefName != "feature/top" {
		t.Fatalf("unexpected chain order: %+v", ds.Chains)
	}
}

func TestDiscoverStackFromBranch_RequiresOpenStartingPR(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.FakeBin(t, dir, "gh", map[string]string{
		"pr view feature/top": `{"number":103,"headRefName":"feature/top","state":"MERGED","baseRefName":"main","url":"https://github.com/test/repo/pull/103"}`,
	})
	testutil.SetBinary(t, &ghpkg.Binary, fake)

	_, err := discoverStackFromBranch("feature/top")
	if err == nil {
		t.Fatal("expected error for non-open starting PR")
	}
}
