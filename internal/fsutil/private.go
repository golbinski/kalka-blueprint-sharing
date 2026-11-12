package fsutil

import (
	"os"
	"path/filepath"
)

// WritePrivate writes a file only the current user can read: mode 0600,
// and on Windows a protected DACL granting access to the current user only
// (spec 5). The write is atomic.
func WritePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil && !isWindows {
		tmp.Close()
		return err
	}
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
	if err := restrictToUser(tmp.Name()); err != nil {
		return err
	}
	return retry(func() error { return os.Rename(tmp.Name(), path) })
}
