package instance

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOnlyOneHoldsTheLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	a, err := TryLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TryLock(path); !errors.Is(err, ErrHeld) {
		t.Fatalf("a second one must be refused: %v", err)
	}
	if HolderPID(path) != os.Getpid() {
		t.Errorf("holder %d", HolderPID(path))
	}
	if got := Describe(path); got == "" {
		t.Errorf("no description")
	}
	a.Unlock()
	a.Unlock() // harmless
	b, err := TryLock(path)
	if err != nil {
		t.Fatalf("free again after Unlock: %v", err)
	}
	b.Unlock()
}

func TestWaitLockGetsItWhenItIsReleased(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	a, _ := TryLock(path)
	go func() { time.Sleep(150 * time.Millisecond); a.Unlock() }()
	start := time.Now()
	b, err := WaitLock(path, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	b.Unlock()
	if time.Since(start) < 100*time.Millisecond {
		t.Errorf("did not wait")
	}

	c, _ := TryLock(path)
	defer c.Unlock()
	if _, err := WaitLock(path, 120*time.Millisecond); !errors.Is(err, ErrHeld) {
		t.Errorf("gives up: %v", err)
	}
}

func TestHolderPIDOfNothing(t *testing.T) {
	if HolderPID(filepath.Join(t.TempDir(), "none")) != 0 || Describe(filepath.Join(t.TempDir(), "none")) != "" {
		t.Error("unknown holder")
	}
}
