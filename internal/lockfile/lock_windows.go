//go:build windows

package lockfile

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/windows"
)

var errWouldBlock = windows.ERROR_LOCK_VIOLATION

// The lock covers one byte far past the content, so the PID stays
// readable by others while the lock is held (a locked range cannot be
// read through another handle on Windows).
const lockOffset = 1 << 30

func overlapped() *windows.Overlapped {
	return &windows.Overlapped{Offset: lockOffset}
}

func lock(f *os.File) error {
	err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped())
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return errWouldBlock
	}
	return err
}

func unlock(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, overlapped())
}

func readShared(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 64))
}
