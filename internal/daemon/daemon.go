// Package daemon is the process around the core (spec 4): it hosts one
// core.Node, serves the local API and the web UI on loopback (13), and
// shows notices when no page is open. All networking and replication live
// in the core.
package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/lockfile"
	"github.com/golbinski/kalka/internal/name"
)

// Options configure a daemon: the core's options plus the local API.
type Options struct {
	core.Options
	// APIAddr is where the local API listens; empty means
	// 127.0.0.1:<ui_port>. "-" disables the API.
	APIAddr string
	// Listener, if set, is used instead of listening on APIAddr (the
	// setup server hands over its port).
	Listener net.Listener
	// Token, if set, is the API token; otherwise a random one is made.
	Token string
	// OpenUI, if set, opens the UI's URL in a browser.
	OpenUI func(url string)
	// NoUI serves the API but not the web UI (`--no-ui`, spec 13). It
	// also turns notifications off: there is no page for them to open.
	NoUI bool
	// OpenExternal, if set, opens an http or https link in the system
	// browser (POST /api/open-external).
	OpenExternal func(url string) error
	// OpenFolder, if set, shows a folder in the system's file manager
	// (POST /api/open-folder).
	OpenFolder func(dir string) error
	// Notify, if set, shows a notice as an OS notification; it is called
	// only while no UI page is connected, and must not block for long.
	Notify func(core.Notice)
	// Autostart, if set, lets the settings change the start-at-login
	// entry (SystemAutostart in the real binary; nil in tests).
	Autostart Autostart
	// Lock, if set, is the single-daemon lock already taken by the caller
	// (before the setup form). Otherwise New takes it, before the core
	// reads anything (spec 4.1). Run releases it when it returns.
	Lock *lockfile.Lock
	// About is the version line of Settings (GET /api/about).
	About About
}

// About is what `kalka version` prints, for the page.
type About struct {
	Version        string `json:"version"`
	WireProtocol   int    `json:"wire_protocol"`
	PluginProtocol int    `json:"plugin_protocol"`
	Reference      string `json:"reference"`
}

// LockFile is the single-daemon lock in the config directory (spec 4.1).
const LockFile = "daemon.lock"

// AcquireLock takes the single-daemon lock of a config directory without
// waiting. A running daemon makes it fail with a *lockfile.Held error that
// names its PID (errors.Is(err, lockfile.ErrHeld)).
func AcquireLock(configDir string) (*lockfile.Lock, error) {
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return nil, err
	}
	return lockfile.Acquire(filepath.Join(configDir, LockFile))
}

// Daemon is one running kalka: the core node and its local API.
type Daemon struct {
	*core.Node
	opts    Options
	dir     string
	token   string
	apiAddr string

	mu       sync.Mutex
	shutdown context.CancelFunc
	set      settings
}

// New takes the lock and prepares a daemon around a new core node. With
// another daemon running on the config directory it fails with a
// *lockfile.Held error.
func New(opts Options) (*Daemon, error) {
	if opts.Lock == nil {
		l, err := AcquireLock(opts.ConfigDir)
		if err != nil {
			return nil, err
		}
		opts.Lock = l
	}
	if opts.NoUI {
		opts.Notify, opts.OpenUI = nil, nil
	}
	st, err := LoadState(opts.ConfigDir)
	if err != nil {
		opts.Lock.Release()
		return nil, err
	}
	opts.Options.Paused = st.Paused
	n, err := core.New(opts.Options)
	if err != nil {
		opts.Lock.Release()
		return nil, err
	}
	n.SetNotifications(on(st.Notifications))
	token := opts.Token
	if token == "" {
		tok := make([]byte, 32)
		_, _ = rand.Read(tok)
		token = hex.EncodeToString(tok)
	}
	d := &Daemon{Node: n, opts: opts, dir: opts.ConfigDir, token: token}
	d.set.st = st
	n.SetSeen(d.seen)
	return d, nil
}

// Validate checks the three required settings (spec 5).
var Validate = core.Validate

// Run serves the local API and runs the node until ctx
// ends or Shutdown is called. Background work such as a running
// replication round finishes before it returns.
func (d *Daemon) Run(ctx context.Context) error {
	defer d.opts.Lock.Release()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.mu.Lock()
	d.shutdown = cancel
	d.mu.Unlock()
	ln := d.opts.Listener
	if ln != nil || d.opts.APIAddr != "-" {
		addr := d.opts.APIAddr
		if addr == "" {
			addr = fmt.Sprintf("127.0.0.1:%d", d.opts.Config.UIPort)
		}
		var err error
		if ln == nil {
			if ln, err = net.Listen("tcp", addr); err != nil {
				return fmt.Errorf("local API on %s: %w (is %s already running?)", addr, err, name.Binary)
			}
		}
		d.apiAddr = ln.Addr().String()
		if err := d.writeDaemonFile(); err != nil {
			ln.Close()
			return err
		}
		defer d.removeDaemonFile()
		// Requests' contexts end when the server shuts down, so event
		// streams close at once and the graceful shutdown below is quick.
		reqCtx, endRequests := context.WithCancel(context.Background())
		hs := &http.Server{Handler: d.Handler(), ReadHeaderTimeout: 10 * time.Second,
			BaseContext: func(net.Listener) context.Context { return reqCtx }}
		hs.RegisterOnShutdown(endRequests)
		go func() { _ = hs.Serve(ln) }()
		defer func() {
			// Let answers in flight (POST /api/shutdown's among them) finish,
			// for at most a second.
			sctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if hs.Shutdown(sctx) != nil {
				_ = hs.Close()
			}
		}()
	}
	pctx, stop := context.WithCancel(ctx)
	defer stop()
	// Subscribe before anything can publish: a paused start raises its
	// notice as soon as the node is ready.
	if d.opts.Notify != nil {
		events, stopW := d.Bus().Watch()
		go d.presentNotices(pctx.Done(), events, stopW)
	}
	if d.Paused() {
		go func() { // one notice for a start in the paused state (spec 22.3)
			select {
			case <-d.Ready():
			case <-pctx.Done():
				return
			}
			if d.notificationsOn() {
				d.Bus().Publish(core.BusEvent{Type: "notice", Data: core.Notice{Kind: "paused", Title: "kalka is paused",
					Message: "kalka started paused: it does not sync until you resume it"}})
			}
		}()
	}
	return d.Node.Run(ctx)
}

// Shutdown asks Run to stop; it returns at once.
func (d *Daemon) Shutdown() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.shutdown != nil {
		d.shutdown()
	}
}

// URL is the UI address with the token, as `kalka open` uses it.
func (d *Daemon) URL() string { return "http://" + d.apiAddr + "/?token=" + d.token }

// APIAddr is the address the local API listens on, once Run started it.
func (d *Daemon) APIAddr() string { return d.apiAddr }

// Token is the per-start API token.
func (d *Daemon) Token() string { return d.token }
