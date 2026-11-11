//go:build !windows

package instance

import (
	"errors"
	"os"
	"syscall"
)

var errWouldBlock = syscall.EWOULDBLOCK

func lockFile(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return err
	}
}

func unlockFile(f *os.File) { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
