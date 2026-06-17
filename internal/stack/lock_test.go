// internal/stack/lock_test.go
package stack

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sdfRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, SDFDir, StacksDir), 0755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAcquireAndReleaseLock(t *testing.T) {
	root := sdfRepo(t)
	l, err := AcquireLock(root, "feat", time.Second)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, SDFDir, "feat.lock")); err != nil {
		t.Fatalf("lock file missing: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, SDFDir, "feat.lock")); !os.IsNotExist(err) {
		t.Errorf("lock file should be removed after release")
	}
}

func TestAcquireTimesOutWhenHeld(t *testing.T) {
	root := sdfRepo(t)
	l, err := AcquireLock(root, "feat", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()

	start := time.Now()
	_, err = AcquireLock(root, "feat", 200*time.Millisecond)
	if err == nil {
		t.Fatalf("expected timeout acquiring a held lock")
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Errorf("acquire returned too fast; did not wait for timeout")
	}
}

func TestStealsStaleLock(t *testing.T) {
	root := sdfRepo(t)
	// Write a lock owned by a definitely-dead PID with an old timestamp.
	stale := lockData{PID: 999999, Stamp: time.Now().Add(-time.Hour).Unix()}
	if err := writeLockFile(filepath.Join(root, SDFDir, "feat.lock"), stale); err != nil {
		t.Fatal(err)
	}
	l, err := AcquireLock(root, "feat", time.Second)
	if err != nil {
		t.Fatalf("should steal stale lock: %v", err)
	}
	l.Release()
}

func TestStaleLockReclaimsOldCorruptFile(t *testing.T) {
	root := sdfRepo(t)
	path := filepath.Join(root, SDFDir, "feat.lock")
	// Fresh corrupt file: NOT stale (being written) -> AcquireLock should time out fast.
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock(root, "feat", 150*time.Millisecond); err == nil {
		t.Fatal("fresh corrupt lock should not be stolen")
	}
	// Make it old: now it IS stale and should be reclaimed.
	old := time.Now().Add(-2 * staleAfter)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	l, err := AcquireLock(root, "feat", time.Second)
	if err != nil {
		t.Fatalf("old corrupt lock should be reclaimed: %v", err)
	}
	_ = l.Release()
}
