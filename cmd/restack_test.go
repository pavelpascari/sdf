package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitpkg "github.com/pavelpascari/sdf/internal/git"
	"github.com/pavelpascari/sdf/internal/ops"
	"github.com/pavelpascari/sdf/internal/stack"
)

func TestReorderNodes_MoveMiddleToFirst(t *testing.T) {
	nodes := []stack.Node{
		{Branch: "a", Status: "open"},
		{Branch: "b", Status: "open"},
		{Branch: "c", Status: "open"},
		{Branch: "d", Status: "open"},
	}
	result := reorderNodes(nodes, "c", "a")
	expected := []string{"a", "c", "b", "d"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d nodes, got %d", len(expected), len(result))
	}
	for i, name := range expected {
		if result[i].Branch != name {
			t.Errorf("position %d: expected %s, got %s", i, name, result[i].Branch)
		}
	}
}

func TestReorderNodes_MoveToBase(t *testing.T) {
	nodes := []stack.Node{
		{Branch: "a", Status: "open"},
		{Branch: "b", Status: "open"},
		{Branch: "c", Status: "open"},
	}
	result := reorderNodes(nodes, "c", "")
	expected := []string{"c", "a", "b"}
	for i, name := range expected {
		if result[i].Branch != name {
			t.Errorf("position %d: expected %s, got %s", i, name, result[i].Branch)
		}
	}
}

func TestReorderNodes_MoveForward(t *testing.T) {
	nodes := []stack.Node{
		{Branch: "a", Status: "open"},
		{Branch: "b", Status: "open"},
		{Branch: "c", Status: "open"},
		{Branch: "d", Status: "open"},
	}
	result := reorderNodes(nodes, "a", "c")
	expected := []string{"b", "c", "a", "d"}
	for i, name := range expected {
		if result[i].Branch != name {
			t.Errorf("position %d: expected %s, got %s", i, name, result[i].Branch)
		}
	}
}

func TestReorderNodes_PreservesFields(t *testing.T) {
	nodes := []stack.Node{
		{Branch: "a", PR: 1, Status: "open", BaseTip: "aaa"},
		{Branch: "b", PR: 2, Status: "open", BaseTip: "bbb"},
		{Branch: "c", PR: 3, Status: "open", BaseTip: "ccc"},
	}
	result := reorderNodes(nodes, "c", "a")
	if result[1].Branch != "c" || result[1].PR != 3 || result[1].BaseTip != "ccc" {
		t.Errorf("moved node lost fields: %+v", result[1])
	}
}

func TestReorderNodes_AdjacentSwap(t *testing.T) {
	nodes := []stack.Node{
		{Branch: "a", Status: "open"},
		{Branch: "b", Status: "open"},
		{Branch: "c", Status: "open"},
	}
	result := reorderNodes(nodes, "b", "c")
	expected := []string{"a", "c", "b"}
	for i, name := range expected {
		if result[i].Branch != name {
			t.Errorf("position %d: expected %s, got %s", i, name, result[i].Branch)
		}
	}
}

func TestComputeRestackPlan_IdentifiesAffected(t *testing.T) {
	s := &stack.Stack{
		StackID: "test", Base: "main",
		Nodes: []stack.Node{
			{Branch: "a", Status: "open"},
			{Branch: "b", Status: "open"},
			{Branch: "c", Status: "open"},
			{Branch: "d", Status: "open"},
		},
	}
	newNodes := reorderNodes(s.Nodes, "c", "a")
	affected := computeRestackPlan(s, newNodes)
	if len(affected) != 3 {
		t.Fatalf("expected 3 affected branches, got %d", len(affected))
	}
	expects := map[string]string{"c": "a", "b": "c", "d": "b"}
	for _, a := range affected {
		want, ok := expects[a.Branch]
		if !ok {
			t.Errorf("unexpected affected branch: %s", a.Branch)
			continue
		}
		if a.NewParent != want {
			t.Errorf("branch %s: expected new parent %s, got %s", a.Branch, want, a.NewParent)
		}
	}
}

func TestComputeRestackPlan_NoOpWhenSamePosition(t *testing.T) {
	s := &stack.Stack{
		StackID: "test", Base: "main",
		Nodes: []stack.Node{
			{Branch: "a", Status: "open"},
			{Branch: "b", Status: "open"},
			{Branch: "c", Status: "open"},
		},
	}
	newNodes := reorderNodes(s.Nodes, "b", "a")
	affected := computeRestackPlan(s, newNodes)
	if len(affected) != 0 {
		t.Errorf("expected 0 affected branches for no-op, got %d", len(affected))
	}
}

