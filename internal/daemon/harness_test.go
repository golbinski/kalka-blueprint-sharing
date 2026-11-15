package daemon

// A small copy of the core's scenario harness: daemons with their local
// API, over an in-process rendezvous.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/rendezvous"
)

type world struct {
	t     *testing.T
	url   string
	ticks atomic.Int64
}

func newWorld(t *testing.T) *world {
	t.Helper()
	srv := &rendezvous.Server{ExpireTimeout: time.Second}
	mux := http.NewServeMux()
	srv.Mount(mux, "/")
	hs := httptest.NewServer(mux)
	t.Cleanup(func() { hs.Close(); srv.Close() })
	return &world{t: t, url: hs.URL}
}

// now is a shared clock: every call is one second later, so entries made
// in test order sort in test order.
func (w *world) now() time.Time {
	return time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC).Add(time.Duration(w.ticks.Add(1)) * time.Second)
}

type node struct {
	w      *world
	email  string
	cfgDir string
	folder string
	d      *Daemon
	stop   func()

	opts []func(*Options)
}

func (w *world) node(email string, opts ...func(*Options)) *node {
	w.t.Helper()
	root := w.t.TempDir()
	n := &node{w: w, email: email, opts: opts, cfgDir: filepath.Join(root, "config"), folder: filepath.Join(root, "blueprints")}
	os.MkdirAll(n.folder, 0o755)
	n.start()
	return n
}

func (n *node) start() {
	t := n.w.t
	t.Helper()
	o := Options{Options: core.Options{
		Config:    &config.Config{Email: n.email, Rendezvous: n.w.url, Folder: n.folder, UIPort: 1},
		ConfigDir: n.cfgDir, Now: n.w.now, Loopback: true,
	}, APIAddr: "127.0.0.1:0"}
	for _, f := range n.opts {
		f(&o)
	}
	d, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = d.Run(ctx); close(done) }()
	select {
	case <-d.Ready():
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not connect to the rendezvous", n.email)
	}
	n.d = d
	var once sync.Once
	n.stop = func() { once.Do(func() { cancel(); <-done }) }
	t.Cleanup(n.stop)
}

func (n *node) path(name string) string { return filepath.Join(n.folder, name) }

func (n *node) write(name, content string) {
	n.w.t.Helper()
	if err := os.WriteFile(n.path(name), []byte(content), 0o644); err != nil {
		n.w.t.Fatal(err)
	}
}

func (n *node) read(name string) string {
	n.w.t.Helper()
	b, err := os.ReadFile(n.path(name))
	if err != nil {
		n.w.t.Fatal(err)
	}
	return string(b)
}

func (n *node) doc(name string) *bp.Document {
	n.w.t.Helper()
	d, err := bp.Parse([]byte(n.read(name)))
	if err != nil {
		n.w.t.Fatal(err)
	}
	return d
}

// comment adds an unsigned entry, as typed by hand.
func (n *node) comment(name, anchor, body string) *bp.Entry {
	n.w.t.Helper()
	d := n.doc(name)
	e, err := bp.NewEntry(bp.KindComment, n.email, n.w.now(), anchor, d.Rev(), body)
	if err != nil {
		n.w.t.Fatal(err)
	}
	if err := d.AddEntry(e); err != nil {
		n.w.t.Fatal(err)
	}
	n.write(name, string(d.Bytes()))
	return e
}

const plan = `# Import pipeline refactor

## Context

The importer is slow.

## Plan

1. Stream rows.
2. Batch inserts.

## Rollout

Behind a flag.
`
