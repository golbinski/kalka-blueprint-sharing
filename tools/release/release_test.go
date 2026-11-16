package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestSplitCommand(t *testing.T) {
	got, err := splitCommand(`signtool sign /f "C:\My Certs\k.pfx" '/d' kalka {file}`)
	want := []string{"signtool", "sign", "/f", `C:\My Certs\k.pfx`, "/d", "kalka", "{file}"}
	if err != nil || strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%q %v", got, err)
	}
	for _, bad := range []string{"", "  ", `a "b`} {
		if _, err := splitCommand(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestLoadSigning(t *testing.T) {
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	if s, err := loadSigning(get); err != nil || s != (signing{}) {
		t.Fatalf("empty: %+v %v", s, err)
	}
	f := filepath.Join(t.TempDir(), "signing.env")
	os.WriteFile(f, []byte("# signing\nKALKA_WIN_SIGN_CMD = \"osslsigncode sign {file}\"\nKALKA_MAC_IDENTITY=Developer ID Application: Example\n"), 0o600)
	env["KALKA_SIGNING"] = f
	env["KALKA_MAC_IDENTITY"] = "from env"
	s, err := loadSigning(get)
	if err != nil || s.winCmd != "osslsigncode sign {file}" || s.macIdentity != "from env" {
		t.Fatalf("file: %+v %v", s, err)
	}
	env["KALKA_MAC_NOTARY_PROFILE"] = "p"
	delete(env, "KALKA_MAC_IDENTITY")
	os.WriteFile(f, []byte("KALKA_WIN_SIGN_CMD=x\n"), 0o600)
	if _, err := loadSigning(get); err == nil {
		t.Fatal("notarizing without an identity accepted")
	}
	os.WriteFile(f, []byte("no equals sign\n"), 0o600)
	if _, err := loadSigning(get); err == nil {
		t.Fatal("bad line accepted")
	}
}

// TestSignWindows: unsigned says so and succeeds; a configured command
// runs on the file; a failing command fails the build.
func TestSignWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "kalka.exe")
	os.WriteFile(exe, []byte("x"), 0o644)
	var log bytes.Buffer
	if err := (signing{}).windows(exe, "kalka.exe", &log); err != nil || !strings.Contains(log.String(), "unsigned: kalka.exe") {
		t.Fatalf("unsigned: %v %q", err, log.String())
	}
	log.Reset()
	s := signing{winCmd: `sh -c 'cp "$0" "$0.signed"' {file}`}
	if err := s.windows(exe, "kalka.exe", &log); err != nil || !strings.Contains(log.String(), "signed: kalka.exe") {
		t.Fatalf("signed: %v %q", err, log.String())
	}
	if _, err := os.Stat(exe + ".signed"); err != nil {
		t.Fatal("the command did not run on the file")
	}
	if err := (signing{winCmd: "false"}).windows(exe, "kalka.exe", &log); err == nil {
		t.Fatal("a failing signing command passed")
	}
}

