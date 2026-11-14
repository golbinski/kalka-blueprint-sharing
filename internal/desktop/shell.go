package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/golbinski/kalka/internal/cli"
	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/lockfile"
	"github.com/golbinski/kalka/internal/name"
)

// UI is the window, the tray and native dialogs; the Wails binding
// implements it, tests fake it. Every method may be called from any
// goroutine.
type UI interface {
	SetTray(TrayModel)
	// LoadWindow sets the page the window shows, without showing it.
	LoadWindow(url string)
	// ShowWindow shows and focuses the window; a hash navigates the page.
	ShowWindow(hash string)
	HideWindow()
	MinimiseWindow()
	WindowVisible() bool
	// Notify shows an OS notification; its click calls Shell.Clicked
	// with hash. An error means notifications are unavailable.
	Notify(id, title, body, hash string) error
	// Ask shows a native question and blocks until it is answered.
	Ask(title, message, yes, no string) bool
	Info(title, message string)
	// Quit ends the shell's event loop.
	Quit()
}

// Plan is the outcome of Prepare: what this start of the shell does.
type Plan struct {
	Action StartAction
	Lock   *lockfile.Lock // StartInProcess
	Client *daemon.Client // StartAttach
}

// HolderTimeout bounds how long Prepare waits for the daemon holding the
// lock to answer (it may be starting).
var HolderTimeout = 15 * time.Second

// Prepare takes the single-daemon lock or finds out who holds it, and
// picks the start (DecideStart). StartShowOther has already asked the
// other shell to show its window.
func Prepare(ctx context.Context, dir string, hidden bool) (Plan, error) {
	deadline := time.Now().Add(HolderTimeout)
	for {
		lock, err := daemon.AcquireLock(dir)
		if err == nil {
			return Plan{Action: StartInProcess, Lock: lock}, nil
		}
		if !errors.Is(err, lockfile.ErrHeld) {
			return Plan{}, err
		}
		c := answering(ctx, dir)
		var sum core.Summary
		if c != nil {
			if sum, err = c.Summary(ctx); err != nil {
				c = nil
			}
		}
		switch a := DecideStart(false, c != nil, sum.Shell == "desktop", hidden); a {
		case StartWait:
			if time.Now().After(deadline) {
				return Plan{}, fmt.Errorf("another %s holds the lock in %s but does not answer; it may be waiting for setup in a browser", name.Product, dir)
			}
			select {
			case <-ctx.Done():
				return Plan{}, ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		case StartShowOther:
			err := c.ShowUI(ctx, "")
			if errors.Is(err, daemon.ErrNoShell) { // it just left
				return Plan{Action: StartAttach, Client: c}, nil
			}
			return Plan{Action: a}, err
		case StartAttach:
			return Plan{Action: a, Client: c}, nil
		default:
			return Plan{Action: a}, nil
		}
	}
}

// answering returns a client if a daemon answers its status call.
func answering(ctx context.Context, dir string) *daemon.Client {
	c, err := daemon.Connect(dir)
	if err != nil {
		return nil
	}
	sctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := c.Status(sctx); err != nil {
		return nil
	}
	return c
}

// Shell drives the window and tray from the daemon's API.
type Shell struct {
	UI     UI
	Dir    string    // config directory
	Log    io.Writer // the shell's own messages
	Hidden bool      // started with --hidden
	// TrayHost: a tray is there to bring a hidden window back.
	TrayHost bool
	// Autostart overrides the in-process daemon's start-at-login back end
	// (tests); nil is the system's.
	Autostart daemon.Autostart
	Loc       *time.Location

	mu       sync.Mutex
	c        *daemon.Client
	attached bool
	stop     context.CancelFunc // the in-process daemon
	done     chan struct{}      // closed when it returned
	code     int
	sum      core.Summary
	set      daemon.Settings
	tray     TrayModel
	quitting bool
	ctx      context.Context
	cancel   context.CancelFunc
	kick     chan struct{}
}

// Start runs the plan; it returns at once and works in the background.
func (s *Shell) Start(p Plan) {
	if s.Log == nil {
		s.Log = io.Discard
	}
	if s.Loc == nil {
		s.Loc = time.Local
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.kick = make(chan struct{}, 1)
	s.tray = StartingTray()
	s.UI.SetTray(s.tray)
	switch p.Action {
	case StartInProcess:
		dctx, stop := context.WithCancel(context.Background())
		s.mu.Lock()
		s.stop, s.done = stop, make(chan struct{})
		s.mu.Unlock()
		go func() {
			code := cli.ServeDaemon(dctx, cli.DaemonOptions{Dir: s.Dir, Background: true, Shell: true, Lock: p.Lock,
				OpenUI: s.openUI, Autostart: s.Autostart, Stdout: io.Discard, Stderr: io.Discard})
			s.mu.Lock()
			s.code = code
			s.mu.Unlock()
			close(s.done)
		}()
		go s.waitForDaemon()
	case StartAttach:
		s.mu.Lock()
		s.attached = true
		s.mu.Unlock()
		go s.connected(p.Client)
	}
}

// openUI shows the setup form, before the daemon itself runs.
func (s *Shell) openUI(url string) {
	s.UI.LoadWindow(url)
	s.UI.ShowWindow("")
}

// waitForDaemon connects to the in-process daemon once it answers.
func (s *Shell) waitForDaemon() {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			s.mu.Lock()
			code, quitting := s.code, s.quitting
			s.mu.Unlock()
			if !quitting {
				if code != 0 {
					s.UI.Info(name.Product+" could not start", fmt.Sprintf("See %s.log in %s.", name.Product, s.Dir))
				}
				s.UI.Quit()
			}
			return
		case <-t.C:
			if c := answering(s.ctx, s.Dir); c != nil {
				s.connected(c)
				return
			}
		}
	}
}

