// Package samples holds the sample blueprints, built into kalka so that
// `kalka samples`, the app and the Windows installer can copy them into
// Personal to try kalka with (spec 12.1).
package samples

import (
	"embed"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/golbinski/kalka/internal/store"
)

//go:embed blueprints
var files embed.FS

// Dir is the folder under Personal the samples are copied to.
const Dir = "samples"

// PendingFile, in the configuration folder, asks for the samples to be
// added when kalka is set up: the installer runs before setup.
const PendingFile = "add-samples"

// Seed copies the samples into <folder>/personal/samples, keeping their
// folders. A file that exists is left as it is, so seeding twice changes
// nothing. It returns the paths written, relative to the folder.
func Seed(folder string) ([]string, error) {
	if folder == "" {
		return nil, errors.New("no folder is set")
	}
	var written []string
	err := fs.WalkDir(files, "blueprints", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".md" {
			return err
		}
		rel := path.Join(store.PersonalDir, Dir, p[len("blueprints/"):])
		dst := filepath.Join(folder, filepath.FromSlash(rel))
		if _, err := os.Stat(dst); err == nil {
			return nil
		}
		b, err := files.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := f.Write(b); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		written = append(written, rel)
		return nil
	})
	return written, err
}

// Names are the samples' paths under personal/samples.
func Names() []string {
	var out []string
	_ = fs.WalkDir(files, "blueprints", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && path.Ext(p) == ".md" {
			out = append(out, p[len("blueprints/"):])
		}
		return nil
	})
	return out
}

// Ask records in the configuration folder that the samples are to be
// added once kalka is set up.
func Ask(configDir string) error {
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configDir, PendingFile), nil, 0o600)
}

// SeedIfAsked adds the samples when Ask was called, and forgets the
// request once they are in place.
func SeedIfAsked(configDir, folder string) ([]string, error) {
	p := filepath.Join(configDir, PendingFile)
	if _, err := os.Stat(p); err != nil {
		return nil, nil
	}
	written, err := Seed(folder)
	if err != nil {
		return written, err
	}
	return written, os.Remove(p)
}
