// Package lockfile is the single-daemon lock (spec 4.1): an exclusive,
// non-blocking lock on a file that the kernel releases when the process
// ends, however it ends. flock on macOS and Linux, LockFileEx on Windows.
// The PID written into the file is for messages only.
package lockfile

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Held is returned when another process (or another Lock in this one)
// holds the lock. PID is the holder's, as it wrote it; 0 if unknown.
type Held struct{ PID int }

func (h *Held) Error() string {
	if h.PID > 0 {
		return fmt.Sprintf("already running (pid %d)", h.PID)
	}
	return "already running"
}

// ErrHeld matches any *Held with errors.Is.
var ErrHeld = errors.New("lock held")

func (h *Held) Is(target error) bool { return target == ErrHeld }

// Lock is a held lock.
type Lock struct{ f *os.File }

// Acquire takes the lock on path without waiting, creating the file with
// owner-only permissions. It returns a *Held error when the lock is taken.
func Acquire(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lock(f); err != nil {
		f.Close()
		if errors.Is(err, errWouldBlock) {
			return nil, &Held{PID: ReadPID(path)}
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	// Record our PID. The lock covers the first byte range only on
	// Windows (see lock_windows.go), so the content stays writable.
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	_ = f.Sync()
	return &Lock{f: f}, nil
}

// Release gives the lock up. The file stays; its PID is cleared.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = l.f.Truncate(0)
	err := unlock(l.f)
	if cerr := l.f.Close(); err == nil {
		err = cerr
	}
	l.f = nil
	return err
}

// ReadPID reads the PID a holder wrote; 0 if none.
func ReadPID(path string) int {
	raw, err := readShared(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	return n
}
