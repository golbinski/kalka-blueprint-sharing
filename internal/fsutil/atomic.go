// Package fsutil holds file-system helpers that must behave on Windows.
package fsutil

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// ErrChanged means the file changed between reading and writing.
var ErrChanged = errors.New("file changed while it was being updated")

// WriteAtomic replaces path with data through a temporary file in the same
// directory and a rename (spec 14). If expect is not nil, the current file
// must still hold exactly expect, otherwise ErrChanged is returned and
// nothing is written. On Windows the rename is retried briefly, because
// editors and antivirus scanners hold files open.
func WriteAtomic(path string, data, expect []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if info, err := os.Stat(path); err == nil {
		_ = os.Chmod(tmp.Name(), info.Mode().Perm())
	}
	if expect != nil {
		cur, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(cur, expect) {
			return ErrChanged
		}
	}
	return retry(func() error { return os.Rename(tmp.Name(), path) })
}

func retry(f func() error) error {
	err := f()
	if runtime.GOOS != "windows" {
		return err
	}
	for wait := 20 * time.Millisecond; err != nil && wait <= 640*time.Millisecond; wait *= 2 {
		time.Sleep(wait)
		err = f()
	}
	return err
}
