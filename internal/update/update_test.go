package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func v(t *testing.T, s string) Version {
	t.Helper()
	x, err := Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestParseAndCompare(t *testing.T) {
	for _, bad := range []string{"", "1.2", "1.2.x", "a.b.c", "1.2.3.4", "-1.0.0"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) succeeded", bad)
		}
	}
	if got := v(t, "v1.2.3-rc.1+b5").String(); got != "1.2.3-rc.1" {
		t.Errorf("String = %q", got)
	}
	order := []string{"0.1.0", "0.1.1", "0.2.0", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.10.0", "2.0.0"}
	for i := range order {
		for j := range order {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := v(t, order[i]).Compare(v(t, order[j])); got != want {
				t.Errorf("%s vs %s = %d, want %d", order[i], order[j], got, want)
			}
		}
	}
}

func TestCompatible(t *testing.T) {
	for _, c := range []struct {
		cur, cand string
		want      bool
	}{
		{"1.2.3", "1.9.0", true},
		{"1.2.3", "1.2.4", true},
		{"1.2.3", "2.0.0", false},
		{"0.1.1", "0.1.5", true},
		{"0.1.1", "0.2.0", false},
		{"0.1.1", "1.0.0", false},
		{"0.0.1", "0.0.2", true},
	} {
		if got := Compatible(v(t, c.cur), v(t, c.cand)); got != c.want {
			t.Errorf("%s -> %s: %v, want %v", c.cur, c.cand, got, c.want)
		}
	}
}

func rels(t *testing.T, vs ...string) []Release {
	var out []Release
	for _, s := range vs {
		out = append(out, Release{Version: v(t, s), Tag: "v" + s})
	}
	return out
}

func TestPick(t *testing.T) {
	for _, c := range []struct {
		name, cur string
		have      []string
		want      string
		compat    bool
	}{
		{"nothing newer", "0.1.1", []string{"0.1.1", "0.1.0"}, "", false},
		{"newest compatible", "0.1.1", []string{"0.1.2", "0.1.5", "0.2.0"}, "0.1.5", true},
		{"compatible beats a newer major", "1.2.0", []string{"2.0.0", "1.9.0"}, "1.9.0", true},
		{"only a new major", "1.2.0", []string{"2.0.0", "2.1.0"}, "2.1.0", false},
		{"pre-releases ignored", "0.1.1", []string{"0.1.2-rc1", "0.2.0-beta"}, "", false},
		{"pre-release build sees them", "0.1.2-rc1", []string{"0.1.2-rc2", "0.1.2"}, "0.1.2", true},
	} {
		got, compat, ok := Pick(rels(t, c.have...), v(t, c.cur))
		if (c.want == "") == ok || (ok && (got.Version.String() != c.want || compat != c.compat)) {
			t.Errorf("%s: got %v %v %v, want %q %v", c.name, got.Version, compat, ok, c.want, c.compat)
		}
	}
}

