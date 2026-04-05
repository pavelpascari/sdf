package ops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOperationSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	sdfDir := filepath.Join(dir, ".sdf")
	os.MkdirAll(sdfDir, 0755)

	op := &Operation{
		Command:        "sync",
		StackID:        "my-feature",
		OriginalBranch: "feat/auth",
		Snapshot:       map[string]string{"feat/auth": "abc123", "feat/api": "def456"},
		Steps: []*Step{
			{ID: "rebase-auth", Kind: KindGitRebase, Phase: PhaseMutation, Status: StatusPending,
				Inputs: map[string]Value{"branch": Lit("feat/auth"), "onto": Lit("main")}},
		},
	}

	if err := Save(dir, op); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}
	if loaded.Command != "sync" {
		t.Errorf("Command = %q, want %q", loaded.Command, "sync")
	}
	if loaded.Snapshot["feat/auth"] != "abc123" {
		t.Errorf("Snapshot[feat/auth] = %q, want %q", loaded.Snapshot["feat/auth"], "abc123")
	}
	if len(loaded.Steps) != 1 || loaded.Steps[0].ID != "rebase-auth" {
		t.Errorf("Steps not preserved: %+v", loaded.Steps)
	}
}

func TestOperationLoadReturnsNilWhenNoOperation(t *testing.T) {
	dir := t.TempDir()
	sdfDir := filepath.Join(dir, ".sdf")
	os.MkdirAll(sdfDir, 0755)
	os.WriteFile(filepath.Join(sdfDir, "local.json"), []byte("{}\n"), 0644)

	op, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if op != nil {
		t.Fatalf("expected nil operation, got %+v", op)
	}
}

func TestOperationClear(t *testing.T) {
	dir := t.TempDir()
	sdfDir := filepath.Join(dir, ".sdf")
	os.MkdirAll(sdfDir, 0755)

	op := &Operation{Command: "restack", Steps: []*Step{}}
	Save(dir, op)

	if err := Clear(dir); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	loaded, _ := Load(dir)
	if loaded != nil {
		t.Fatalf("expected nil after Clear, got %+v", loaded)
	}
}
