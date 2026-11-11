// Package instance keeps one trecs-notes of a kind running per user: a lock
// on a file in the person's data folder, held for as long as the process
// lives (and so released by the system if it crashes).
//
// The same lock, waited for instead of tried, also keeps two processes of the
// same person from syncing at once (see internal/remote).
package instance

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// ErrHeld means another process holds the lock.
var ErrHeld = errors.New("already running")

// Lock is a lock taken on a file. Unlock releases it (so does the end of
// the process).
type Lock struct{ f *os.File }

// TryLock takes the lock on path, creating the file, or returns ErrHeld when
// another process has it. The holder's process id is written into the file,
// for HolderPID.
func TryLock(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		if errors.Is(err, errWouldBlock) {
			return nil, ErrHeld
		}
		return nil, err
	}
	// the id is informational: a failure to write it is not a failure to lock
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return &Lock{f: f}, nil
}

// WaitLock is TryLock, trying again until the lock is free or d has passed
// (then ErrHeld).
func WaitLock(path string, d time.Duration) (*Lock, error) {
	deadline := time.Now().Add(d)
	for {
		l, err := TryLock(path)
		if !errors.Is(err, ErrHeld) || time.Now().After(deadline) {
			return l, err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Unlock releases the lock.
func (l *Lock) Unlock() {
	if l == nil || l.f == nil {
		return
	}
	unlockFile(l.f)
	_ = l.f.Close()
	l.f = nil
}

// HolderPID is the process id the holder of the lock on path wrote there, or
// 0 if it cannot be told.
func HolderPID(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// Describe says who holds the lock on path, for a message: " (process 4242)"
// or "".
func Describe(path string) string {
	if pid := HolderPID(path); pid > 0 {
		return fmt.Sprintf(" (process %d)", pid)
	}
	return ""
}