func TestAssetName(t *testing.T) {
	ver := v(t, "0.2.0")
	for goos, want := range map[string]string{
		"windows": "kalka_0.2.0_windows_arm64_setup.exe",
		"darwin":  "kalka_0.2.0_darwin_arm64_app.zip",
		"linux":   "kalka_0.2.0_linux_arm64_desktop.tar.gz",
	} {
		if got, ok := AssetName(ver, goos, "arm64"); !ok || got != want {
			t.Errorf("%s: %q", goos, got)
		}
	}
	if _, ok := AssetName(ver, "plan9", "amd64"); ok {
		t.Error("plan9 has a package")
	}
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// fake is a GitHub with one release, whose checksum for the package is
// sum, and a count of its calls.
func fake(t *testing.T, pkg []byte, sum string) (Source, Release, *int) {
	t.Helper()
	calls := new(int)
	mux := http.NewServeMux()
	mux.HandleFunc("/dl/pkg", func(w http.ResponseWriter, _ *http.Request) { *calls++; w.Write(pkg) })
	mux.HandleFunc("/dl/sums", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s  other.zip\n%s  pkg.bin\n", strings.Repeat("0", 64), sum)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	rel := Release{Tag: "v0.1.5", Assets: map[string]string{"pkg.bin": srv.URL + "/dl/pkg", SumsAsset: srv.URL + "/dl/sums"}}
	return Source{API: srv.URL, HTTP: srv.Client()}, rel, calls
}

func TestFetchVerifies(t *testing.T) {
	data := []byte("the package")
	src, rel, calls := fake(t, data, sha(data))
	dir := t.TempDir()
	p, err := src.Fetch(context.Background(), rel, "pkg.bin", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, data) {
		t.Fatalf("file holds %q", got)
	}
	// A second fetch keeps the verified file.
	if _, err := src.Fetch(context.Background(), rel, "pkg.bin", dir); err != nil || *calls != 1 {
		t.Fatalf("second fetch: %v, %d downloads", err, *calls)
	}
}

func TestFetchRefusesBadSum(t *testing.T) {
	src, rel, _ := fake(t, []byte("tampered"), sha([]byte("the package")))
	dir := t.TempDir()
	if _, err := src.Fetch(context.Background(), rel, "pkg.bin", dir); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v", err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("left %d files behind", len(ents))
	}
	// Without SHA256SUMS, or without a line for the asset, nothing is installed.
	delete(rel.Assets, SumsAsset)
	if _, err := src.Fetch(context.Background(), rel, "pkg.bin", dir); err == nil {
		t.Error("fetched without SHA256SUMS")
	}
	if _, err := src.Fetch(context.Background(), rel, "missing.bin", dir); err == nil {
		t.Error("fetched an asset the release lacks")
	}
}

func TestFetchReplacesCorruptKept(t *testing.T) {
	data := []byte("the package")
	src, rel, calls := fake(t, data, sha(data))
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pkg.bin"), []byte("half"), 0o600)
	p, err := src.Fetch(context.Background(), rel, "pkg.bin", dir)
	if got, _ := os.ReadFile(p); err != nil || !bytes.Equal(got, data) || *calls != 1 {
		t.Fatalf("%v %q %d", err, got, *calls)
	}
}

func TestReleasesAndETag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/releases" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("If-None-Match") == `"e1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"e1"`)
		fmt.Fprint(w, `[{"tag_name":"v0.1.5","html_url":"https://example.test/n","assets":[{"name":"SHA256SUMS","browser_download_url":"https://example.test/s"}]},
			{"tag_name":"v9.9.9","draft":true},{"tag_name":"nightly"}]`)
	}))
	defer srv.Close()
	s := Source{API: srv.URL, Repo: "o/r"}
	got, etag, nm, err := s.Releases(context.Background(), "")
	if err != nil || nm || etag != `"e1"` || len(got) != 1 || got[0].Version.String() != "0.1.5" ||
		got[0].NotesURL != "https://example.test/n" || got[0].Assets[SumsAsset] == "" {
		t.Fatalf("%+v %q %v %v", got, etag, nm, err)
	}
	if _, _, nm, err := s.Releases(context.Background(), etag); err != nil || !nm {
		t.Fatalf("not modified: %v %v", nm, err)
	}
}

func TestReleasesRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer srv.Close()
	if _, _, _, err := (Source{API: srv.URL}).Releases(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("err = %v", err)
	}
}

func TestDetect(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir()) // macOS temp folders are symlinks
	touch := func(p ...string) string {
		f := filepath.Join(append([]string{dir}, p...)...)
		os.MkdirAll(filepath.Dir(f), 0o755)
		os.WriteFile(f, nil, 0o755)
		return f
	}
	exe := touch("lin", "kalka-desktop")
	if got := Detect("linux", exe); got.Kind != Unknown {
		t.Errorf("incomplete tarball: %v", got)
	}
	touch("lin", "kalka")
	touch("lin", "kalka-plugin-gh")
	if got := Detect("linux", exe); got.Kind != Tarball || got.Path != filepath.Join(dir, "lin") {
		t.Errorf("tarball: %v", got)
	}
	if got := Detect("linux", touch("lin", "kalka")); got.Kind != Unknown {
		t.Errorf("the CLI alone: %v", got)
	}
	win := touch("win", "kalka-desktop.exe")
	if got := Detect("windows", win); got.Kind != Unknown {
		t.Errorf("a zip unpacked by hand: %v", got)
	}
	touch("win", "uninstall.exe")
	if got := Detect("windows", win); got.Kind != Installer {
		t.Errorf("installer: %v", got)
	}
	app := touch("apps", "Kalka.app", "Contents", "MacOS", "kalka")
	if got := Detect("darwin", app); got.Kind != AppBundle || got.Path != filepath.Join(dir, "apps", "Kalka.app") {
		t.Errorf("bundle: %v", got)
	}
	if got := Detect("darwin", touch("elsewhere", "kalka")); got.Kind != Unknown {
		t.Errorf("a bare binary on a Mac: %v", got)
	}
	if os.Getuid() != 0 {
		ro := filepath.Join(dir, "ro")
		os.MkdirAll(filepath.Join(ro, "Kalka.app", "Contents", "MacOS"), 0o755)
		os.WriteFile(filepath.Join(ro, "Kalka.app", "Contents", "MacOS", "kalka"), nil, 0o755)
		os.Chmod(ro, 0o555)
		defer os.Chmod(ro, 0o755)
		if got := Detect("darwin", filepath.Join(ro, "Kalka.app", "Contents", "MacOS", "kalka")); got.Kind != Unknown {
			t.Errorf("a folder that cannot be written: %v", got)
		}
	}
}

func tarball(t *testing.T, files map[string]string) string {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for n, c := range files {
		tw.WriteHeader(&tar.Header{Name: "kalka_0.1.5_linux_amd64_desktop/" + n, Mode: 0o755, Size: int64(len(c)), Typeflag: tar.TypeReg})
		tw.Write([]byte(c))
	}
	tw.Close()
	zw.Close()
	p := filepath.Join(t.TempDir(), "pkg.tar.gz")
	os.WriteFile(p, buf.Bytes(), 0o600)
	return p
}

func TestInstallTarball(t *testing.T) {
	dir := t.TempDir()
	for _, p := range programs {
		os.WriteFile(filepath.Join(dir, p), []byte("old "+p), 0o755)
	}
	// An archive missing a program changes nothing.
	if err := installTarball(tarball(t, map[string]string{"kalka": "n", "kalka-desktop": "n"}), dir); err == nil {
		t.Fatal("installed an incomplete archive")
	}
	for _, p := range programs {
		if b, _ := os.ReadFile(filepath.Join(dir, p)); string(b) != "old "+p {
			t.Fatalf("%s changed: %q", p, b)
		}
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 3 {
		t.Errorf("temporary files left: %v", ents)
	}
	files := map[string]string{"README.txt": "r"}
	for _, p := range programs {
		files[p] = "new " + p
	}
	if err := installTarball(tarball(t, files), dir); err != nil {
		t.Fatal(err)
	}
	for _, p := range programs {
		b, _ := os.ReadFile(filepath.Join(dir, p))
		fi, _ := os.Stat(filepath.Join(dir, p))
		if string(b) != "new "+p || fi.Mode().Perm() != 0o755 {
			t.Errorf("%s = %q %v", p, b, fi.Mode())
		}
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 3 {
		t.Errorf("extra files: %v", ents)
	}
}

func appZip(t *testing.T, entries map[string]string) string {
	p := filepath.Join(t.TempDir(), "app.zip")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	for n, c := range entries {
		h := &zip.FileHeader{Name: n}
		h.SetMode(0o755)
		if strings.HasSuffix(n, "/") {
			h.SetMode(os.ModeDir | 0o755)
		}
		if strings.HasSuffix(n, "Current") {
			h.SetMode(os.ModeSymlink | 0o755)
		}
		w, _ := zw.CreateHeader(h)
		w.Write([]byte(c))
	}
	zw.Close()
	f.Close()
	return p
}

func TestInstallApp(t *testing.T) {
	parent := t.TempDir()
	app := filepath.Join(parent, "Kalka.app")
	os.MkdirAll(filepath.Join(app, "Contents"), 0o755)
	os.WriteFile(filepath.Join(app, "Contents", "old"), []byte("x"), 0o644)
	pre := "kalka_0.1.5_darwin_arm64_app/Kalka.app/"
	zipPath := appZip(t, map[string]string{
		"kalka_0.1.5_darwin_arm64_app/README.txt": "r",
		pre:                          "",
		pre + "Contents/":            "",
		pre + "Contents/MacOS/kalka": "binary",
		pre + "Contents/Current":     "MacOS",
		pre + "../evil":              "no",
	})
	if err := installApp(zipPath, app); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(app, "Contents", "MacOS", "kalka")); string(b) != "binary" {
		t.Errorf("binary = %q", b)
	}
	if l, err := os.Readlink(filepath.Join(app, "Contents", "Current")); err != nil || l != "MacOS" {
		t.Errorf("symlink = %q %v", l, err)
	}
	if exists(filepath.Join(app, "Contents", "old")) {
		t.Error("the old bundle's files remain")
	}
	if ents, _ := os.ReadDir(parent); len(ents) != 1 {
		t.Errorf("left beside the app: %v", ents)
	}
	// A zip without the app leaves the installed one alone.
	if err := installApp(appZip(t, map[string]string{"x/README.txt": "r"}), app); err == nil || !exists(filepath.Join(app, "Contents", "MacOS", "kalka")) {
		t.Fatalf("bad zip: %v", err)
	}
}
