package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/autostart"
	"github.com/golbinski/kalka/internal/cli"
	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/rendezvous"
)

// fakeUI records what the shell asks of the window and tray.
type fakeUI struct {
	mu       sync.Mutex
	url      string
	visible  bool
	hashes   []string
	notices  []string
	asked    []string
	infos    []string
	answer   bool
	tray     TrayModel
	quit     chan struct{}
	quitOnce sync.Once
	minimise int
}

func newFakeUI() *fakeUI { return &fakeUI{quit: make(chan struct{}), answer: true} }

func (u *fakeUI) SetTray(m TrayModel) { u.mu.Lock(); u.tray = m; u.mu.Unlock() }
func (u *fakeUI) LoadWindow(url string) {
	u.mu.Lock()
	u.url = url
	u.mu.Unlock()
}
func (u *fakeUI) ShowWindow(hash string) {
	u.mu.Lock()
	u.visible = true
	u.hashes = append(u.hashes, hash)
	u.mu.Unlock()
}
func (u *fakeUI) HideWindow()         { u.mu.Lock(); u.visible = false; u.mu.Unlock() }
func (u *fakeUI) MinimiseWindow()     { u.mu.Lock(); u.visible = false; u.minimise++; u.mu.Unlock() }
func (u *fakeUI) WindowVisible() bool { u.mu.Lock(); defer u.mu.Unlock(); return u.visible }
func (u *fakeUI) Notify(id, title, body, hash string) error {
	u.mu.Lock()
	u.notices = append(u.notices, id)
	u.mu.Unlock()
	return nil
}
func (u *fakeUI) Ask(title, message, yes, no string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.asked = append(u.asked, message)
	return u.answer
}
func (u *fakeUI) Info(title, message string) {
	u.mu.Lock()
	u.infos = append(u.infos, title+": "+message)
	u.mu.Unlock()
}
func (u *fakeUI) Quit() { u.quitOnce.Do(func() { close(u.quit) }) }

func (u *fakeUI) get(f func(*fakeUI) bool) bool { u.mu.Lock(); defer u.mu.Unlock(); return f(u) }

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

type fakeAutostart struct {
	mu sync.Mutex
	on bool
	n  int // enables
}

func (f *fakeAutostart) Get() (autostart.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return autostart.Status{Enabled: f.on, Where: "test"}, nil
}
func (f *fakeAutostart) Enable() (autostart.Status, error) {
	f.mu.Lock()
	f.on = true
	f.n++
	f.mu.Unlock()
	return f.Get()
}
func (f *fakeAutostart) Disable() error { f.mu.Lock(); f.on = false; f.mu.Unlock(); return nil }

// world is a config directory set up against a test rendezvous.
func world(t *testing.T) string {
	t.Helper()
	srv := &rendezvous.Server{ExpireTimeout: time.Second}
	mux := http.NewServeMux()
	srv.Mount(mux, "/")
	hs := httptest.NewServer(mux)
	t.Cleanup(func() { hs.Close(); srv.Close() })
	root := t.TempDir()
	dir := filepath.Join(root, "cfg")
	t.Setenv("KALKA_CONFIG_DIR", dir)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	var errb bytes.Buffer
	if code := cli.Run([]string{"setup", "--email", "alice@corp.com", "--rendezvous", hs.URL,
		"--folder", filepath.Join(root, "bp"), "--ui-port", strconv.Itoa(port)}, nil, io.Discard, &errb); code != 0 {
		t.Fatalf("setup: %s", errb.String())
	}
	return dir
}

