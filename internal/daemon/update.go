package daemon

// Updates (spec 24): the daemon looks for new releases on GitHub, downloads
// and verifies them, and, when the desktop shell hosts it, installs one
// and restarts on a click. internal/update does the work; this is the
// schedule, the state the page and the tray show, and the endpoints.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/update"
)

// UpdateOptions switch the update check on.
type UpdateOptions struct {
	Source update.Source
	// Version is the running build's; "dev" or anything that is not a
	// version never checks.
	Version string
	// GOOS and GOARCH pick the package; Exe is the running program (tests
	// change them; the defaults are this process's).
	GOOS, GOARCH, Exe string
	// Install: this process is the desktop shell, so it can restart
	// itself after an update. Without it, updates are only announced.
	Install bool
	// Delay is the wait before the first check, Every the time between
	// checks (30 s and 6 h by default).
	Delay, Every time.Duration
	// Apply replaces update.Apply (tests).
	Apply func(update.Target, string) error
}

// Update states.
const (
	UpdateNone        = "none"
	UpdateAvailable   = "available"
	UpdateDownloading = "downloading"
	UpdateReady       = "ready"
	UpdateError       = "error"
)

// UpdateStatus is GET /api/update.
type UpdateStatus struct {
	Current string `json:"current"`
	// Checks is false for builds that never check (dev builds).
	Checks bool   `json:"checks"`
	Latest string `json:"latest,omitempty"`
	State  string `json:"state"`
	// Compatible: the release is within the caret policy of the running
	// version, so it is installed on its own when the setting is on.
	Compatible bool   `json:"compatible"`
	NotesURL   string `json:"notes_url,omitempty"`
	// CanInstall: this kalka can replace itself with the release.
	CanInstall bool      `json:"can_install"`
	CheckedAt  time.Time `json:"checked_at,omitzero"`
	Error      string    `json:"error,omitempty"`
}

type updater struct {
	d      *Daemon
	o      UpdateOptions
	cur    update.Version
	target update.Target

	mu   sync.Mutex
	st   UpdateStatus
	rel  update.Release
	file string
	etag string
	rels []update.Release
	busy bool
}

func newUpdater(d *Daemon, o UpdateOptions) *updater {
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.GOARCH == "" {
		o.GOARCH = runtime.GOARCH
	}
	if o.Exe == "" {
		o.Exe, _ = os.Executable()
	}
	if o.Delay == 0 {
		o.Delay = 30 * time.Second
	}
	if o.Every == 0 {
		o.Every = 6 * time.Hour
	}
	if o.Apply == nil {
		o.Apply = update.Apply
	}
	u := &updater{d: d, o: o, st: UpdateStatus{Current: o.Version, State: UpdateNone}}
	if v, err := update.Parse(o.Version); err == nil && o.Version != "dev" {
		u.cur, u.st.Checks = v, true
		if o.Install {
			u.target = update.Detect(o.GOOS, o.Exe)
		}
	}
	return u
}

func (u *updater) dir(v update.Version) string {
	return filepath.Join(u.d.dir, "updates", v.String())
}

// status is a copy of the current status.
func (u *updater) status() UpdateStatus {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.st
}

// loop checks after the delay and then at every interval, until ctx ends.
func (u *updater) loop(ctx context.Context) {
	if !u.st.Checks {
		return
	}
	t := time.NewTimer(u.o.Delay)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := u.check(ctx); err != nil {
			u.d.log("update check: %v", err)
		}
		t.Reset(u.o.Every)
	}
}

func (d *Daemon) log(format string, args ...any) {
	if d.opts.Log != nil {
		fmt.Fprintf(d.opts.Log, format+"\n", args...)
	}
}

func (u *updater) changed() { u.d.Bus().Publish(core.BusEvent{Type: "settings"}) }

// begin marks a check or download as running; false when one already is.
func (u *updater) begin() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.busy {
		return false
	}
	u.busy = true
	return true
}

func (u *updater) end() { u.mu.Lock(); u.busy = false; u.mu.Unlock() }

// check asks GitHub for releases; a compatible one is downloaded at once
// when the setting says so.
func (u *updater) check(ctx context.Context) error {
	if !u.st.Checks {
		return errors.New("this build does not check for updates")
	}
	if !u.begin() {
		return nil
	}
	defer u.end()
	u.mu.Lock()
	etag, rels := u.etag, u.rels
	u.mu.Unlock()
	got, newTag, same, err := u.o.Source.Releases(ctx, etag)
	if err == nil && !same {
		rels = got
	}
	u.mu.Lock()
	u.st.CheckedAt = time.Now()
	if err != nil {
		u.st.Error = err.Error()
		if u.st.State == UpdateNone || u.st.State == UpdateAvailable {
			u.st.State = UpdateError
		}
		u.mu.Unlock()
		u.changed()
		return err
	}
	u.etag, u.rels, u.st.Error = newTag, rels, ""
	rel, compat, ok := update.Pick(rels, u.cur)
	switch {
	case !ok:
		u.st.State, u.st.Latest, u.st.NotesURL, u.st.CanInstall = UpdateNone, "", "", false
		u.rel, u.file = update.Release{}, ""
	case u.st.Latest != rel.Version.String() || u.st.State == UpdateError:
		u.rel, u.file = rel, ""
		asset, _ := update.AssetName(rel.Version, u.o.GOOS, u.o.GOARCH)
		_, has := rel.Assets[asset]
		u.st.State, u.st.Latest, u.st.Compatible, u.st.NotesURL = UpdateAvailable, rel.Version.String(), compat, rel.NotesURL
		u.st.CanInstall = u.target.Kind != update.Unknown && has
	}
	auto := u.st.State == UpdateAvailable && u.st.Compatible && u.st.CanInstall
	u.mu.Unlock()
	if !ok {
		u.pruneDownloads(u.cur) // an update that was installed leaves its package behind
	}
	u.changed()
	if auto && u.d.autoUpdateOn() {
		return u.fetch(ctx)
	}
	return nil
}

