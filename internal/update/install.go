package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Kind is how the running kalka was installed, which decides whether it
// can replace itself (spec 24.3).
type Kind int

const (
	Unknown   Kind = iota // a CLI-only or packaged install, or no write access: notify only
	Installer             // Windows: the per-user installer's folder
	AppBundle             // macOS: inside Kalka.app
	Tarball               // Linux: the desktop archive's three programs, side by side
)

// Target is where an update goes: the app bundle, or the install folder.
type Target struct {
	Kind Kind
	Path string
}

// programs are what the Linux desktop archive holds beside each other,
// and what an install must have to be recognised. extras are installed
// when the archive has them and never required, so an install that a
// kalka without them updated is still recognised.
var (
	programs = []string{"kalka", "kalka-desktop", "kalka-plugin-gh"}
	extras   = []string{"kalka-plugin-jira"}
)

// Detect recognises the install the executable exe belongs to on goos.
// Anything it does not recognise, or cannot write to, is Unknown: kalka
// then only tells about the release and never writes.
func Detect(goos, exe string) Target {
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	dir := filepath.Dir(exe)
	switch goos {
	case "windows":
		if strings.EqualFold(filepath.Base(exe), "kalka-desktop.exe") && exists(filepath.Join(dir, "uninstall.exe")) {
			return Target{Installer, dir}
		}
	case "darwin":
		const tail = "/Kalka.app/Contents/MacOS"
		if p := filepath.ToSlash(dir); strings.HasSuffix(p, tail) {
			app := filepath.FromSlash(strings.TrimSuffix(p, "/Contents/MacOS"))
			if writable(filepath.Dir(app)) {
				return Target{AppBundle, app}
			}
		}
	case "linux":
		if filepath.Base(exe) != "kalka-desktop" {
			break
		}
		for _, p := range programs {
			if !exists(filepath.Join(dir, p)) {
				return Target{}
			}
		}
		if writable(dir) {
			return Target{Tarball, dir}
		}
	}
	return Target{}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// writable probes a folder with a temporary file.
func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".kalka-probe-*")
	if err != nil {
		return false
	}
	f.Close()
	return os.Remove(f.Name()) == nil
}

// installTarball replaces the three programs in dir from the desktop
// archive: each is written beside its target and renamed over it, which a
// running program survives on Linux. Nothing is renamed unless all three
// were written.
func installTarball(pkg, dir string) error {
	f, err := os.Open(pkg)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(zr)
	staged := map[string]string{}
	cleanup := func() {
		for _, t := range staged {
			_ = os.Remove(t)
		}
	}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			cleanup()
			return err
		}
		base := path.Base(h.Name)
		if h.Typeflag != tar.TypeReg || (!contains(programs, base) && !contains(extras, base)) {
			continue
		}
		tmp := filepath.Join(dir, "."+base+".new")
		staged[base] = tmp
		if err := writeFile(tmp, tr, 0o755); err != nil {
			cleanup()
			return err
		}
	}
	for _, p := range programs {
		if staged[p] == "" {
			cleanup()
			return fmt.Errorf("the archive has no %s", p)
		}
	}
	for _, p := range append(append([]string(nil), programs...), extras...) {
		if staged[p] == "" {
			continue
		}
		if err := os.Rename(staged[p], filepath.Join(dir, p)); err != nil {
			cleanup()
			return err
		}
	}
	return nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func writeFile(dst string, r io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// installApp swaps the Kalka.app in the zip in for the one at app: the
// new bundle is unpacked beside it, the old one renamed aside, the new one
// renamed in and the old one removed. A failed swap puts the old one back.
func installApp(pkg, app string) error {
	parent := filepath.Dir(app)
	fresh, old := filepath.Join(parent, ".Kalka.app.new"), filepath.Join(parent, ".Kalka.app.old")
	_ = os.RemoveAll(fresh)
	_ = os.RemoveAll(old)
	if err := unzipApp(pkg, fresh); err != nil {
		_ = os.RemoveAll(fresh)
		return err
	}
	if err := os.Rename(app, old); err != nil {
		_ = os.RemoveAll(fresh)
		return err
	}
	if err := os.Rename(fresh, app); err != nil {
		_ = os.Rename(old, app)
		_ = os.RemoveAll(fresh)
		return err
	}
	return os.RemoveAll(old)
}

// unzipApp unpacks the Kalka.app directory of the zip into dst, keeping
// modes and symbolic links.
func unzipApp(pkg, dst string) error {
	zr, err := zip.OpenReader(pkg)
	if err != nil {
		return err
	}
	defer zr.Close()
	const marker = "Kalka.app/"
	found := false
	for _, e := range zr.File {
		i := strings.Index(e.Name, marker)
		if i < 0 {
			continue
		}
		rel := path.Clean(e.Name[i+len(marker):])
		if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
			continue
		}
		found = true
		out := filepath.Join(dst, filepath.FromSlash(rel))
		mode := e.Mode()
		switch {
		case mode.IsDir():
			err = os.MkdirAll(out, 0o755)
		case mode&os.ModeSymlink != 0:
			err = symlink(e, out)
		default:
			if err = os.MkdirAll(filepath.Dir(out), 0o755); err == nil {
				err = copyEntry(e, out, mode.Perm()|0o600)
			}
		}
		if err != nil {
			return err
		}
	}
	if !found {
		return errors.New("the zip holds no Kalka.app")
	}
	return nil
}

func copyEntry(e *zip.File, dst string, mode os.FileMode) error {
	r, err := e.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	return writeFile(dst, r, mode)
}

func symlink(e *zip.File, dst string) error {
	r, err := e.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	target, err := io.ReadAll(io.LimitReader(r, 4096))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Symlink(string(target), dst)
}