// connected sets up the window and tray for a daemon that answers.
func (s *Shell) connected(c *daemon.Client) {
	s.mu.Lock()
	s.c = c
	s.mu.Unlock()
	ctx := s.ctx
	set, err := c.Settings(ctx)
	if err != nil {
		fmt.Fprintf(s.Log, "settings: %v\n", err)
	}
	// Start at login is turned on once, at the shell's first start
	// (spec 22.2); after that only the user changes it.
	if err == nil && !on(set.AutostartInitialized) {
		in := daemon.Settings{AutostartInitialized: ptr(true)}
		if set.StartAtLoginAvailable && !on(set.StartAtLogin) {
			in.StartAtLogin = ptr(true)
		}
		if out, err := c.ApplySettings(ctx, in); err != nil {
			fmt.Fprintf(s.Log, "start at login: %v\n", err)
		} else {
			set = out
		}
	}
	s.mu.Lock()
	s.set = set
	s.mu.Unlock()
	s.UI.LoadWindow(c.URL() + "&" + daemon.WindowParam + "=1")
	if !s.Hidden || s.UI.WindowVisible() {
		s.UI.ShowWindow("")
	}
	s.refresh()
	if sum := s.summary(); sum.Paused {
		s.notify("paused", name.Product+" is paused", "It is not syncing with your team until you resume.", "#/settings")
	}
	go s.events(c)
	go s.refresher()
}

// events follows the daemon's bus as its shell, until the daemon goes.
func (s *Shell) events(c *daemon.Client) {
	for {
		_ = c.ShellEvents(s.ctx, s.onEvent)
		if s.isQuitting() || s.ctx.Err() != nil {
			return
		}
		if answering(s.ctx, s.Dir) == nil {
			s.daemonGone()
			return
		}
		time.Sleep(time.Second)
	}
}

// daemonGone: the daemon stopped (`kalka stop`, or a crash); the shell
// ends with it.
func (s *Shell) daemonGone() {
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done != nil {
		<-done
	}
	fmt.Fprintf(s.Log, "%s stopped; the shell quits\n", name.Product)
	s.UI.Quit()
}

func (s *Shell) onEvent(typ string, data json.RawMessage) {
	switch typ {
	case "notice":
		var n core.Notice
		if json.Unmarshal(data, &n) == nil && !s.UI.WindowVisible() {
			s.notify("notice-"+n.Kind+"-"+n.Path+n.Team, n.Title, n.Message, NoticeHash(n))
		}
	case "prompt":
		var p core.Pending
		if json.Unmarshal(data, &p) == nil && p.Scope == "" && !s.UI.WindowVisible() && s.notificationsOn() {
			title, body := PromptText(p.Prompt)
			if err := s.UI.Notify("prompt-"+p.ID, title, body, ""); err != nil {
				// No notification centre (an unsigned macOS build): a
				// dialog, since the prompt needs an answer in minutes.
				go func() {
					if s.UI.Ask(title, body, "Open "+name.Product, "Later") {
						s.UI.ShowWindow("")
					}
				}()
			}
		}
	case "ui-show":
		var q struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(data, &q)
		s.UI.ShowWindow(DocHash(q.Path))
	}
	s.poke()
}

// notify shows a notice; without a notification centre it is shown only
// in the window (spec 22.4).
func (s *Shell) notify(id, title, body, hash string) {
	if err := s.UI.Notify(id, title, body, hash); err != nil {
		fmt.Fprintf(s.Log, "notice %q not shown: %v\n", title, err)
	}
}

