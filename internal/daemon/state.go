package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/golbinski/kalka/internal/autostart"
	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/fsutil"
)

// StateFile holds the settings the daemon owns (spec 5, 22.2). It is
// changed only through the local API.
const StateFile = "state.yaml"

// State is state.yaml. Settings that default to on are pointers, so a
// missing key means the default.
type State struct {
	Paused             bool  `yaml:"paused"`
	Notifications      *bool `yaml:"notifications,omitempty"`
	KeepRunningOnClose *bool `yaml:"keep_running_on_close,omitempty"`
	AutoUpdate         *bool `yaml:"auto_update,omitempty"`
	// UpdateNoticed is the version whose "ready" notice was raised (spec 24.2).
	UpdateNoticed        string `yaml:"update_noticed,omitempty"`
	CloseNoticeShown     bool   `yaml:"close_notice_shown,omitempty"`
	AutostartInitialized bool   `yaml:"autostart_initialized,omitempty"`
	// Seen marks handed-to-you items as seen (spec 11.3): document id ->
	// the release or reassignment it was seen at.
	Seen map[string]string `yaml:"seen,omitempty"`
}

func on(b *bool) bool { return b == nil || *b }

// LoadState reads state.yaml; a missing file is the defaults.
func LoadState(dir string) (State, error) {
	var s State
	raw, err := os.ReadFile(filepath.Join(dir, StateFile))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return State{}, fmt.Errorf("%s: %w", StateFile, err)
	}
	return s, nil
}

func saveState(dir string, s State) error {
	raw, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return fsutil.WritePrivate(filepath.Join(dir, StateFile), raw)
}

// Autostart is the system's start-at-login entry (spec 22.2): the
// system's entry is the truth, it is not copied into state.yaml.
type Autostart interface {
	Get() (autostart.Status, error)
	Enable() (autostart.Status, error)
	Disable() error
}

// SystemAutostart is the real entry for this binary.
type SystemAutostart struct{}

func (SystemAutostart) Get() (autostart.Status, error) { return autostart.Get() }
func (SystemAutostart) Enable() (autostart.Status, error) {
	exe, err := autostart.Executable()
	if err != nil {
		return autostart.Status{}, err
	}
	return autostart.Enable(exe)
}
func (SystemAutostart) Disable() error { return autostart.Disable() }

// Settings is GET /api/settings, and with every field optional the body of
// PUT /api/settings.
type Settings struct {
	Paused             *bool `json:"paused,omitempty"`
	Notifications      *bool `json:"notifications,omitempty"`
	KeepRunningOnClose *bool `json:"keep_running_on_close,omitempty"`
	AutoUpdate         *bool `json:"auto_update,omitempty"`
	StartAtLogin       *bool `json:"start_at_login,omitempty"`
	// StartAtLoginAvailable is false where this kalka cannot change the
	// entry (tests, or a build without autostart).
	StartAtLoginAvailable bool   `json:"start_at_login_available"`
	StartAtLoginError     string `json:"start_at_login_error,omitempty"`
	CloseNoticeShown      *bool  `json:"close_notice_shown,omitempty"`
	// AutostartInitialized is set once the desktop shell turned start at
	// login on at its first start (spec 22.2).
	AutostartInitialized *bool `json:"autostart_initialized,omitempty"`
}

// settings guards state.yaml.
type settings struct {
	mu sync.Mutex
	st State
}

func ptr(b bool) *bool { return &b }

func (d *Daemon) notificationsOn() bool {
	d.set.mu.Lock()
	defer d.set.mu.Unlock()
	return on(d.set.st.Notifications)
}

// Settings reports the current settings.
func (d *Daemon) Settings() Settings {
	d.set.mu.Lock()
	st := d.set.st
	d.set.mu.Unlock()
	out := Settings{Paused: ptr(d.Paused()), Notifications: ptr(on(st.Notifications)),
		KeepRunningOnClose: ptr(on(st.KeepRunningOnClose)), AutoUpdate: ptr(on(st.AutoUpdate)), CloseNoticeShown: ptr(st.CloseNoticeShown),
		AutostartInitialized: ptr(st.AutostartInitialized)}
	if a := d.opts.Autostart; a != nil {
		out.StartAtLoginAvailable = true
		s, err := a.Get()
		out.StartAtLogin = ptr(s.Enabled)
		if err != nil {
			out.StartAtLoginError = err.Error()
		}
	}
	return out
}

