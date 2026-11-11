//go:build windows

package instance

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const (
	lockfileExclusiveLock   = 0x2
	lockfileFailImmediately = 0x1
	errLockViolation        = syscall.Errno(33) // ERROR_LOCK_VIOLATION
	// The locked byte is far past the end of the file, so that the process id
	// written at the start can still be read by others (a locked range cannot).
	lockOffset = 0x7FFFFFF0
)

var errWouldBlock = errLockViolation

func overlapped() *syscall.Overlapped {
	return &syscall.Overlapped{Offset: lockOffset}
}

func lockFile(f *os.File) error {
	r, _, err := procLockFileEx.Call(f.Fd(), lockfileExclusiveLock|lockfileFailImmediately, 0, 1, 0, uintptr(unsafe.Pointer(overlapped())))
	if r == 0 {
		return err
	}
	return nil
}

func unlockFile(f *os.File) {
	_, _, _ = procUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(overlapped())))
}
