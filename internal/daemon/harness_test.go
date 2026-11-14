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
	"github.com/golbinski/kalka/internal/syncproto"
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

	opts    []func(*Options)
	mu      sync.Mutex
	answers map[string]bool // by prompt kind; missing means yes
	seen    []syncproto.Prompt
}

func (w *world) node(email string, opts ...func(*Options)) *node {
	w.t.Helper()
	root := w.t.TempDir()
	n := &node{w: w, email: email, opts: opts, cfgDir: filepath.Join(root, "config"), folder: filepath.Join(root, "blueprints"), answers: map[string]bool{}}
	os.MkdirAll(n.folder, 0o755)
	n.start()
	return n
}

func (n *node) start() {
	t := n.w.t
	t.Helper()
	o := Options{Options: core.Options{
		Config:    &config.Config{Email: n.email, Rendezvous: n.w.url, Folder: n.folder, UIPort: 1},
		ConfigDir: n.cfgDir, Now: n.w.now, Loopback: true, Prompter: n.prompt,
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

func (n *node) prompt(_ context.Context, _ string, p syncproto.Prompt) (bool, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.seen = append(n.seen, p)
	ok, set := n.answers[p.Kind]
	return ok || !set, nil
}

func (n *node) answer(kind string, ok bool) {
	n.mu.Lock()
	n.answers[kind] = ok
	n.mu.Unlock()
}

func (n *node) prompts(kind string) []syncproto.Prompt {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []syncproto.Prompt
	for _, p := range n.seen {
		if p.Kind == kind {
			out = append(out, p)
		}
	}
	return out
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

// comment adds an entry the way `kalka comment` does.
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

func (n *node) sync(name string, to *node, intent string) (syncproto.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return n.d.Sync(ctx, core.SyncRequest{Path: n.path(name), To: to.email, Intent: intent}, "", nil)
}

func (n *node) mustSync(name string, to *node, intent string) syncproto.Result {
	n.w.t.Helper()
	r, err := n.sync(name, to, intent)
	if err != nil {
		n.w.t.Fatalf("%s syncs %s with %s: %v", n.email, name, to.email, err)
	}
	return r
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

func equalFiles(t *testing.T, name string, nodes ...*node) {
	t.Helper()
	first := nodes[0].read(name)
	for _, n := range nodes[1:] {
		if got := n.read(name); bp.Normalize(got) != bp.Normalize(first) {
			t.Fatalf("%s and %s differ:\n%s\n----\n%s", nodes[0].email, n.email, first, got)
		}
	}
}

// review sets up alice's plan reviewed by the given nodes.
func review(t *testing.T, alice *node, reviewers ...*node) {
	t.Helper()
	alice.write("plan.md", plan)
	for _, r := range reviewers {
		alice.mustSync("plan.md", r, core.IntentSync)
		if r.only() != "plan.md" {
			t.Fatalf("%s stored the plan as %s", r.email, r.only())
		}
	}
}
