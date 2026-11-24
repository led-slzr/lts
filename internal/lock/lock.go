// Package lock provides per-repo advisory locks shared by every LTS process
// (TUI and CLI), so two processes never mutate the same repo at once.
// Locks are flock(2) on <workspace>/.lts-locks/<repo>.lock: the kernel
// releases them when the holder exits, so a crash never leaves a stale lock.
package lock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Dir is the lock directory inside a workspace.
func Dir(workDir string) string { return filepath.Join(workDir, ".lts-locks") }

func path(workDir, repo string) string {
	return filepath.Join(Dir(workDir), sanitize(repo)+".lock")
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '_'
		}
		return r
	}, s)
}

// Held is a set of acquired locks.
type Held struct{ files []*os.File }

// Release frees every lock (idempotent, nil-safe).
func (h *Held) Release() {
	if h == nil {
		return
	}
	for _, f := range h.files {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
	h.files = nil
}

// BusyError reports a lock held by another process.
type BusyError struct {
	Repo  string
	Owner string // "pid 123: lts create feat/x" (best effort)
}

func (e *BusyError) Error() string {
	if e.Owner != "" {
		return fmt.Sprintf("%s is busy (%s)", e.Repo, e.Owner)
	}
	return e.Repo + " is busy (another lts process)"
}

// TryAcquire locks every repo or none; it never blocks. owner is recorded
// in the lock file for diagnostics.
func TryAcquire(workDir string, repos []string, owner string) (*Held, error) {
	if err := os.MkdirAll(Dir(workDir), 0755); err != nil {
		return nil, err
	}
	names := uniqueSorted(repos) // fixed order: no lock-order deadlocks
	h := &Held{}
	for _, r := range names {
		f, err := os.OpenFile(path(workDir, r), os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			h.Release()
			return nil, err
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			f.Close()
			h.Release()
			if errors.Is(err, syscall.EWOULDBLOCK) {
				return nil, &BusyError{Repo: r, Owner: readOwner(workDir, r)}
			}
			return nil, err
		}
		f.Truncate(0)
		f.WriteAt([]byte(fmt.Sprintf("pid %d: %s\n", os.Getpid(), owner)), 0)
		h.files = append(h.files, f)
	}
	return h, nil
}

// Acquire is TryAcquire that retries until timeout (0 = try once).
func Acquire(workDir string, repos []string, owner string, timeout time.Duration, onWait func(*BusyError)) (*Held, error) {
	deadline := time.Now().Add(timeout)
	notified := false
	for {
		h, err := TryAcquire(workDir, repos, owner)
		var be *BusyError
		if err == nil || !errors.As(err, &be) || time.Now().After(deadline) {
			return h, err
		}
		if !notified && onWait != nil {
			onWait(be)
			notified = true
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// Holder reports whether repo is locked by an open file description other
// than the caller's own, and the recorded owner. Note: flock conflicts
// between descriptors even within one process, so callers must exclude
// locks they hold themselves.
func Holder(workDir, repo string) (locked bool, owner string) {
	f, err := os.OpenFile(path(workDir, repo), os.O_RDWR, 0644)
	if err != nil {
		return false, ""
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return true, readOwner(workDir, repo)
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false, ""
}

func readOwner(workDir, repo string) string {
	data, err := os.ReadFile(path(workDir, repo))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