func TestComputeRestackPlan_SkipsMergedNodes(t *testing.T) {
	s := &stack.Stack{
		StackID: "test", Base: "main",
		Nodes: []stack.Node{
			{Branch: "a", Status: "open"},
			{Branch: "b", Status: "merged"},
			{Branch: "c", Status: "open"},
			{Branch: "d", Status: "open"},
		},
	}
	newNodes := reorderNodes(s.Nodes, "d", "a")
	affected := computeRestackPlan(s, newNodes)
	for _, a := range affected {
		if a.Branch == "b" {
			t.Error("merged branch b should not be in affected list")
		}
	}
}

func TestRestackValidation_BranchNotInStack(t *testing.T) {
	err := validateRestack(
		&stack.Stack{
			StackID: "test", Base: "main",
			Nodes: []stack.Node{{Branch: "a", Status: "open"}},
		},
		"nonexistent", "a",
	)
	if err == nil || !strings.Contains(err.Error(), "not part of stack") {
		t.Errorf("expected 'not part of stack' error, got: %v", err)
	}
}

func TestRestackValidation_AfterNotInStack(t *testing.T) {
	err := validateRestack(
		&stack.Stack{
			StackID: "test", Base: "main",
			Nodes: []stack.Node{
				{Branch: "a", Status: "open"},
				{Branch: "b", Status: "open"},
			},
		},
		"b", "nonexistent",
	)
	if err == nil || !strings.Contains(err.Error(), "not part of stack") {
		t.Errorf("expected 'not part of stack' error, got: %v", err)
	}
}

func TestRestackValidation_AfterSelf(t *testing.T) {
	err := validateRestack(
		&stack.Stack{
			StackID: "test", Base: "main",
			Nodes: []stack.Node{
				{Branch: "a", Status: "open"},
				{Branch: "b", Status: "open"},
			},
		},
		"b", "b",
	)
	if err == nil || !strings.Contains(err.Error(), "cannot move") {
		t.Errorf("expected 'cannot move' error, got: %v", err)
	}
}

func TestRestackValidation_AlreadyInPosition(t *testing.T) {
	err := validateRestack(
		&stack.Stack{
			StackID: "test", Base: "main",
			Nodes: []stack.Node{
				{Branch: "a", Status: "open"},
				{Branch: "b", Status: "open"},
				{Branch: "c", Status: "open"},
			},
		},
		"b", "a",
	)
	if err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("expected 'already in position' error, got: %v", err)
	}
}

func TestRestackValidation_AfterBase(t *testing.T) {
	err := validateRestack(
		&stack.Stack{
			StackID: "test", Base: "main",
			Nodes: []stack.Node{
				{Branch: "a", Status: "open"},
				{Branch: "b", Status: "open"},
			},
		},
		"b", "main",
	)
	if err != nil {
		t.Errorf("expected no error for --after base, got: %v", err)
	}
}

func TestRestackValidation_ValidMove(t *testing.T) {
	err := validateRestack(
		&stack.Stack{
			StackID: "test", Base: "main",
			Nodes: []stack.Node{
				{Branch: "a", Status: "open"},
				{Branch: "b", Status: "open"},
				{Branch: "c", Status: "open"},
			},
		},
		"c", "a",
	)
	if err != nil {
		t.Errorf("expected no error for valid move, got: %v", err)
	}
}

