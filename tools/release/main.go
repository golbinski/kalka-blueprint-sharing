// Command release builds the release artifacts (spec 15, 23) into one
// directory, with SHA256SUMS:
//
//   - every target: kalka and kalka-plugin-gh in a .tar.gz (.zip on
//     Windows), the headless archive of spec 15. On Windows it also holds
//     kalka-desktop.exe, the shell, which with arguments is the console-less
//     CLI (pure Go, so it builds on any host);
//   - Windows: a per-user NSIS installer, when makensis is on the PATH;
//   - macOS: Kalka.app in a zip, built on a Mac (cgo);
//   - Linux: a desktop archive (kalka, kalka-desktop, a .desktop file and an
//     icon), built on Linux with GTK 4 and WebKitGTK 6.0 installed.
//
// Desktop artifacts the host cannot build are skipped with a line saying
// so. -purego builds them without cgo (Wails' experimental back ends), so
// one host can build them all.
//
// Signing is configuration (spec 23.5): see signing.go. With none, every
// artifact is reported "unsigned" and the build succeeds.
//
// The archives are reproducible: -trimpath, no build id or VCS stamp, and
// every archive entry gets the commit's time, fixed owners and modes, in
// sorted order. Two runs on one commit with one Go version give the same
// bytes (signatures and the installer aside).
//
//	go run ./tools/release -version 0.2.0 [-out dist] [-targets windows/amd64,...] [-purego]
//
// The web UI must be built first (make web): it is embedded in kalka.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

var defaultTargets = "windows/amd64,windows/arm64,darwin/arm64,darwin/amd64,linux/amd64,linux/arm64"