// Settings are the settings state.yaml holds, read without a daemon:
// start at login, which the system's entry decides, is left out.
func (s State) Settings() Settings {
	return Settings{Paused: ptr(s.Paused), Notifications: ptr(on(s.Notifications)),
		KeepRunningOnClose: ptr(on(s.KeepRunningOnClose)), AutoUpdate: ptr(on(s.AutoUpdate)), CloseNoticeShown: ptr(s.CloseNoticeShown),
		AutostartInitialized: ptr(s.AutostartInitialized)}
}

// Apply changes the settings given and saves state.yaml.
func (d *Daemon) Apply(in Settings) (Settings, error) {
	if in.StartAtLogin != nil {
		a := d.opts.Autostart
		if a == nil {
			return Settings{}, errors.New("start at login cannot be changed by this kalka")
		}
		var err error
		if *in.StartAtLogin {
			_, err = a.Enable()
		} else {
			err = a.Disable()
		}
		if err != nil {
			return Settings{}, fmt.Errorf("start at login: %w", err)
		}
	}
	d.set.mu.Lock()
	st := d.set.st
	if in.Paused != nil {
		st.Paused = *in.Paused
	}
	if in.Notifications != nil {
		st.Notifications = ptr(*in.Notifications)
	}
	if in.KeepRunningOnClose != nil {
		st.KeepRunningOnClose = ptr(*in.KeepRunningOnClose)
	}
	if in.AutoUpdate != nil {
		st.AutoUpdate = ptr(*in.AutoUpdate)
	}
	if in.CloseNoticeShown != nil {
		st.CloseNoticeShown = *in.CloseNoticeShown
	}
	if in.AutostartInitialized != nil {
		st.AutostartInitialized = *in.AutostartInitialized
	}
	err := saveState(d.dir, st)
	if err == nil {
		d.set.st = st
	}
	d.set.mu.Unlock()
	if err != nil {
		return Settings{}, err
	}
	d.SetNotifications(on(st.Notifications))
	d.SetPaused(st.Paused)
	if d.upd != nil && in.AutoUpdate != nil && *in.AutoUpdate {
		// Turned on: take what is waiting. A check is cheap (ETag).
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			_ = d.upd.check(ctx)
		}()
	}
	d.Bus().Publish(core.BusEvent{Type: "settings"})
	return d.Settings(), nil
}

func (d *Daemon) apiSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, d.Settings())
		return
	}
	var in Settings
	if err := readBody(r, &in); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	out, err := d.Apply(in)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (d *Daemon) apiPause(paused bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		out, err := d.Apply(Settings{Paused: &paused})
		if err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// presentNotices shows notices as OS notifications while no UI page is
// connected (a page shows them itself).
func (d *Daemon) presentNotices(done <-chan struct{}, events <-chan core.BusEvent, stop func()) {
	defer stop()
	for {
		select {
		case <-done:
			return
		case e := <-events:
			n, ok := e.Data.(core.Notice)
			if e.Type != "notice" || !ok || d.Bus().Clients() > 0 || d.Bus().Shells() > 0 {
				continue
			}
			d.opts.Notify(n)
		}
	}
}

// seen is the seen marks, for the node.
func (d *Daemon) seen() map[string]string {
	d.set.mu.Lock()
	defer d.set.mu.Unlock()
	out := make(map[string]string, len(d.set.st.Seen))
	for k, v := range d.set.st.Seen {
		out[k] = v
	}
	return out
}

// MarkSeen clears the handed-to-you item of the document at path
// (opening it, or `kalka waiting --seen`). Marks that no longer name what
// makes a document yours are dropped. It reports whether an item was
// waiting and is cleared now.
func (d *Daemon) MarkSeen(path string) (bool, error) {
	id, mark, ok := d.SeenMark(path)
	if !ok {
		return false, nil
	}
	current := d.WaitingMarks()
	d.set.mu.Lock()
	st := d.set.st
	seen := map[string]string{id: mark}
	for k, v := range st.Seen {
		if k != id && current[k] == v {
			seen[k] = v
		}
	}
	if st.Seen[id] == mark {
		d.set.mu.Unlock()
		return false, nil // seen already
	}
	st.Seen = seen
	err := saveState(d.dir, st)
	if err == nil {
		d.set.st = st
	}
	d.set.mu.Unlock()
	if err != nil {
		return false, err
	}
	d.Bus().Publish(core.BusEvent{Type: "waiting"})
	return true, nil
}