func restackTestRepo(t *testing.T) string {
	t.Helper()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", strings.Join(args, " "), string(out))
		}
		return strings.TrimSpace(string(out))
	}

	writeFile := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "-b", "main")
	git("config", "user.email", "test@test.com")
	git("config", "user.name", "Test")
	git("config", "commit.gpgsign", "false")

	writeFile("README.md", "# test\n")
	git("add", "README.md")
	git("commit", "-m", "initial")
	mainTip := git("rev-parse", "HEAD")

	git("checkout", "-b", "branchA")
	writeFile("a1.txt", "a1\n")
	git("add", "a1.txt")
	git("commit", "-m", "a1")
	branchATip := git("rev-parse", "HEAD")

	git("checkout", "-b", "branchB")
	writeFile("b1.txt", "b1\n")
	git("add", "b1.txt")
	git("commit", "-m", "b1")
	branchBTip := git("rev-parse", "HEAD")

	git("checkout", "-b", "branchC")
	writeFile("c1.txt", "c1\n")
	git("add", "c1.txt")
	git("commit", "-m", "c1")
	branchCTip := git("rev-parse", "HEAD")

	git("checkout", "-b", "branchD")
	writeFile("d1.txt", "d1\n")
	git("add", "d1.txt")
	git("commit", "-m", "d1")

	s := &stack.Stack{
		StackID: "test-stack",
		Base:    "main",
		Nodes: []stack.Node{
			{Branch: "branchA", Status: "open", BaseTip: mainTip},
			{Branch: "branchB", Status: "open", BaseTip: branchATip},
			{Branch: "branchC", Status: "open", BaseTip: branchBTip},
			{Branch: "branchD", Status: "open", BaseTip: branchCTip},
		},
	}
	if err := stack.Save(dir, s); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestRunRestack_MoveCAfterA(t *testing.T) {
	restackTestRepo(t)

	err := runRestackLogic("branchC", "branchA", false, false)
	if err != nil {
		t.Fatalf("restack failed: %v", err)
	}

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	expected := []string{"branchA", "branchC", "branchB", "branchD"}
	for i, name := range expected {
		if s.Nodes[i].Branch != name {
			t.Errorf("position %d: expected %s, got %s", i, name, s.Nodes[i].Branch)
		}
	}

	// Verify branchC has a1.txt (from parent branchA) but not b1.txt
	gitpkg.Checkout("branchC")
	if _, err := os.Stat("a1.txt"); err != nil {
		t.Error("branchC should have a1.txt from parent branchA")
	}
	if _, err := os.Stat("b1.txt"); err == nil {
		t.Error("branchC should NOT have b1.txt (branchB is no longer its parent)")
	}
	if _, err := os.Stat("c1.txt"); err != nil {
		t.Error("branchC should still have its own c1.txt")
	}

	// Verify branchB now has c1.txt (from new parent branchC)
	gitpkg.Checkout("branchB")
	if _, err := os.Stat("c1.txt"); err != nil {
		t.Error("branchB should have c1.txt from new parent branchC")
	}
	if _, err := os.Stat("b1.txt"); err != nil {
		t.Error("branchB should still have its own b1.txt")
	}

	// Verify branchD has everything (end of chain)
	gitpkg.Checkout("branchD")
	for _, f := range []string{"a1.txt", "c1.txt", "b1.txt", "d1.txt"} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("branchD should have %s", f)
		}
	}
}

func TestRunRestackAbort_NoRestackInProgress(t *testing.T) {
	restackTestRepo(t)
	err := runRestackAbort()
	if err == nil || !strings.Contains(err.Error(), "no restack in progress") {
		t.Errorf("expected 'no restack in progress' error, got: %v", err)
	}
}

func TestRunRestackContinue_NoRestackInProgress(t *testing.T) {
	restackTestRepo(t)
	err := runRestackContinue()
	if err == nil || !strings.Contains(err.Error(), "no restack in progress") {
		t.Errorf("expected 'no restack in progress' error, got: %v", err)
	}
}

func TestRunRestack_SnapshotSavedAndCleared(t *testing.T) {
	dir := restackTestRepo(t)

	// Before restack — no operation
	op, _ := ops.Load(dir)
	if op != nil {
		t.Fatal("expected no operation before restack")
	}

	err := runRestackLogic("branchC", "branchA", false, false)
	if err != nil {
		t.Fatalf("restack failed: %v", err)
	}

	// After successful restack — operation should be cleared
	op, _ = ops.Load(dir)
	if op != nil {
		t.Error("expected operation to be cleared after success")
	}
}

func TestRunRestack_MoveToBase(t *testing.T) {
	restackTestRepo(t)

	err := runRestackLogic("branchC", "main", false, false)
	if err != nil {
		t.Fatalf("restack failed: %v", err)
	}

	s, err := stack.Load(".")
	if err != nil {
		t.Fatal(err)
	}

	expected := []string{"branchC", "branchA", "branchB", "branchD"}
	for i, name := range expected {
		if s.Nodes[i].Branch != name {
			t.Errorf("position %d: expected %s, got %s", i, name, s.Nodes[i].Branch)
		}
	}

	// branchC should only have its own file + README (parent is main)
	gitpkg.Checkout("branchC")
	if _, err := os.Stat("c1.txt"); err != nil {
		t.Error("branchC should have c1.txt")
	}
	if _, err := os.Stat("a1.txt"); err == nil {
		t.Error("branchC should NOT have a1.txt (main is its parent now)")
	}
}
