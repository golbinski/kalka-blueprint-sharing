package core

// The test world of the end-to-end scenarios (spec 16.1): several nodes in
// one process, each with its own config directory and folder, over the
// in-process rendezvous and real WebRTC on loopback.

import (
	"context"
	"io"
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
	"github.com/golbinski/kalka/internal/rendezvous"
)

type world struct {
	t     *testing.T
	url   string
	srv   *rendezvous.Server
	ticks atomic.Int64
}

func newWorld(t *testing.T) *world {
	t.Helper()
	srv := &rendezvous.Server{ExpireTimeout: time.Second}
	mux := http.NewServeMux()
	srv.Mount(mux, "/")
	hs := httptest.NewServer(mux)
	t.Cleanup(func() { hs.Close(); srv.Close() })
	return &world{t: t, url: hs.URL, srv: srv}
}

// now is a shared clock: every call is one second later, so entries made
// in test order sort in test order.
func (w *world) now() time.Time {
	return time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC).Add(time.Duration(w.ticks.Add(1)) * time.Second)
}

type node struct {
	w        *world
	email    string
	cfgDir   string
	folder   string
	d        *Node
	stop     func()
	autoRepl bool // run the replication loop (otherwise rounds run only when a test asks)

	// moved maps a file name a test wrote to where publishing moved it
	// (out of Personal into the team's folder, spec 14).
	moved map[string]string

	taps *tapSwitch // routes session channels to the current tap
}

func (w *world) node(email string) *node {
	w.t.Helper()
	root := w.t.TempDir()
	n := &node{w: w, email: email, cfgDir: filepath.Join(root, "config"), folder: filepath.Join(root, "blueprints")}
	os.MkdirAll(n.folder, 0o755)
	n.start()
	return n
}

func (n *node) start() {
	t := n.w.t
	t.Helper()
	d, err := New(Options{
		Config:    &config.Config{Email: n.email, Rendezvous: n.w.url, Folder: n.folder, UIPort: 1},
		ConfigDir: n.cfgDir, Now: n.w.now, Loopback: true,
		manualReplication: !n.autoRepl, Log: testLog(n.email),
	})
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

func (n *node) path(name string) string {
	if r, ok := n.moved[name]; ok {
		name = r
	}
	return filepath.Join(n.folder, name)
}

// follow records that publishing name moved it to path.
func (n *node) follow(name, path string) {
	if path == "" || path == n.path(name) {
		return
	}
	r, err := filepath.Rel(n.folder, path)
	if err != nil {
		n.w.t.Fatal(err)
	}
	if n.moved == nil {
		n.moved = map[string]string{}
	}
	n.moved[name] = r
}

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

// comment adds a signed entry the way `kalka comment` does.
func (n *node) comment(name, anchor, body string) *bp.Entry {
	n.w.t.Helper()
	d := n.doc(name)
	e, err := bp.NewEntry(bp.KindComment, n.email, n.w.now(), anchor, d.Rev(), body)
	if err != nil {
		n.w.t.Fatal(err)
	}
	e.Sign(d.KalkaID(), n.d.signer())
	if err := d.AddEntry(e); err != nil {
		n.w.t.Fatal(err)
	}
	n.write(name, string(d.Bytes()))
	return e
}

// only returns the single blueprint file in the node's folder.
func (n *node) only() string {
	n.w.t.Helper()
	var names []string
	for _, i := range n.d.Store.List() {
		names = append(names, i.Rel)
	}
	if len(names) != 1 {
		n.w.t.Fatalf("%s has %v", n.email, names)
	}
	return names[0]
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

func entries(d *bp.Document) []*bp.Entry {
	var out []*bp.Entry
	for _, it := range d.Items() {
		if it.Entry != nil {
			out = append(out, it.Entry)
		}
	}
	return out
}

func problemCodes(d *bp.Document) map[string]int {
	m := map[string]int{}
	for _, p := range d.Problems() {
		m[p.Code]++
	}
	return m
}

func equalFiles(t *testing.T, name string, nodes ...*node) {
	t.Helper()
	first := nodes[0].read(name)
	for _, n := range nodes[1:] {
		if got := n.read(name); bp.Normalize(got) != bp.Normalize(first) {
			t.Fatalf("%s and %s differ:\n%s\n----\n%s", nodes[0].email, n.email, first, got)
		}
	}
}

func testLog(email string) io.Writer {
	if os.Getenv("KALKA_TEST_LOG") == "" {
		return nil
	}
	return prefixWriter{email + ": "}
}

type prefixWriter struct{ p string }

func (w prefixWriter) Write(b []byte) (int, error) {
	os.Stderr.WriteString(w.p + string(b))
	return len(b), nil
}