// fetch downloads and verifies the chosen release; the caller holds busy.
func (u *updater) fetch(ctx context.Context) error {
	u.mu.Lock()
	rel := u.rel
	u.st.State = UpdateDownloading
	u.mu.Unlock()
	u.changed()
	asset, _ := update.AssetName(rel.Version, u.o.GOOS, u.o.GOARCH)
	path, err := u.o.Source.Fetch(ctx, rel, asset, u.dir(rel.Version))
	u.mu.Lock()
	if err != nil {
		u.st.State, u.st.Error = UpdateError, err.Error()
		u.mu.Unlock()
		u.changed()
		return err
	}
	u.st.State, u.st.Error, u.file = UpdateReady, "", path
	u.mu.Unlock()
	u.pruneDownloads(rel.Version)
	u.changed()
	u.d.updateNotice(rel.Version.String())
	return nil
}

// pruneDownloads removes the packages of other versions.
func (u *updater) pruneDownloads(keep update.Version) {
	root := filepath.Join(u.d.dir, "updates")
	ents, _ := os.ReadDir(root)
	for _, e := range ents {
		if e.Name() != keep.String() {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
}

// install downloads the release if that has not happened, installs it and
// stops the daemon, which the shell takes as its cue to end; the new
// version starts as this one has ended (internal/update).
func (u *updater) install(ctx context.Context) error {
	st := u.status()
	switch {
	case !st.Checks || st.Latest == "":
		return errors.New("there is no update to install")
	case !st.CanInstall:
		return fmt.Errorf("this kalka cannot install updates itself: download %s from %s", st.Latest, st.NotesURL)
	}
	if st.State != UpdateReady {
		if !u.begin() {
			return errors.New("an update is downloading; try again in a moment")
		}
		err := u.fetch(ctx)
		u.end()
		if err != nil {
			return err
		}
	}
	u.mu.Lock()
	file := u.file
	u.mu.Unlock()
	if err := u.o.Apply(u.target, file); err != nil {
		return fmt.Errorf("install: %w", err)
	}
	go u.d.Shutdown()
	return nil
}

// UpdateStatus reports the update state.
func (d *Daemon) UpdateStatus() UpdateStatus {
	if d.upd == nil {
		return UpdateStatus{State: UpdateNone}
	}
	return d.upd.status()
}

func (d *Daemon) autoUpdateOn() bool {
	d.set.mu.Lock()
	defer d.set.mu.Unlock()
	return on(d.set.st.AutoUpdate)
}

// updateNotice raises the one notice per version, when it is ready.
func (d *Daemon) updateNotice(version string) {
	d.set.mu.Lock()
	st := d.set.st
	if st.UpdateNoticed == version {
		d.set.mu.Unlock()
		return
	}
	st.UpdateNoticed = version
	err := saveState(d.dir, st)
	if err == nil {
		d.set.st = st
	}
	d.set.mu.Unlock()
	if err != nil || !d.notificationsOn() {
		return
	}
	d.Bus().Publish(core.BusEvent{Type: "notice", Data: core.Notice{Kind: core.NoticeUpdate, Title: "kalka " + version + " is ready",
		Message: "Restart to update, in Settings or from the tray menu"}})
}

func (d *Daemon) mountUpdates(mux *http.ServeMux) {
	need := func(w http.ResponseWriter) *updater {
		if d.upd == nil {
			fail(w, http.StatusNotImplemented, errors.New("this kalka does not check for updates"))
		}
		return d.upd
	}
	mux.HandleFunc("GET /api/update", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, d.UpdateStatus()) })
	mux.HandleFunc("POST /api/update/check", func(w http.ResponseWriter, r *http.Request) {
		u := need(w)
		if u == nil {
			return
		}
		if err := u.check(r.Context()); err != nil {
			fail(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, u.status())
	})
	mux.HandleFunc("POST /api/update/install", func(w http.ResponseWriter, r *http.Request) {
		u := need(w)
		if u == nil {
			return
		}
		if err := u.install(r.Context()); err != nil {
			fail(w, http.StatusConflict, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"restarting": true})
	})
}