// TestShellInProcess: the first start runs the daemon in-process; a
// second start shows the first one's window, or with --hidden nothing;
// closing hides with a one-time notice; Quit asks and stops the daemon.
func TestShellInProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a daemon")
	}
	dir := world(t)
	ctx := context.Background()
	plan, err := Prepare(ctx, dir, false)
	if err != nil || plan.Action != StartInProcess {
		t.Fatalf("prepare: %+v %v", plan, err)
	}
	ui, as := newFakeUI(), &fakeAutostart{}
	s := &Shell{UI: ui, Dir: dir, TrayHost: true, Autostart: as}
	s.Start(plan)
	t.Cleanup(func() { s.Stop() })

	eventually(t, "the window", func() bool {
		return ui.get(func(u *fakeUI) bool {
			return strings.Contains(u.url, "token=") && strings.HasSuffix(u.url, "&window=1") && u.visible
		})
	})
	eventually(t, "the tray", func() bool { return ui.get(func(u *fakeUI) bool { return u.tray.State == TrayOnline }) })
	// First run turned start at login on, once.
	if as.n != 1 || !as.on {
		t.Fatalf("autostart enables %d", as.n)
	}
	c, err := daemon.Connect(dir)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the shell attached", func() bool {
		sum, err := c.Summary(ctx)
		return err == nil && sum.Shell == "desktop"
	})

	// A second start of the shell shows this window and exits.
	ui.HideWindow()
	if p, err := Prepare(ctx, dir, false); err != nil || p.Action != StartShowOther {
		t.Fatalf("second start: %+v %v", p, err)
	}
	eventually(t, "the window shown again", func() bool { return ui.WindowVisible() })
	ui.HideWindow()
	if p, err := Prepare(ctx, dir, true); err != nil || p.Action != StartExit {
		t.Fatalf("hidden second start: %+v %v", p, err)
	}
	time.Sleep(200 * time.Millisecond)
	if ui.WindowVisible() {
		t.Fatal("--hidden showed the window")
	}
	// Bare `kalka` takes the same path.
	if err := c.ShowUI(ctx, "plans/a.md"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the document", func() bool {
		return ui.get(func(u *fakeUI) bool { return u.hashes[len(u.hashes)-1] == "#/doc/plans%2Fa.md" })
	})

	// Close: hide, one notice the first time only.
	s.Close()
	s.Close()
	if ui.WindowVisible() || ui.get(func(u *fakeUI) bool { return strings.Count(strings.Join(u.notices, " "), "close-notice") != 1 }) {
		t.Fatalf("close: %+v", ui.notices)
	}
	if set, _ := c.Settings(ctx); set.CloseNoticeShown == nil || !*set.CloseNoticeShown {
		t.Fatal("close notice not recorded")
	}

	// Pause from the tray.
	s.TogglePause()
	eventually(t, "paused", func() bool { return ui.get(func(u *fakeUI) bool { return u.tray.State == TrayPaused }) })
	s.TogglePause()
	// Start at login from the tray.
	s.ToggleStartAtLogin()
	if as.on {
		t.Fatal("start at login still on")
	}

	// Quit asks, then stops the daemon.
	ui.mu.Lock()
	ui.answer = false
	ui.mu.Unlock()
	s.Quit()
	if _, err := c.Status(ctx); err != nil {
		t.Fatal("cancelled quit stopped the daemon")
	}
	ui.mu.Lock()
	ui.answer = true
	ui.mu.Unlock()
	go s.Quit()
	select {
	case <-ui.quit:
	case <-time.After(30 * time.Second):
		t.Fatal("quit did not end the shell")
	}
	if strings.Contains(ui.asked[0], "without a window") {
		t.Fatalf("question: %q", ui.asked[0])
	}
	if p, err := Prepare(ctx, dir, false); err != nil || p.Action != StartInProcess {
		t.Fatalf("after quit: %+v %v", p, err)
	} else {
		p.Lock.Release()
	}

	// A later start does not turn start at login on again.
	plan, _ = Prepare(ctx, dir, true)
	ui2 := newFakeUI()
	s2 := &Shell{UI: ui2, Dir: dir, Hidden: true, TrayHost: true, Autostart: as}
	s2.Start(plan)
	eventually(t, "the second shell", func() bool { return ui2.get(func(u *fakeUI) bool { return u.url != "" }) })
	if as.n != 1 || ui2.WindowVisible() {
		t.Fatalf("second run: enables %d, visible %v", as.n, ui2.WindowVisible())
	}
	// `kalka stop` ends the shell too.
	if _, err := daemonClient(t, dir).Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ui2.quit:
	case <-time.After(30 * time.Second):
		t.Fatal("kalka stop did not end the shell")
	}
}

