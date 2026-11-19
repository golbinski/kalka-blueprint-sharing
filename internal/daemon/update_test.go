package daemon

import (
	"archive/tar"
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/update"
)

// githubWith serves the release list of a fake GitHub: 0.1.5 (compatible
// with 0.1.1, with a Linux package) and 0.2.0 (not). hits counts requests.
func githubWith(t *testing.T) (srv *httptest.Server, hits *atomic.Int64) {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, p := range []string{"kalka", "kalka-desktop", "kalka-plugin-gh"} {
		_ = tw.WriteHeader(&tar.Header{Name: "kalka_0.1.5_linux_amd64_desktop/" + p, Mode: 0o755, Size: 3, Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte("new"))
	}
	tw.Close()
	zw.Close()
	pkg := buf.Bytes()
	sum := sha256.Sum256(pkg)
	const asset = "kalka_0.1.5_linux_amd64_desktop.tar.gz"
	hits = new(atomic.Int64)
	var base string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		fmt.Fprintf(w, `[{"tag_name":"v0.2.0","html_url":"%[1]s/notes/0.2.0","assets":[]},
			{"tag_name":"v0.1.5","html_url":"%[1]s/notes/0.1.5","assets":[
			{"name":%[2]q,"browser_download_url":"%[1]s/dl/pkg"},
			{"name":"SHA256SUMS","browser_download_url":"%[1]s/dl/sums"}]}]`, base, asset)
	})
	mux.HandleFunc("/dl/pkg", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(pkg) })
	mux.HandleFunc("/dl/sums", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
	})
	srv = httptest.NewServer(mux)
	base = srv.URL
	t.Cleanup(srv.Close)
	return srv, hits
}

// updating is a daemon whose update check talks to the fake GitHub, with
// an install folder of the Linux desktop archive and a recording Apply.
func updating(t *testing.T, version string, delay time.Duration, applied *[]string) (*node, *httptest.Server, *atomic.Int64) {
	t.Helper()
	srv, hits := githubWith(t)
	dir := t.TempDir()
	for _, p := range []string{"kalka", "kalka-desktop", "kalka-plugin-gh"} {
		if err := os.WriteFile(filepath.Join(dir, p), []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	opt := func(o *Options) {
		o.Update = &UpdateOptions{Source: update.Source{API: srv.URL, Repo: "o/r", HTTP: srv.Client()}, Version: version,
			GOOS: "linux", GOARCH: "amd64", Exe: filepath.Join(dir, "kalka-desktop"), Install: true,
			Delay: delay, Every: time.Hour,
			Apply: func(_ update.Target, pkg string) error { *applied = append(*applied, pkg); return nil }}
	}
	return newWorld(t).node("ann@example.com", opt), srv, hits
}

func TestAutoUpdateDownloadsCompatible(t *testing.T) {
	var applied []string
	// The check waits long enough for the watch below to see its notice.
	n, _, _ := updating(t, "0.1.1", 300*time.Millisecond, &applied)
	events, stop := n.d.Bus().Watch()
	defer stop()
	waitFor(t, "ready", func() bool { return n.d.UpdateStatus().State == UpdateReady })
	st := n.d.UpdateStatus()
	if st.Latest != "0.1.5" || !st.Compatible || !st.CanInstall || st.NotesURL == "" || st.Error != "" {
		t.Fatalf("status = %+v", st)
	}
	var notice core.Notice
	for notice.Kind == "" {
		select {
		case e := <-events:
			if nn, ok := e.Data.(core.Notice); ok && e.Type == "notice" {
				notice = nn
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no notice")
		}
	}
	if notice.Kind != core.NoticeUpdate || !strings.Contains(notice.Title, "0.1.5") {
		t.Errorf("notice = %+v", notice)
	}
	// Nothing was installed without a click, and the notice is once per version.
	if len(applied) != 0 {
		t.Fatalf("applied %v", applied)
	}
	n.d.updateNotice("0.1.5")
	select {
	case e := <-events:
		if e.Type == "notice" {
			t.Fatal("a second notice for one version")
		}
	case <-time.After(50 * time.Millisecond):
	}
	raw, _ := os.ReadFile(filepath.Join(n.cfgDir, StateFile))
	if !strings.Contains(string(raw), "update_noticed: 0.1.5") {
		t.Errorf("state.yaml: %s", raw)
	}
}

func TestUpdateOffWaitsForTheButton(t *testing.T) {
	var applied []string
	n, _, _ := updating(t, "0.1.1", 500*time.Millisecond, &applied) // time to turn the setting off first
	off := false
	if _, err := n.d.Apply(Settings{AutoUpdate: &off}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "available", func() bool { return n.d.UpdateStatus().State == UpdateAvailable })
	time.Sleep(100 * time.Millisecond)
	if st := n.d.UpdateStatus(); st.State != UpdateAvailable {
		t.Fatalf("downloaded with the setting off: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(n.cfgDir, "updates")); err == nil {
		t.Fatal("something was downloaded")
	}
	c := NewClient(n.d.APIAddr(), n.d.Token())
	if err := c.InstallUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || filepath.Base(applied[0]) != "kalka_0.1.5_linux_amd64_desktop.tar.gz" {
		t.Fatalf("applied %v", applied)
	}
	waitFor(t, "the daemon to stop", func() bool {
		_, err := os.Stat(filepath.Join(n.cfgDir, DaemonFile))
		return err != nil
	})
}

func TestUpdateOutsidePolicyIsOffered(t *testing.T) {
	var applied []string
	n, _, _ := updating(t, "0.2.0-rc1", time.Millisecond, &applied) // a pre-release build sees 0.2.0, its own release
	// 0.2.0 has no package for this system in the fake: offered, not installable.
	waitFor(t, "available", func() bool { return n.d.UpdateStatus().State == UpdateAvailable })
	st := n.d.UpdateStatus()
	if st.Latest != "0.2.0" || st.CanInstall {
		t.Fatalf("status = %+v", st)
	}
	c := NewClient(n.d.APIAddr(), n.d.Token())
	if err := c.InstallUpdate(context.Background()); err == nil {
		t.Fatal("installed a release without a package")
	}
}

func TestDevBuildNeverChecks(t *testing.T) {
	var applied []string
	n, _, hits := updating(t, "dev", time.Millisecond, &applied)
	time.Sleep(100 * time.Millisecond)
	st := n.d.UpdateStatus()
	if st.Checks || st.State != UpdateNone || hits.Load() != 0 {
		t.Fatalf("status = %+v, %d requests", st, hits.Load())
	}
	c := NewClient(n.d.APIAddr(), n.d.Token())
	if _, err := c.UpdateStatus(context.Background(), true); err == nil {
		t.Fatal("a dev build checked")
	}
}

func TestAutoUpdateSettingDefaultsOn(t *testing.T) {
	n := newWorld(t).node("ann@example.com")
	if s := n.d.Settings(); s.AutoUpdate == nil || !*s.AutoUpdate {
		t.Fatalf("default = %v", s.AutoUpdate)
	}
	off := false
	if _, err := n.d.Apply(Settings{AutoUpdate: &off}); err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(n.cfgDir)
	if err != nil || st.AutoUpdate == nil || *st.AutoUpdate {
		t.Fatalf("state = %+v %v", st, err)
	}
	if s := st.Settings(); *s.AutoUpdate {
		t.Error("State.Settings reports it on")
	}
}