// TestPESubsystem builds a console and a GUI Windows program and reads
// their subsystems back.
func TestPESubsystem(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiles")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\ngo 1.21\n"), 0o644)
	for bin, flags := range map[string]string{"kalka.exe": "", "kalka-desktop.exe": "-H=windowsgui"} {
		cmd := exec.Command("go", "build", "-ldflags", flags, "-o", bin, ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0", "GOFLAGS=", "GOWORK=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", bin, err, out)
		}
	}
	if err := checkSubsystems(dir); err != nil {
		t.Fatal(err)
	}
	os.Rename(filepath.Join(dir, "kalka.exe"), filepath.Join(dir, "tmp"))
	os.Rename(filepath.Join(dir, "kalka-desktop.exe"), filepath.Join(dir, "kalka.exe"))
	os.Rename(filepath.Join(dir, "tmp"), filepath.Join(dir, "kalka-desktop.exe"))
	if err := checkSubsystems(dir); err == nil {
		t.Fatal("swapped subsystems passed")
	}
	if _, err := peSubsystem([]byte("not an exe at all, but long enough to have a header field......")); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestNSISScript(t *testing.T) {
	b, err := nsisScript(nsisVars{Version: "0.3.0", Arch: "amd64", AppID: "com.example.kalka", Src: "/s", Out: "/o/setup.exe", WebView2: "https://wv2.example"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`!define VERSION "0.3.0"`, `OutFile "/o/setup.exe"`, `File "/s/kalka-desktop.exe"`,
		"RequestExecutionLevel user", `Uninstall\com.example.kalka"`, `ExecShell "open" "https://wv2.example"`,
		// The kalka:// scheme, per user, and its removal (spec 23.1a).
		`WriteRegStr HKCU "Software\Classes\kalka" "URL Protocol" ""`,
		`WriteRegStr HKCU "Software\Classes\kalka\shell\open\command" "" '"$INSTDIR\kalka-desktop.exe" "%1"'`,
		`DeleteRegKey HKCU "Software\Classes\kalka"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(s, "[[") {
		t.Error("unexpanded template field")
	}
}

// TestNSISCompile compiles the installer script around placeholder files
// when makensis is installed.
func TestNSISCompile(t *testing.T) {
	mk, err := exec.LookPath("makensis")
	if err != nil {
		t.Skip("no makensis")
	}
	src, out := t.TempDir(), t.TempDir()
	for _, f := range []string{"kalka.exe", "kalka-desktop.exe", "kalka-plugin-gh.exe", "README.txt"} {
		os.WriteFile(filepath.Join(src, f), []byte(f), 0o644)
	}
	setup := filepath.Join(out, "kalka_0.3.0_windows_amd64_setup.exe")
	b, err := nsisScript(nsisVars{Version: "0.3.0", Arch: "amd64", AppID: "com.example.kalka", Src: src, Out: setup, WebView2: "https://wv2.example"})
	if err != nil {
		t.Fatal(err)
	}
	nsi := filepath.Join(out, "kalka.nsi")
	os.WriteFile(nsi, b, 0o644)
	if msg, err := exec.Command(mk, append(makensisFlags, nsi)...).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, msg)
	}
	if fi, err := os.Stat(setup); err != nil || fi.Size() == 0 {
		t.Fatalf("no installer: %v", err)
	}
}

func TestInfoPlist(t *testing.T) {
	p := filepath.Join(t.TempDir(), "Info.plist")
	os.WriteFile(p, infoPlist("0.3.0", "kalka"), 0o644)
	if _, err := exec.LookPath("plutil"); err != nil {
		t.Skip("no plutil")
	}
	if out, err := exec.Command("plutil", "-lint", p).CombinedOutput(); err != nil {
		t.Fatalf("%s", out)
	}
	out, _ := exec.Command("plutil", "-extract", "CFBundleExecutable", "raw", p).Output()
	if strings.TrimSpace(string(out)) != "kalka" {
		t.Fatalf("executable %q", out)
	}
	// The kalka:// scheme (spec 23.1a). This only reads the file: opening
	// a bundle is what registers its scheme, and no test does that.
	out, _ = exec.Command("plutil", "-extract", "CFBundleURLTypes.0.CFBundleURLSchemes.0", "raw", p).Output()
	if strings.TrimSpace(string(out)) != "kalka" {
		t.Fatalf("URL scheme %q", out)
	}
}

func TestInfoPlistText(t *testing.T) {
	s := string(infoPlist("0.3.0", ""))
	if !strings.Contains(s, "<key>CFBundleURLSchemes</key>") || !strings.Contains(s, "<string>kalka</string>\n\t\t\t</array>") {
		t.Fatal(s)
	}
}

func TestDesktopEntry(t *testing.T) {
	for _, want := range []string{"Exec=kalka-desktop %u\n", "MimeType=x-scheme-handler/kalka;\n", "Name=kalka\n"} {
		if !strings.Contains(desktopEntry, want) {
			t.Errorf("kalka.desktop lacks %q:\n%s", want, desktopEntry)
		}
	}
}

// TestReleaseArtifacts runs a release for one target with placeholder
// binaries: the headless archive and kalka-rendezvous's own archive are
// written, the server's holds nothing of the client, and SHA256SUMS
// covers every artifact (plan T8).
func TestReleaseArtifacts(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	if _, err := os.Stat(filepath.Join("internal", "ui", "dist", "index.html")); err != nil {
		t.Skip("the web UI is not built")
	}
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	out := t.TempDir()
	var built []string
	r := &release{version: "0.3.0", out: out, log: io.Discard,
		build: func(goos, goarch string, b build, dst string, _ bool) error {
			built = append(built, b.pkg+" "+b.version)
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			return os.WriteFile(dst, []byte(b.bin), 0o755)
		}}
	if err := r.run([]string{"linux/amd64"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(built, "./cmd/kalka-rendezvous main.version") {
		t.Fatalf("built %v", built)
	}
	sums, err := os.ReadFile(filepath.Join(out, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]string{}
	for _, l := range strings.Split(strings.TrimSpace(string(sums)), "\n") {
		sum, name, _ := strings.Cut(l, "  ")
		listed[name] = sum
	}
	entries, _ := os.ReadDir(out)
	var names []string
	for _, e := range entries {
		if e.Name() == "SHA256SUMS" {
			continue
		}
		names = append(names, e.Name())
		data, _ := os.ReadFile(filepath.Join(out, e.Name()))
		h := sha256.Sum256(data)
		if listed[e.Name()] != hex.EncodeToString(h[:]) {
			t.Errorf("SHA256SUMS does not cover %s", e.Name())
		}
	}
	if len(listed) != len(names) {
		t.Errorf("SHA256SUMS lists %d, the directory holds %d: %v", len(listed), len(names), names)
	}
	for _, want := range []string{"kalka_0.3.0_linux_amd64.tar.gz", "kalka-rendezvous_0.3.0_linux_amd64.tar.gz"} {
		if _, ok := listed[want]; !ok {
			t.Errorf("no %s in %v", want, names)
		}
	}
	got := tarNames(t, filepath.Join(out, "kalka-rendezvous_0.3.0_linux_amd64.tar.gz"))
	want := []string{"kalka-rendezvous_0.3.0_linux_amd64/README.txt", "kalka-rendezvous_0.3.0_linux_amd64/kalka-rendezvous"}
	if !slices.Equal(got, want) {
		t.Errorf("the server's archive holds %v", got)
	}
	if head := tarNames(t, filepath.Join(out, "kalka_0.3.0_linux_amd64.tar.gz")); slices.ContainsFunc(head, func(n string) bool { return strings.Contains(n, "rendezvous") }) {
		t.Errorf("the headless archive holds the server: %v", head)
	}
}

func tarNames(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
	}
	return names
}