func daemonClient(t *testing.T, dir string) *daemon.Client {
	t.Helper()
	c, err := daemon.Connect(dir)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestShellAttach: with a headless daemon holding the lock the shell
// attaches; closing with keep-running off leaves the daemon running;
// Quit says it stops that daemon too, and does.
func TestShellAttach(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a daemon")
	}
	dir := world(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	as := &fakeAutostart{}
	go func() {
		done <- cli.ServeDaemon(ctx, cli.DaemonOptions{Dir: dir, NoNotifications: true, Autostart: as, Stdout: io.Discard, Stderr: io.Discard})
	}()
	eventually(t, "the headless daemon", func() bool { return answering(ctx, dir) != nil })

	plan, err := Prepare(ctx, dir, false)
	if err != nil || plan.Action != StartAttach {
		t.Fatalf("prepare: %+v %v", plan, err)
	}
	ui := newFakeUI()
	s := &Shell{UI: ui, Dir: dir, TrayHost: true}
	s.Start(plan)
	eventually(t, "the window", func() bool { return ui.WindowVisible() })
	c := daemonClient(t, dir)
	eventually(t, "attached", func() bool { sum, _ := c.Summary(ctx); return sum.Shell == "desktop" })

	// Keep running off: closing quits the shell only.
	off := false
	if _, err := c.ApplySettings(ctx, daemon.Settings{KeepRunningOnClose: &off}); err != nil {
		t.Fatal(err)
	}
	s.refresh()
	s.Close()
	select {
	case <-ui.quit:
	case <-time.After(10 * time.Second):
		t.Fatal("close did not quit the shell")
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := c.Status(ctx); err != nil {
		t.Fatal("closing stopped the headless daemon")
	}
	eventually(t, "the shell detached", func() bool { sum, _ := c.Summary(ctx); return sum.Shell == "" })

	// A new shell attaches again; Quit stops the headless daemon.
	plan, _ = Prepare(ctx, dir, false)
	ui = newFakeUI()
	s = &Shell{UI: ui, Dir: dir, TrayHost: true}
	s.Start(plan)
	eventually(t, "the tray", func() bool { return ui.get(func(u *fakeUI) bool { return u.tray.State != TrayStarting }) })
	go s.Quit()
	select {
	case <-ui.quit:
	case <-time.After(30 * time.Second):
		t.Fatal("quit did not end the shell")
	}
	if !strings.Contains(ui.asked[0], "without a window") {
		t.Fatalf("question: %q", ui.asked[0])
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("daemon exit %d", code)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the headless daemon did not stop")
	}
}

// TestPresentWhenHidden (spec 22.4): with the window hidden the shell
// shows notices as notifications; with it visible the page shows them.
func TestPresentWhenHidden(t *testing.T) {
	ui := newFakeUI()
	s := &Shell{UI: ui, kick: make(chan struct{}, 1)}
	s.onEvent("notice", json.RawMessage(`{"kind":"joined","title":"Joined","message":"carol joined","team":"lending"}`))
	if got := strings.Join(ui.notices, " "); got != "notice-joined-lending" {
		t.Fatalf("hidden: %q", got)
	}
	ui.ShowWindow("")
	s.onEvent("notice", json.RawMessage(`{"kind":"joined","title":"Joined","message":"x"}`))
	if len(ui.notices) != 1 {
		t.Fatalf("visible: %q", ui.notices)
	}
	// ui-show opens the document.
	s.onEvent("ui-show", json.RawMessage(`{"path":"a b.md"}`))
	if !ui.WindowVisible() || ui.hashes[len(ui.hashes)-1] != "#/doc/a%20b.md" {
		t.Fatalf("ui-show: %q", ui.hashes)
	}
}
