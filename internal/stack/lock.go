// internal/stack/lock.go
package stack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// LockTimeout bounds how long an sdf process waits to acquire a stack lock.
const LockTimeout = 10 * time.Second

// staleAfter is how old a lock may be before it is considered abandoned.
const staleAfter = 5 * time.Minute

// Lock is a held advisory lock on a stack's metadata.
type Lock struct {
	path string
}

type lockData struct {
	PID   int   `json:"pid"`
	Stamp int64 `json:"stamp"` // unix seconds
}

func lockPath(root, stackID string) string {
	return filepath.Join(root, SDFDir, stackID+".lock")
}

func writeLockFile(path string, d lockData) error {
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// AcquireLock obtains an exclusive lock for a stack, polling until timeout.
// A lock whose owner process is dead or older than staleAfter is stolen.
func AcquireLock(root, stackID string, timeout time.Duration) (*Lock, error) {
	path := lockPath(root, stackID)
	deadline := time.Now().Add(timeout)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			data, _ := json.Marshal(lockData{PID: os.Getpid(), Stamp: time.Now().Unix()})
			_, _ = f.Write(data)
			_ = f.Close()
			return &Lock{path: path}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("cannot create lock %s: %w", path, err)
		}
		if isStaleLock(path) {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for stack lock %s (another sdf process may be running)", path)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Release removes the lock file.
func (l *Lock) Release() error {
	if l == nil || l.path == "" {
		return nil
	}
	return os.Remove(l.path)
}

func isStaleLock(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var d lockData
	if json.Unmarshal(data, &d) != nil {
		// Unparseable: likely a lock being written right now (O_EXCL created,
		// content not yet flushed). Treat a recent file as held to avoid a
		// TOCTOU steal, but reclaim a stale one via mtime so a crash between
		// create and write cannot leave a permanent lock.
		info, statErr := os.Stat(path)
		if statErr != nil || time.Since(info.ModTime()) > staleAfter {
			return true
		}
		return false
	}
	if time.Since(time.Unix(d.Stamp, 0)) > staleAfter {
		return true
	}
	return !processAlive(d.PID)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
