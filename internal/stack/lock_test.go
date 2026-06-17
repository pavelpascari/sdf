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
