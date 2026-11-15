package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		"RequestExecutionLevel user", `Uninstall\com.example.kalka"`, `ExecShell "open" "https://wv2.example"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(s, "[[") {
		t.Error("unexpanded template field")
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
}
