package gh

import (
	"path/filepath"
	"testing"

	"github.com/pavelpascari/sdf/internal/testutil"
)

func TestPRList_ParsesJSON(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.GHFakeBinWith(t, dir, map[string]string{
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
	// Should use --search with head: qualifiers to scope the query
	expected := "pr list --state all --json number,headRefName,state,baseRefName,url,statusCheckRollup,reviewDecision,mergeable,isDraft --search head:feat/a head:feat/b head:feat/c --limit 10"
	if log[0] != expected {
		t.Errorf("unexpected arguments:\n  got:  %s\n  want: %s", log[0], expected)
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
	fake := testutil.GHFakeBinWith(t, dir, map[string]string{
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
	fake := testutil.GHFakeBinWith(t, dir, map[string]string{
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

func TestPRList_EmptyBranches(t *testing.T) {
	// PRList with no branches should return nil without calling gh
	prs, err := PRList(nil)
	if err != nil {
		t.Fatalf("PRList(nil) failed: %v", err)
	}
	if prs != nil {
		t.Fatalf("expected nil, got %v", prs)
	}

	prs, err = PRList([]string{})
	if err != nil {
		t.Fatalf("PRList([]) failed: %v", err)
	}
	if prs != nil {
		t.Fatalf("expected nil, got %v", prs)
	}
}

func TestPRListByBase_Arguments(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.GHFakeBinWith(t, dir, map[string]string{
		"pr list": `[
			{"number":21,"headRefName":"feat/new","state":"OPEN","baseRefName":"feat/a","url":"https://github.com/test/repo/pull/21"}
		]`,
	})
	testutil.SetBinary(t, &Binary, fake)

	prs, err := PRListByBase([]string{"feat/a", "feat/b"})
	if err != nil {
		t.Fatalf("PRListByBase failed: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(prs))
	}

	log := testutil.ReadLog(t, dir, "gh")
	if len(log) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(log))
	}
	expected := "pr list --state open --json number,headRefName,state,baseRefName,url,statusCheckRollup,reviewDecision,mergeable,isDraft --search base:feat/a base:feat/b --limit 10"
	if log[0] != expected {
		t.Errorf("unexpected arguments:\n  got:  %s\n  want: %s", log[0], expected)
	}
}

func TestPRListByBase_FiltersBaseBranch(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.GHFakeBinWith(t, dir, map[string]string{
		"pr list": `[
			{"number":21,"headRefName":"feat/new","state":"OPEN","baseRefName":"feat/a","url":""},
			{"number":22,"headRefName":"feat/unrelated","state":"OPEN","baseRefName":"other","url":""}
		]`,
	})
	testutil.SetBinary(t, &Binary, fake)

	prs, err := PRListByBase([]string{"feat/a"})
	if err != nil {
		t.Fatalf("PRListByBase failed: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("expected 1 filtered PR, got %d", len(prs))
	}
	if prs[0].HeadRefName != "feat/new" {
		t.Errorf("expected feat/new, got %s", prs[0].HeadRefName)
	}
}

func TestPRListByBase_EmptyBranches(t *testing.T) {
	prs, err := PRListByBase(nil)
	if err != nil {
		t.Fatalf("PRListByBase(nil) failed: %v", err)
	}
	if prs != nil {
		t.Fatalf("expected nil, got %v", prs)
	}
}

func TestMergePRResults(t *testing.T) {
	primary := []PRInfo{
		{Number: 1, HeadRefName: "feat/a"},
		{Number: 2, HeadRefName: "feat/b"},
	}

	t.Run("empty child returns primary", func(t *testing.T) {
		got := MergePRResults(primary, nil)
		if len(got) != 2 {
			t.Fatalf("expected 2 PRs, got %d", len(got))
		}
	})

	t.Run("adds new child PRs", func(t *testing.T) {
		child := []PRInfo{
			{Number: 3, HeadRefName: "feat/c"},
		}
		got := MergePRResults(primary, child)
		if len(got) != 3 {
			t.Fatalf("expected 3 PRs, got %d", len(got))
		}
	})

	t.Run("deduplicates by head branch", func(t *testing.T) {
		child := []PRInfo{
			{Number: 99, HeadRefName: "feat/a"}, // duplicate
			{Number: 3, HeadRefName: "feat/c"},  // new
		}
		got := MergePRResults(primary, child)
		if len(got) != 3 {
			t.Fatalf("expected 3 PRs, got %d", len(got))
		}
		// primary takes precedence
		for _, pr := range got {
			if pr.HeadRefName == "feat/a" && pr.Number != 1 {
				t.Errorf("expected primary PR #1 for feat/a, got #%d", pr.Number)
			}
		}
	})
}

func TestPRCreate_Arguments(t *testing.T) {
	dir := t.TempDir()
	fake := testutil.GHFakeBinWith(t, dir, map[string]string{
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
	fake := testutil.GHFakeBinWith(t, dir, map[string]string{
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
	fake := testutil.GHFakeBinWith(t, dir, map[string]string{
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
	fake := testutil.GHFakeBinWith(t, dir, map[string]string{
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
	// PRViewBody uses "pr view" prefix — the canonical default is the full
	// PR fields variant, but this test needs the body variant. We use FakeBin
	// directly with explicit validation for the body shape.
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

func TestAggregateCheckStatus(t *testing.T) {
	tests := []struct {
		name   string
		checks []CheckRun
		want   string
	}{
		{name: "empty", checks: nil, want: ""},
		{name: "all pass", checks: []CheckRun{
			{Name: "Lint", Status: "COMPLETED", Conclusion: "SUCCESS"},
			{Name: "Test", Status: "COMPLETED", Conclusion: "SUCCESS"},
		}, want: "pass"},
		{name: "skipped counts as pass", checks: []CheckRun{
			{Name: "Lint", Status: "COMPLETED", Conclusion: "SUCCESS"},
			{Name: "Auto-merge", Status: "COMPLETED", Conclusion: "SKIPPED"},
		}, want: "pass"},
		{name: "one failure", checks: []CheckRun{
			{Name: "Lint", Status: "COMPLETED", Conclusion: "SUCCESS"},
			{Name: "Test", Status: "COMPLETED", Conclusion: "FAILURE"},
		}, want: "fail"},
		{name: "canceled is fail", checks: []CheckRun{
			{Name: "Build", Status: "COMPLETED", Conclusion: "CANCELED"},
		}, want: "fail"},
		{name: "in progress", checks: []CheckRun{
			{Name: "Lint", Status: "COMPLETED", Conclusion: "SUCCESS"},
			{Name: "Test", Status: "IN_PROGRESS", Conclusion: ""},
		}, want: "pending"},
		{name: "queued", checks: []CheckRun{
			{Name: "Test", Status: "QUEUED", Conclusion: ""},
		}, want: "pending"},
		{name: "failure beats pending", checks: []CheckRun{
			{Name: "Lint", Status: "IN_PROGRESS", Conclusion: ""},
			{Name: "Test", Status: "COMPLETED", Conclusion: "FAILURE"},
		}, want: "fail"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AggregateCheckStatus(tt.checks)
			if got != tt.want {
				t.Errorf("AggregateCheckStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}
