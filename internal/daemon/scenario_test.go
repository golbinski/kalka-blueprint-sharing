package daemon

// End-to-end scenarios of spec 16.1: several daemons in one process, each
// with its own config directory and folder, over the in-process PeerJS
// rendezvous and real WebRTC on loopback. Prompts are answered by scripts.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/identity"
	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/rendezvous"
	"github.com/golbinski/kalka/internal/store"
	"github.com/golbinski/kalka/internal/syncproto"
	"github.com/golbinski/kalka/internal/transport"
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
	noKeyID bool // do not register the key id (keyid_test.go)

	mu      sync.Mutex
	answers map[string]bool // by prompt kind; missing means yes
	seen    []syncproto.Prompt
}

func (w *world) node(email string) *node {
	w.t.Helper()
	root := w.t.TempDir()
	n := &node{w: w, email: email, cfgDir: filepath.Join(root, "config"), folder: filepath.Join(root, "blueprints"), answers: map[string]bool{}}
	os.MkdirAll(n.folder, 0o755)
	n.start()
	return n
}

func (n *node) start() {
	t := n.w.t
	t.Helper()
	d, err := New(Options{
		Config:    &config.Config{Email: n.email, Rendezvous: n.w.url, Folder: n.folder, UIPort: 1},
		ConfigDir: n.cfgDir, Now: n.w.now, Loopback: true, APIAddr: "127.0.0.1:0",
		Prompter: n.prompt, noKeyID: n.noKeyID,
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
	return n.d.Sync(ctx, SyncRequest{Path: n.path(name), To: to.email, Intent: intent}, "", nil)
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

// review sets up alice's plan reviewed by the given nodes.
func review(t *testing.T, alice *node, reviewers ...*node) {
	t.Helper()
	alice.write("plan.md", plan)
	for _, r := range reviewers {
		alice.mustSync("plan.md", r, IntentSync)
		if r.only() != "plan.md" {
			t.Fatalf("%s stored the plan as %s", r.email, r.only())
		}
	}
}

func TestScenario1ReviewRoundTrip(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.node("alice@corp.com"), w.node("bob@corp.com")
	alice.write("plan.md", plan)

	r := alice.mustSync("plan.md", bob, IntentSync)
	if r.Status != "unchanged" {
		t.Fatalf("alice's outcome %q", r.Status)
	}
	// First contact: both confirmed the same code, and both pinned verified.
	ac, bc := alice.prompts(syncproto.PromptVerify), bob.prompts(syncproto.PromptVerify)
	if len(ac) != 1 || len(bc) != 1 || ac[0].Code != bc[0].Code || ac[0].Code == "" {
		t.Fatalf("verify prompts %+v %+v", ac, bc)
	}
	if p, _ := alice.d.Peers.Get(bob.email); !p.Verified {
		t.Fatal("alice did not mark bob verified")
	}
	if p, _ := bob.d.Peers.Get(alice.email); !p.Verified {
		t.Fatal("bob did not mark alice verified")
	}
	if n := bob.prompts(syncproto.PromptNew); len(n) != 1 || n[0].Title != "Import pipeline refactor" {
		t.Fatalf("accept prompt %+v", n)
	}
	d := alice.doc("plan.md")
	if d.KalkaID() == "" || d.Owner() != alice.email {
		t.Fatal("first send did not prepare the file")
	}
	body := d.Body()

	bob.comment("plan.md", "plan", "Batch size?")
	bob.comment("plan.md", "context", "How slow?")
	bob.mustSync("plan.md", alice, IntentSync)

	got := alice.doc("plan.md")
	if len(entries(got)) != 2 || got.Body() != body {
		t.Fatalf("alice's file:\n%s", alice.read("plan.md"))
	}
	equalFiles(t, "plan.md", alice, bob)
	// The second sync needs no code: both are verified.
	if len(alice.prompts(syncproto.PromptVerify)) != 1 {
		t.Fatal("asked for the code again")
	}
}

func TestScenario2ParallelReviewers(t *testing.T) {
	w := newWorld(t)
	alice, bob, carol := w.node("alice@corp.com"), w.node("bob@corp.com"), w.node("carol@corp.com")
	review(t, alice, bob, carol)
	eb := bob.comment("plan.md", "plan", "from bob")
	ec := carol.comment("plan.md", "plan", "from carol")
	carol.mustSync("plan.md", alice, IntentSync)
	bob.mustSync("plan.md", alice, IntentSync)

	es := entries(alice.doc("plan.md"))
	if len(es) != 2 || es[0].ID != eb.ID || es[1].ID != ec.ID {
		t.Fatalf("alice's log not in timestamp order:\n%s", alice.read("plan.md"))
	}
	alice.mustSync("plan.md", bob, IntentSync)
	alice.mustSync("plan.md", carol, IntentSync)
	equalFiles(t, "plan.md", alice, bob, carol)
}

func TestScenario3OwnerRevision(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.node("alice@corp.com"), w.node("bob@corp.com")
	review(t, alice, bob)
	bob.comment("plan.md", "rollout", "Which flag?")
	bob.comment("plan.md", "plan", "Fine")
	bob.mustSync("plan.md", alice, IntentSync)

	// Alice rewrites the plan and drops the Rollout heading.
	revised := strings.Replace(alice.read("plan.md"), "2. Batch inserts.", "2. Batch inserts of 500 rows.", 1)
	revised = strings.Replace(revised, "## Rollout\n\nBehind a flag.\n\n", "", 1)
	if revised == alice.read("plan.md") || strings.Contains(revised, "Rollout") {
		t.Fatal("test edit did not apply")
	}
	alice.write("plan.md", revised)
	alice.mustSync("plan.md", bob, IntentSync)

	d := bob.doc("plan.md")
	if !strings.Contains(d.Body(), "500 rows") || d.HasSection("rollout") {
		t.Fatalf("bob did not take the new body:\n%s", bob.read("plan.md"))
	}
	codes := problemCodes(d)
	if codes["orphaned"] != 1 || codes["outdated"] != 2 {
		t.Fatalf("problem codes %v", codes)
	}
	equalFiles(t, "plan.md", alice, bob)
}

func TestScenario4NonOwnerBodyEdit(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.node("alice@corp.com"), w.node("bob@corp.com")
	review(t, alice, bob)
	e := bob.comment("plan.md", "plan", "I tweaked step 1")
	bob.write("plan.md", strings.Replace(bob.read("plan.md"), "1. Stream rows.", "1. Stream rows in parallel.", 1))

	r := bob.mustSync("plan.md", alice, IntentSync)
	if len(bob.prompts(syncproto.PromptConverge)) != 1 || r.Conflict == "" {
		t.Fatalf("no convergence: %+v", r)
	}
	conflict, err := os.ReadFile(r.Conflict)
	if err != nil || !strings.Contains(string(conflict), "in parallel") {
		t.Fatalf("conflict copy: %v\n%s", err, conflict)
	}
	d := bob.doc("plan.md")
	if strings.Contains(d.Body(), "in parallel") {
		t.Fatal("bob kept his edit")
	}
	if es := entries(alice.doc("plan.md")); len(es) != 1 || es[0].ID != e.ID {
		t.Fatal("bob's entry did not survive")
	}
	equalFiles(t, "plan.md", alice, bob)
	// The conflict copy is ignored by the store.
	if bob.only() != "plan.md" {
		t.Fatal("conflict copy indexed")
	}
}

func TestScenario5Handoff(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.node("alice@corp.com"), w.node("bob@corp.com")
	review(t, alice, bob)

	// Declined: alice's file is untouched.
	bob.answer(syncproto.PromptHandoff, false)
	before := alice.read("plan.md")
	_, err := alice.sync("plan.md", bob, IntentHandoff)
	var rej *syncproto.Rejected
	if !errors.As(err, &rej) || rej.Reason != merge.Declined || alice.read("plan.md") != before {
		t.Fatalf("declined handoff: %v", err)
	}

	bob.answer(syncproto.PromptHandoff, true)
	alice.mustSync("plan.md", bob, IntentHandoff)
	for _, n := range []*node{alice, bob} {
		d := n.doc("plan.md")
		es := entries(d)
		if d.Owner() != bob.email || len(es) != 1 || es[0].Kind != bp.KindHandoff || es[0].Sig == "" {
			t.Fatalf("%s after handoff:\n%s", n.email, n.read("plan.md"))
		}
	}
	equalFiles(t, "plan.md", alice, bob)

	// Bob can now revise.
	bob.write("plan.md", strings.Replace(bob.read("plan.md"), "Behind a flag.", "Behind flag import_v2.", 1))
	bob.mustSync("plan.md", alice, IntentSync)
	if !strings.Contains(alice.read("plan.md"), "import_v2") {
		t.Fatal("bob's revision did not reach alice")
	}
	// Alice cannot.
	alice.answer(syncproto.PromptConverge, false)
	alice.write("plan.md", strings.Replace(alice.read("plan.md"), "import_v2", "import_v3", 1))
	_, err = alice.sync("plan.md", bob, IntentSync)
	if !errors.As(err, &rej) || rej.Reason != merge.NotOwner || strings.Contains(bob.read("plan.md"), "import_v3") {
		t.Fatalf("alice's revision after handoff: %v", err)
	}
	// Handoff by a non-owner is refused locally.
	if _, err := alice.sync("plan.md", bob, IntentHandoff); err == nil || !strings.Contains(err.Error(), "only the owner") {
		t.Fatalf("handoff by non-owner: %v", err)
	}
}

// dropAfterWrite closes the connection right after the responder writes,
// so the initiator never sees the reply.
type dropAfterWrite struct {
	syncproto.Store
	conn *transport.Conn
	once *atomic.Bool
}

func (s dropAfterWrite) Update(f *store.File, d *bp.Document) error {
	err := s.Store.Update(f, d)
	if s.once.CompareAndSwap(false, true) {
		s.conn.Close()
	}
	return err
}

func TestScenario6LostAcknowledgement(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.node("alice@corp.com"), w.node("bob@corp.com")
	review(t, alice, bob)
	var fired atomic.Bool
	bob.d.wrapStore = func(s syncproto.Store, c *transport.Conn) syncproto.Store {
		return dropAfterWrite{Store: s, conn: c, once: &fired}
	}
	before := alice.read("plan.md")
	if _, err := alice.sync("plan.md", bob, IntentHandoff); err == nil {
		t.Fatal("handoff succeeded despite the dropped connection")
	}
	if !fired.Load() || bob.doc("plan.md").Owner() != bob.email {
		t.Fatal("bob did not write before the drop")
	}
	if alice.read("plan.md") != before {
		t.Fatal("alice wrote the candidate without the acknowledgement")
	}
	bobAfter := bob.read("plan.md")
	prompts := len(bob.prompts(syncproto.PromptHandoff))

	r := alice.mustSync("plan.md", bob, IntentHandoff)
	if bob.read("plan.md") != bobAfter {
		t.Fatalf("retry changed bob's file:\n%s\n----\n%s", bobAfter, bob.read("plan.md"))
	}
	if len(bob.prompts(syncproto.PromptHandoff)) != prompts {
		t.Fatal("retry prompted bob again")
	}
	if r.Status != "updated" || len(entries(alice.doc("plan.md"))) != 1 {
		t.Fatalf("alice after retry: %+v\n%s", r, alice.read("plan.md"))
	}
	equalFiles(t, "plan.md", alice, bob)
}

func TestScenario7StaleReviewerAfterHandoff(t *testing.T) {
	w := newWorld(t)
	alice, bob, carol := w.node("alice@corp.com"), w.node("bob@corp.com"), w.node("carol@corp.com")
	review(t, alice, bob, carol)
	ec := carol.comment("plan.md", "plan", "carol's note")
	alice.mustSync("plan.md", bob, IntentHandoff)
	eb := bob.comment("plan.md", "plan", "bob's note")
	bob.write("plan.md", strings.Replace(bob.read("plan.md"), "Behind a flag.", "Behind flag import_v2.", 1))

	carol.mustSync("plan.md", alice, IntentSync)
	if carol.doc("plan.md").Owner() != bob.email {
		t.Fatalf("carol did not converge on alice's copy:\n%s", carol.read("plan.md"))
	}
	carol.mustSync("plan.md", bob, IntentSync)
	d := carol.doc("plan.md")
	ids := map[string]bool{}
	for _, e := range entries(d) {
		ids[e.ID] = true
	}
	if d.Owner() != bob.email || !ids[ec.ID] || !ids[eb.ID] || len(ids) != 3 || !strings.Contains(d.Body(), "import_v2") {
		t.Fatalf("carol's file:\n%s", carol.read("plan.md"))
	}
	equalFiles(t, "plan.md", bob, carol)
}

func TestScenario8KeyChange(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.node("alice@corp.com"), w.node("bob@corp.com")
	review(t, alice, bob)

	bob.stop()
	if err := os.Remove(filepath.Join(bob.cfgDir, identity.KeyFile)); err != nil {
		t.Fatal(err)
	}
	bob.start()

	_, err := alice.sync("plan.md", bob, IntentSync)
	var kc *identity.KeyChangedError
	if !errors.As(err, &kc) || !strings.Contains(err.Error(), "different identity key") {
		t.Fatalf("alice synced with a changed key: %v", err)
	}
	if p, _ := alice.d.Peers.Get(bob.email); p.KeyChanged == nil {
		t.Fatal("the key change is not recorded for the UI")
	}
	// Bob's side learns why.
	_, err = bob.sync("plan.md", alice, IntentSync)
	if err == nil || !strings.Contains(err.Error(), "different key pinned") {
		t.Fatalf("bob's view: %v", err)
	}
	// Removing the pin and verifying again restores syncing.
	if _, err := alice.d.Peers.Forget(bob.email); err != nil {
		t.Fatal(err)
	}
	alice.mustSync("plan.md", bob, IntentSync)
}

func TestScenario9EditorReformat(t *testing.T) {
	w := newWorld(t)
	alice, bob := w.node("alice@corp.com"), w.node("bob@corp.com")
	review(t, alice, bob)
	c := bob.read("plan.md")
	c = strings.ReplaceAll(c, "\n", "\r\n")
	bob.write("plan.md", c)
	if !strings.Contains(alice.read("plan.md"), "kalka_id") {
		t.Fatal("setup")
	}
	r := bob.mustSync("plan.md", alice, IntentSync)
	if r.Status != "unchanged" {
		t.Fatalf("reformat was not the same plan: %+v", r)
	}
	// Trailing whitespace stripped too.
	alice.write("plan.md", strings.Replace(alice.read("plan.md"), "slow.", "slow.   ", 1))
	alice.mustSync("plan.md", bob, IntentSync)
}

func TestScenario10OfflinePeer(t *testing.T) {
	w := newWorld(t)
	alice := w.node("alice@corp.com")
	alice.write("plan.md", plan)
	before := alice.read("plan.md")
	t0 := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := alice.d.Sync(ctx, SyncRequest{Path: alice.path("plan.md"), To: "dave@corp.com"}, "", nil)
	var off *ErrOffline
	if !errors.As(err, &off) || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("offline peer: %v", err)
	}
	if d := time.Since(t0); d > 5*time.Second {
		t.Fatalf("took %v", d)
	}
	// The first send prepared the file, nothing else.
	if after := alice.read("plan.md"); bp.Normalize(after) == bp.Normalize(before) || !strings.Contains(after, "kalka_id") {
		t.Fatal("file not prepared")
	}
	if p := alice.d.Probe(ctx, "dave@corp.com"); p != transport.Offline {
		t.Fatalf("probe: %s", p)
	}
}
