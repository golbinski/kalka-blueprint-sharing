// Command release builds the release archives (spec 15): for each target,
// kalka and kalka-plugin-gh (and kalkaw.exe, the console-less build that
// starts at logon, on Windows) in a .zip or .tar.gz, plus SHA256SUMS.
//
// The archives are reproducible: -trimpath, no build id or VCS stamp, and
// every archive entry gets the commit's time, fixed owners and modes, in
// sorted order. Two runs on one commit with one Go version give the same
// bytes.
//
//	go run ./tools/release -version 0.2.0 [-out dist] [-targets windows/amd64,...]
//
// The web UI must be built first (make web): it is embedded in kalka.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
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
	flag.Parse()
	if err := run(*version, *out, strings.Split(*targets, ",")); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

var versionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

type file struct {
	name string
	data []byte
	mode int64
}

func run(version, out string, targets []string) error {
	if !versionRe.MatchString(version) {
		return fmt.Errorf("-version %q: want semver like 0.2.0", version)
	}
	if _, err := os.Stat(filepath.Join("internal", "ui", "dist", "index.html")); err != nil {
		return errors.New("the web UI is not built: run `make web` first")
	}
	mtime, err := sourceTime()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "kalka-release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	var sums []string
	for _, t := range targets {
		goos, goarch, ok := strings.Cut(strings.TrimSpace(t), "/")
		if !ok {
			return fmt.Errorf("target %q: want GOOS/GOARCH", t)
		}
		exe := ""
		if goos == "windows" {
			exe = ".exe"
		}
		builds := []struct{ pkg, bin, ldflags string }{
			{"./cmd/kalka", "kalka" + exe, ""},
			{"./cmd/kalka-plugin-gh", "kalka-plugin-gh" + exe, ""},
		}
		if goos == "windows" {
			builds = append(builds, struct{ pkg, bin, ldflags string }{"./cmd/kalka", "kalkaw.exe", " -H=windowsgui"})
		}
		base := fmt.Sprintf("kalka_%s_%s_%s", version, goos, goarch)
		var files []file
		for _, b := range builds {
			dst := filepath.Join(tmp, base, b.bin)
			cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false",
				"-ldflags", "-s -w -buildid= -X main.version="+version+b.ldflags, "-o", dst, b.pkg)
			cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0", "GOFLAGS=")
			cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
			fmt.Fprintf(os.Stderr, "build %s/%s %s\n", goos, goarch, b.bin)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("%s for %s: %w", b.bin, t, err)
			}
			data, err := os.ReadFile(dst)
			if err != nil {
				return err
			}
			files = append(files, file{base + "/" + b.bin, data, 0o755})
		}
		files = append(files, file{base + "/README.txt", []byte(readme(version, goos)), 0o644})
		sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })

		var archive bytes.Buffer
		name := base + ".tar.gz"
		if goos == "windows" {
			name = base + ".zip"
			err = writeZip(&archive, files, mtime)
		} else {
			err = writeTarGz(&archive, files, mtime)
		}
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(out, name), archive.Bytes(), 0o644); err != nil {
			return err
		}
		sum := sha256.Sum256(archive.Bytes())
		sums = append(sums, hex.EncodeToString(sum[:])+"  "+name)
		fmt.Fprintf(os.Stderr, "wrote %s\n", filepath.Join(out, name))
	}
	sort.Strings(sums)
	return os.WriteFile(filepath.Join(out, "SHA256SUMS"), []byte(strings.Join(sums, "\n")+"\n"), 0o644)
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

func writeTarGz(w io.Writer, files []file, mtime time.Time) error {
	gz, _ := gzip.NewWriterLevel(w, gzip.BestCompression)
	gz.ModTime = time.Time{} // no timestamp or name in the gzip header
	tw := tar.NewWriter(gz)
	for _, f := range files {
		h := &tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.data)), ModTime: mtime, Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if _, err := tw.Write(f.data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func writeZip(w io.Writer, files []file, mtime time.Time) error {
	zw := zip.NewWriter(w)
	for _, f := range files {
		h := &zip.FileHeader{Name: f.name, Method: zip.Deflate, Modified: mtime}
		h.SetMode(os.FileMode(f.mode))
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := fw.Write(f.data); err != nil {
			return err
		}
	}
	return zw.Close()
}

func readme(version, goos string) string {
	s := "kalka " + version + "\n\nPeer-to-peer review of blueprints. Full guide: docs/install.md in the source.\n\n"
	switch goos {
	case "windows":
		s += `Install
  1. Unzip to a folder of your own, e.g. %LOCALAPPDATA%\Programs\kalka.
     Keep kalka.exe, kalkaw.exe and kalka-plugin-gh.exe together.
  2. Run kalka.exe once: the browser opens the setup form.
  3. In a terminal in that folder: kalka autostart on
     kalka then starts at each logon with no window (kalkaw.exe), and a
     teammate's sync shows as a Windows notification.

The binaries are not code-signed: SmartScreen may warn on first start
("More info" > "Run anyway").
Uninstall: kalka autostart off, then delete the folder and %APPDATA%\kalka.
`
	case "darwin":
		s += `Install
  1. Copy kalka and kalka-plugin-gh to a folder on your PATH, e.g. ~/bin.
  2. The binaries are not notarized. If macOS refuses to run them:
       xattr -d com.apple.quarantine ~/bin/kalka ~/bin/kalka-plugin-gh
  3. Run kalka once: the browser opens the setup form.
  4. kalka autostart on    (a LaunchAgent; kalka starts at each login)

Uninstall: kalka autostart off, then delete the binaries and
~/.config/kalka.
`
	default:
		s += `Install
  1. Copy kalka and kalka-plugin-gh to a folder on your PATH, e.g. ~/.local/bin.
  2. Run kalka once: the browser opens the setup form.
  3. kalka autostart on    (an entry in ~/.config/autostart)

Popups need zenity. Uninstall: kalka autostart off, then delete the
binaries and ~/.config/kalka.
`
	}
	return s
}