func (s *Shell) poke() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// refresher updates the tray on events, at most once a second, and every
// half minute for the last-sync time.
func (s *Shell) refresher() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.kick:
		case <-t.C:
		}
		s.refresh()
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// refresh reads the summary and settings and redraws the tray.
func (s *Shell) refresh() {
	c := s.client()
	if c == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	sum, err := c.Summary(ctx)
	if err != nil {
		return
	}
	set, err := c.Settings(ctx)
	if err != nil {
		return
	}
	m := Tray(sum, set, s.Loc)
	s.mu.Lock()
	s.sum, s.set, s.tray = sum, set, m
	s.mu.Unlock()
	s.UI.SetTray(m)
}

func (s *Shell) client() *daemon.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.c
}

func (s *Shell) summary() core.Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sum
}

func (s *Shell) notificationsOn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.set.Notifications == nil || *s.set.Notifications
}

func (s *Shell) isQuitting() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quitting
}

// Tray returns the last tray model.
func (s *Shell) Tray() TrayModel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tray
}

// Attached reports whether the shell is attached to a headless daemon.
func (s *Shell) Attached() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attached
}

// Open is the tray's "Open kalka".
func (s *Shell) Open() { s.UI.ShowWindow("") }

// OpenWaiting is "N waiting for you".
func (s *Shell) OpenWaiting() { s.UI.ShowWindow(s.Tray().WaitingHash) }

// Clicked handles a click on a notification.
func (s *Shell) Clicked(hash string) { s.UI.ShowWindow(hash) }

// TogglePause is "Pause sync" / "Resume sync".
func (s *Shell) TogglePause() {
	c := s.client()
	if c == nil {
		return
	}
	if _, err := c.SetPaused(s.ctx, !s.summary().Paused); err != nil {
		s.UI.Info(name.Product, err.Error())
	}
	s.refresh()
}

// ToggleStartAtLogin is the "Start at login" check item.
func (s *Shell) ToggleStartAtLogin() {
	c := s.client()
	if c == nil {
		return
	}
	s.mu.Lock()
	want := !on(s.set.StartAtLogin)
	s.mu.Unlock()
	out, err := c.ApplySettings(s.ctx, daemon.Settings{StartAtLogin: &want})
	if err == nil && out.StartAtLoginError != "" {
		err = errors.New(out.StartAtLoginError)
	}
	if err != nil {
		s.UI.Info("Start at login", err.Error())
	}
	s.refresh()
}

// Quit is "Quit kalka": it asks first, then stops the daemon after its
// running rounds, the headless one too when attached (spec 22.1).
func (s *Shell) Quit() {
	title, msg := QuitQuestion(s.Attached())
	if !s.UI.Ask(title, msg, "Quit", "Cancel") {
		return
	}
	s.shutdown(true)
}

// shutdown stops the in-process daemon, or with stopAttached the
// attached one, then ends the shell.
func (s *Shell) shutdown(stopAttached bool) {
	s.mu.Lock()
	if s.quitting {
		s.mu.Unlock()
		return
	}
	s.quitting = true
	stop, done, c, attached := s.stop, s.done, s.c, s.attached
	s.mu.Unlock()
	switch {
	case stop != nil:
		stop()
		<-done
	case attached && stopAttached && c != nil:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, _ = c.Shutdown(ctx)
		cancel()
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.UI.Quit()
}

// Close handles closing the window (spec 22.2); the binding cancels the
// close itself and leaves it to this.
func (s *Shell) Close() {
	s.mu.Lock()
	set, c := s.set, s.c
	s.mu.Unlock()
	keep := c == nil || set.KeepRunningOnClose == nil || *set.KeepRunningOnClose
	action, notice := OnClose(keep, s.TrayHost, c == nil || on(set.CloseNoticeShown))
	switch action {
	case CloseHide:
		s.UI.HideWindow()
	case CloseMinimise:
		s.UI.MinimiseWindow()
	case CloseQuit:
		// The user chose this meaning: no question. A headless daemon the
		// shell attached to keeps running.
		go s.shutdown(false)
		return
	}
	if notice {
		title, body := CloseNotice(action)
		if err := s.UI.Notify("close-notice", title, body, ""); err != nil {
			s.UI.Info(title, body)
		}
		if out, err := c.ApplySettings(s.ctx, daemon.Settings{CloseNoticeShown: ptr(true)}); err == nil {
			s.mu.Lock()
			s.set = out
			s.mu.Unlock()
		}
	}
}

// Stop ends the shell without asking (the system is logging out); the
// in-process daemon finishes its rounds first.
func (s *Shell) Stop() { s.shutdown(false) }

func ptr(b bool) *bool { return &b }
