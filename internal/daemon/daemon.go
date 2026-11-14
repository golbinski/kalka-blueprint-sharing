// Package daemon is the process around the core (spec 4): it hosts one
// core.Node, serves the local API and the web UI on loopback (13), and
// presents prompts when no page is open. All networking and sync live in
// the core.
package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/golbinski/kalka/internal/core"
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
	// Popup, if set, asks the user whether to open the UI; see popups.go.
	// It returns true for "open", false for "later", and must close and
	// return when ctx ends. An error falls back to OpenUI.
	Popup func(ctx context.Context, title, message string) (bool, error)
}

// Daemon is one running kalka: the core node and its local API.
type Daemon struct {
	*core.Node
	opts    Options
	dir     string
	token   string
	apiAddr string
}

// New prepares a daemon around a new core node.
func New(opts Options) (*Daemon, error) {
	n, err := core.New(opts.Options)
	if err != nil {
		return nil, err
	}
	token := opts.Token
	if token == "" {
		tok := make([]byte, 32)
		_, _ = rand.Read(tok)
		token = hex.EncodeToString(tok)
	}
	return &Daemon{Node: n, opts: opts, dir: opts.ConfigDir, token: token}, nil
}

// Validate checks the three required settings (spec 5).
var Validate = core.Validate

// Run serves the local API and runs the node until ctx ends.
func (d *Daemon) Run(ctx context.Context) error {
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
		hs := &http.Server{Handler: d.Handler(), ReadHeaderTimeout: 10 * time.Second}
		go func() { _ = hs.Serve(ln) }()
		defer hs.Close()
	}
	pctx, stop := context.WithCancel(ctx)
	defer stop()
	go d.present(pctx)
	return d.Node.Run(ctx)
}

// present brings the UI up for a prompt nobody can see (popups.go).
func (d *Daemon) present(ctx context.Context) {
	prompts, stopP := d.Prompts.Subscribe("")
	defer stopP()
	pop := newPopups(d)
	defer pop.stop()
	for {
		select {
		case <-ctx.Done():
			return
		case n := <-prompts:
			if n.Closed {
				pop.closed(n.Prompt)
				continue
			}
			pop.opened(ctx, n.Prompt)
		case r := <-pop.results:
			pop.finished(ctx, r)
		}
	}
}

// URL is the UI address with the token, as `kalka open` uses it.
func (d *Daemon) URL() string { return "http://" + d.apiAddr + "/?token=" + d.token }

// APIAddr is the address the local API listens on, once Run started it.
func (d *Daemon) APIAddr() string { return d.apiAddr }

// Token is the per-start API token.
func (d *Daemon) Token() string { return d.token }