func main() {
	version := flag.String("version", "", "release version, e.g. 0.2.0 (required)")
	out := flag.String("out", "dist", "output directory")
	targets := flag.String("targets", defaultTargets, "comma-separated GOOS/GOARCH list")
	purego := flag.Bool("purego", false, "build the macOS and Linux desktop apps without cgo (experimental)")
	flag.Parse()
	sign, err := loadSigning(os.Getenv)
	if err == nil {
		r := &release{version: *version, out: *out, purego: *purego, sign: sign, log: os.Stderr}
		err = r.run(strings.Split(*targets, ","))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

var versionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

// MacMinimum is the oldest macOS the app supports.
const MacMinimum = "11.0"

type file struct {
	name string
	data []byte
	mode int64
}

type release struct {
	version, out string
	purego       bool
	sign         signing
	log          io.Writer

	tmp   string
	mtime time.Time
	sums  []string
}

func (r *release) run(targets []string) error {
	if !versionRe.MatchString(r.version) {
		return fmt.Errorf("-version %q: want semver like 0.2.0", r.version)
	}
	if _, err := os.Stat(filepath.Join("internal", "ui", "dist", "index.html")); err != nil {
		return errors.New("the web UI is not built: run `make web` first")
	}
	var err error
	if r.mtime, err = sourceTime(); err != nil {
		return err
	}
	if err := os.MkdirAll(r.out, 0o755); err != nil {
		return err
	}
	if r.tmp, err = os.MkdirTemp("", "kalka-release-"); err != nil {
		return err
	}
	defer os.RemoveAll(r.tmp)
	for _, t := range targets {
		goos, goarch, ok := strings.Cut(strings.TrimSpace(t), "/")
		if !ok {
			return fmt.Errorf("target %q: want GOOS/GOARCH", t)
		}
		if err := r.target(goos, goarch); err != nil {
			return fmt.Errorf("%s: %w", t, err)
		}
	}
	sort.Strings(r.sums)
	return os.WriteFile(filepath.Join(r.out, "SHA256SUMS"), []byte(strings.Join(r.sums, "\n")+"\n"), 0o644)
}

func (r *release) target(goos, goarch string) error {
	exe := ""
	if goos == "windows" {
		exe = ".exe"
	}
	base := fmt.Sprintf("kalka_%s_%s_%s", r.version, goos, goarch)
	stage := filepath.Join(r.tmp, base)
	bins := []build{
		{pkg: "./cmd/kalka", bin: "kalka" + exe},
		{pkg: "./cmd/kalka-plugin-gh", bin: "kalka-plugin-gh" + exe},
	}
	if goos == "windows" {
		bins = append(bins, build{pkg: "./cmd/kalka-desktop", bin: "kalka-desktop.exe", tags: "desktop,production", ldflags: " -H=windowsgui"})
	}
	for _, b := range bins {
		if err := r.gobuild(goos, goarch, b, filepath.Join(stage, b.bin)); err != nil {
			return err
		}
	}
	if goos == "windows" {
		if err := checkSubsystems(stage); err != nil {
			return err
		}
		for _, b := range bins {
			if err := r.sign.windows(filepath.Join(stage, b.bin), base+"/"+b.bin, r.log); err != nil {
				return err
			}
		}
	}
	if err := os.WriteFile(filepath.Join(stage, "README.txt"), []byte(readme(r.version, goos)), 0o644); err != nil {
		return err
	}
	files, err := collect(stage, base)
	if err != nil {
		return err
	}
	name := base + ".tar.gz"
	if goos == "windows" {
		name = base + ".zip"
	}
	if err := r.archive(name, files); err != nil {
		return err
	}
	switch goos {
	case "windows":
		return r.installer(stage, goarch)
	case "darwin":
		return r.macApp(stage, goarch)
	case "linux":
		return r.linuxDesktop(stage, goarch)
	}
	return nil
}

type build struct{ pkg, bin, tags, ldflags string }

// gobuild compiles one binary; cgo is off unless the build needs it.
func (r *release) gobuild(goos, goarch string, b build, dst string) error {
	return r.gobuildCgo(goos, goarch, b, dst, false)
}

func (r *release) gobuildCgo(goos, goarch string, b build, dst string, cgo bool) error {
	args := []string{"build", "-trimpath", "-buildvcs=false",
		"-ldflags", "-s -w -buildid= -X github.com/golbinski/kalka/internal/cli.version=" + r.version + b.ldflags, "-o", dst}
	if b.tags != "" {
		args = append(args, "-tags", b.tags)
	}
	cmd := exec.Command("go", append(args, b.pkg)...)
	cgoFlag := "0"
	if cgo {
		cgoFlag = "1"
	}
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED="+cgoFlag, "GOFLAGS=")
	if goos == "darwin" {
		cmd.Env = append(cmd.Env, "MACOSX_DEPLOYMENT_TARGET="+MacMinimum)
	}
	cmd.Stdout, cmd.Stderr = r.log, r.log
	fmt.Fprintf(r.log, "build %s/%s %s\n", goos, goarch, b.bin)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", b.bin, err)
	}
	return nil
}

// desktopBuild is how the shell is built for goos/goarch on this host, or
// why it cannot be.
func (r *release) desktopBuild(goos, goarch string) (tags string, cgo bool, why string) {
	if r.purego {
		return "desktop,production,purego", false, ""
	}
	switch goos {
	case "darwin":
		if runtime.GOOS != "darwin" {
			return "", false, "needs a Mac (cgo), or -purego"
		}
	case "linux":
		if runtime.GOOS != "linux" || runtime.GOARCH != goarch {
			return "", false, "needs Linux on " + goarch + " (cgo), or -purego"
		}
		if err := exec.Command("pkg-config", "--exists", "gtk4", "webkitgtk-6.0").Run(); err != nil {
			return "", false, "needs the GTK 4 and WebKitGTK 6.0 development packages (libgtk-4-dev libwebkitgtk-6.0-dev), or -purego"
		}
	}
	return "desktop,production", true, ""
}

// archive writes a zip or tar.gz of files into the output and sums it.
func (r *release) archive(name string, files []file) error {
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	f, err := os.Create(filepath.Join(r.out, name))
	if err != nil {
		return err
	}
	h := sha256.New()
	w := io.MultiWriter(f, h)
	if strings.HasSuffix(name, ".zip") {
		err = writeZip(w, files, r.mtime)
	} else {
		err = writeTarGz(w, files, r.mtime)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	r.sums = append(r.sums, hex.EncodeToString(h.Sum(nil))+"  "+name)
	fmt.Fprintf(r.log, "wrote %s\n", filepath.Join(r.out, name))
	return nil
}

// addFile copies a finished artifact (the installer) into the output.
func (r *release) addFile(src, name string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.out, name), data, 0o644); err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	r.sums = append(r.sums, hex.EncodeToString(sum[:])+"  "+name)
	fmt.Fprintf(r.log, "wrote %s\n", filepath.Join(r.out, name))
	return nil
}

// collect reads the files under dir as archive entries under prefix:
// executables 0755, the rest 0644.
func collect(dir, prefix string) ([]file, error) {
	var files []file
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		mode := int64(0o644)
		if info.Mode()&0o111 != 0 || strings.HasSuffix(p, ".exe") {
			mode = 0o755
		}
		files = append(files, file{prefix + "/" + filepath.ToSlash(rel), data, mode})
		return nil
	})
	return files, err
}

// sourceTime is SOURCE_DATE_EPOCH, else the last commit's time.
func sourceTime() (time.Time, error) {
	s := os.Getenv("SOURCE_DATE_EPOCH")
	if s == "" {
		b, err := exec.Command("git", "log", "-1", "--format=%ct").Output()
		if err != nil {
			return time.Time{}, errors.New("set SOURCE_DATE_EPOCH (no git commit to take the time from)")
		}
		s = strings.TrimSpace(string(b))
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("SOURCE_DATE_EPOCH %q: %w", s, err)
	}
	return time.Unix(n, 0).UTC(), nil
}
