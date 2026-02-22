package gh

import (
	"path/filepath"
	"testing"

	"github.com/pavelpascari/sdf/internal/testutil"
)

func TestPRList_ParsesJSON(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.FakeBin(t, dir, "gh", map[string]string{
		"pr list": `[
			{"number":1,"headRefName":"feat/a","state":"OPEN","baseRefName":"main","url":"https://github.com/test/repo/pull/1"},
			{"number":2,"headRefName":"feat/b","state":"MERGED","baseRefName":"feat/a","url":"https://github.com/test/repo/pull/2"},
			{"number":3,"headRefName":"feat/c","state":"OPEN","baseRefName":"feat/b","url":"https://github.com/test/repo/pull/3"}
		]`,
	})
	testutil.SetBinary(t, &Binary, fake)

	prs, err := PRList([]string{"feat/a", "feat/b", "feat/c"})
	if err != nil {
		t.Fatalf("PRList failed: %v", err)
	}

	if len(prs) != 3 {
		t.Fatalf("expected 3 PRs, got %d", len(prs))
	}

	// Verify the log recorded the correct arguments
	log := testutil.ReadLog(t, dir, "gh")
	if len(log) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(log))
	}
	if log[0] != "pr list --state all --json number,headRefName,state,baseRefName,url --limit 100" {
		t.Errorf("unexpected arguments: %s", log[0])
	}

	// Check parsed data
	byBranch := make(map[string]PRInfo)
	for _, pr := range prs {
		byBranch[pr.HeadRefName] = pr
	}
	if byBranch["feat/a"].State != "OPEN" {
		t.Errorf("expected feat/a state OPEN, got %s", byBranch["feat/a"].State)
	}
	if byBranch["feat/b"].State != "MERGED" {
		t.Errorf("expected feat/b state MERGED, got %s", byBranch["feat/b"].State)
	}
}

func TestPRList_FiltersBranches(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.FakeBin(t, dir, "gh", map[string]string{
		"pr list": `[
			{"number":1,"headRefName":"feat/a","state":"OPEN","baseRefName":"main","url":""},
			{"number":2,"headRefName":"unrelated","state":"OPEN","baseRefName":"main","url":""}
		]`,
	})
	testutil.SetBinary(t, &Binary, fake)

	prs, err := PRList([]string{"feat/a"})
	if err != nil {
		t.Fatalf("PRList failed: %v", err)
	}

	if len(prs) != 1 {
		t.Fatalf("expected 1 filtered PR, got %d", len(prs))
	}
	if prs[0].HeadRefName != "feat/a" {
		t.Errorf("expected feat/a, got %s", prs[0].HeadRefName)
	}
}

func TestPRList_KeepsBestState(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.FakeBin(t, dir, "gh", map[string]string{
		"pr list": `[
			{"number":1,"headRefName":"feat/a","state":"CLOSED","baseRefName":"main","url":""},
			{"number":2,"headRefName":"feat/a","state":"OPEN","baseRefName":"main","url":""}
		]`,
	})
	testutil.SetBinary(t, &Binary, fake)

	prs, err := PRList([]string{"feat/a"})
	if err != nil {
		t.Fatalf("PRList failed: %v", err)
	}

	if len(prs) != 1 {
		t.Fatalf("expected 1 PR (best state), got %d", len(prs))
	}
	if prs[0].Number != 2 {
		t.Errorf("expected PR #2 (OPEN wins over CLOSED), got #%d", prs[0].Number)
	}
}

func TestPRCreate_Arguments(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.FakeBin(t, dir, "gh", map[string]string{
		"pr create": "https://github.com/test/repo/pull/42",
	})
	testutil.SetBinary(t, &Binary, fake)

	url, err := PRCreate("feat: add auth", "PR body here", "main", "feat/auth")
	if err != nil {
		t.Fatalf("PRCreate failed: %v", err)
	}

	if url != "https://github.com/test/repo/pull/42" {
		t.Errorf("unexpected URL: %s", url)
	}

	log := testutil.ReadLog(t, dir, "gh")
	if len(log) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(log))
	}
	expected := "pr create --title feat: add auth --body PR body here --head feat/auth --base main"
	if log[0] != expected {
		t.Errorf("unexpected arguments:\n  got:  %s\n  want: %s", log[0], expected)
	}
}

func TestPRView_ParsesJSON(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.FakeBin(t, dir, "gh", map[string]string{
		"pr view": `{"number":42,"headRefName":"feat/auth","state":"OPEN","baseRefName":"main","url":"https://github.com/test/repo/pull/42"}`,
	})
	testutil.SetBinary(t, &Binary, fake)

	pr, err := PRView("feat/auth")
	if err != nil {
		t.Fatalf("PRView failed: %v", err)
	}

	if pr.Number != 42 {
		t.Errorf("expected PR #42, got #%d", pr.Number)
	}
	if pr.State != "OPEN" {
		t.Errorf("expected state OPEN, got %s", pr.State)
	}
}

func TestPREditBase_Arguments(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.FakeBin(t, dir, "gh", map[string]string{
		"pr edit": "",
	})
	testutil.SetBinary(t, &Binary, fake)

	if err := PREditBase(42, "main"); err != nil {
		t.Fatalf("PREditBase failed: %v", err)
	}

	log := testutil.ReadLog(t, dir, "gh")
	if len(log) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(log))
	}
	if log[0] != "pr edit 42 --base main" {
		t.Errorf("unexpected arguments: %s", log[0])
	}
}

func TestPRMerge_Arguments(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.FakeBin(t, dir, "gh", map[string]string{
		"pr merge": "",
	})
	testutil.SetBinary(t, &Binary, fake)

	if err := PRMerge(42, "squash"); err != nil {
		t.Fatalf("PRMerge failed: %v", err)
	}

	log := testutil.ReadLog(t, dir, "gh")
	if len(log) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(log))
	}
	if log[0] != "pr merge 42 --squash --delete-branch" {
		t.Errorf("unexpected arguments: %s", log[0])
	}
}

func TestPRViewBody_ParsesJSON(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.FakeBin(t, dir, "gh", map[string]string{
		"pr view": `{"body":"This is the PR body.\n\nWith multiple lines."}`,
	})
	testutil.SetBinary(t, &Binary, fake)

	body, err := PRViewBody(42)
	if err != nil {
		t.Fatalf("PRViewBody failed: %v", err)
	}

	if body != "This is the PR body.\n\nWith multiple lines." {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestAvailable_WithFakeBinary(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.FakeBin(t, dir, "gh-test", map[string]string{})
	testutil.SetBinary(t, &Binary, fake)

	if !Available() {
		t.Error("expected Available()=true with fake binary on PATH")
	}
}

func TestAvailable_Missing(t *testing.T) {
	testutil.SetBinary(t, &Binary, filepath.Join(t.TempDir(), "nonexistent"))

	if Available() {
		t.Error("expected Available()=false with nonexistent binary")
	}
}

func TestPRList_ErrorFromBinary(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.FakeBinFail(t, dir, "gh", "authentication required")
	testutil.SetBinary(t, &Binary, fake)

	_, err := PRList([]string{"feat/a"})
	if err == nil {
		t.Fatal("expected error from failing binary")
	}
}
