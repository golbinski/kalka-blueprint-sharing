// Package daemon is the long-running kalka process (spec 4): it keeps the
// rendezvous connection, accepts and starts syncs, watches the folder,
// probes peer presence and serves the local API.
package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/identity"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/peerjs"
	"github.com/golbinski/kalka/internal/store"
	"github.com/golbinski/kalka/internal/syncproto"
	"github.com/golbinski/kalka/internal/transport"
)

// Timing.
var (
	// DialTimeout bounds opening the data channel to a peer.
	DialTimeout = 30 * time.Second
	// ProbeEvery is the minimum interval between probes of one peer.
	ProbeEvery = time.Minute
	// ProbeTimeout bounds one presence probe.
	ProbeTimeout = 8 * time.Second
	// ICERefresh is how often ICE servers (and TURN credentials) are fetched.
	ICERefresh = 10 * time.Minute
)

// Options configure a Daemon.
type Options struct {
	Config    *config.Config
	ConfigDir string
	// Now is the clock for entry timestamps; nil means time.Now.
	Now func() time.Time
	// Log receives one line per event; nil discards.
	Log io.Writer
	// Prompter, if set, answers prompts instead of the hub (tests).
	Prompter func(ctx context.Context, scope string, p syncproto.Prompt) (bool, error)
	// Loopback allows loopback ICE candidates and skips ICE discovery
	// (tests on one machine).
	Loopback bool
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
	// Plugins resolves references (spec 10); nil means none installed.
	Plugins Plugins
}

// Daemon is one running kalka.
type Daemon struct {
	cfg    *config.Config
	dir    string
	now    func() time.Time
	log    io.Writer
	opts   Options
	self   transport.Identity
	pub    ed25519.PublicKey
	peerID string
	srv    peerjs.Server
	cloud  bool

	Peers   *identity.Peers
	Store   *store.Store
	Prompts *Hub
	bus     *Bus
	refs    *refCache

	token string

	mu        sync.Mutex
	ep        *transport.Endpoint
	online    bool
	sigErr    string
	ice       []webrtc.ICEServer
	iceAt     time.Time
	presence  map[string]presence
	addresses map[string]bool // typed this session
	pending   map[string]candidate
	readyCh   chan struct{}
	apiAddr   string
	logMu     sync.Mutex

	// wrapStore, set by tests, replaces the store an incoming sync uses
	// (fault injection for spec 16.1 scenario 6).
	wrapStore func(syncproto.Store, *transport.Conn) syncproto.Store
}

type presence struct {
	State   transport.Presence `json:"state"`
	Checked time.Time          `json:"checked"`
}

// candidate is an unacknowledged handoff, kept so a retry offers the
// identical file (spec 7.4 step 4).
type candidate struct {
	base [32]byte // hash of the local file it was built from
	data []byte
}

// New prepares a daemon: identity key, peer store and folder index.
func New(opts Options) (*Daemon, error) {
	cfg := opts.Config
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	key, err := identity.LoadOrCreateKey(opts.ConfigDir)
	if err != nil {
		return nil, fmt.Errorf("identity key: %w", err)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	peers, err := identity.OpenPeers(opts.ConfigDir, now)
	if err != nil {
		return nil, err
	}
	st, err := store.Open(cfg.Folder)
	if err != nil {
		return nil, fmt.Errorf("blueprint folder: %w", err)
	}
	srv, _ := peerjs.ParseRendezvous(cfg.Rendezvous)
	token := opts.Token
	if token == "" {
		tok := make([]byte, 32)
		_, _ = rand.Read(tok)
		token = hex.EncodeToString(tok)
	}
	log := opts.Log
	if log == nil {
		log = io.Discard
	}
	return &Daemon{
		cfg: cfg, dir: opts.ConfigDir, now: now, log: log, opts: opts,
		self:   transport.Identity{Email: cfg.Email, Key: key},
		pub:    key.Public().(ed25519.PublicKey),
		peerID: transport.PeerID(cfg.Team, cfg.Email),
		srv:    srv, cloud: cfg.Rendezvous == config.PeerJSCloud,
		Peers: peers, Store: st, Prompts: newHub(), bus: newBus(), refs: newRefCache(opts.Plugins, now),
		token:     token,
		presence:  map[string]presence{},
		addresses: map[string]bool{},
		pending:   map[string]candidate{},
		readyCh:   make(chan struct{}),
	}, nil
}

// Validate checks the three required settings (spec 5).
func Validate(c *config.Config) error {
	if c == nil {
		return config.ErrMissing
	}
	if !bp.ValidEmail(c.Email) {
		return fmt.Errorf("email %q is not a valid address", c.Email)
	}
	if _, err := peerjs.ParseRendezvous(c.Rendezvous); err != nil {
		return err
	}
	if c.Folder == "" {
		return errors.New("folder is required")
	}
	return nil
}

// Email is the local user.
func (d *Daemon) Email() string { return d.self.Email }

// PublicKey is the local identity key.
func (d *Daemon) PublicKey() ed25519.PublicKey { return d.pub }

func (d *Daemon) logf(format string, a ...any) {
	d.logMu.Lock()
	defer d.logMu.Unlock()
	fmt.Fprintf(d.log, "%s %s\n", d.now().Format("15:04:05"), fmt.Sprintf(format, a...))
}

// Run serves until ctx ends.
func (d *Daemon) Run(ctx context.Context) error {
	d.refs.ctx = ctx // plugin calls end with the daemon
	ln := d.opts.Listener
	if ln != nil || d.opts.APIAddr != "-" {
		addr := d.opts.APIAddr
		if addr == "" {
			addr = fmt.Sprintf("127.0.0.1:%d", d.cfg.UIPort)
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
	go d.forward(ctx)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); _ = d.Store.Watch(ctx) }()
	go func() { defer wg.Done(); d.signalLoop(ctx) }()
	go func() { defer wg.Done(); d.probeLoop(ctx) }()
	<-ctx.Done()
	wg.Wait()
	return nil
}

// forward relays folder changes and prompts to the UI's event streams, and
// brings the UI up for a prompt nobody can see.
func (d *Daemon) forward(ctx context.Context) {
	folder, stopF := d.Store.Subscribe()
	defer stopF()
	prompts, stopP := d.Prompts.Subscribe("")
	defer stopP()
	pop := newPopups(d)
	defer pop.stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-folder:
			d.bus.Publish(BusEvent{Type: "folder", Data: e})
		case n := <-prompts:
			if n.Closed {
				d.bus.Publish(BusEvent{Type: "prompt-closed", Data: n.Prompt})
				pop.closed(n.Prompt)
				continue
			}
			d.bus.Publish(BusEvent{Type: "prompt", Data: n.Prompt})
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

// Ready is closed when the daemon first registers with the rendezvous.
func (d *Daemon) Ready() <-chan struct{} { return d.readyCh }

func (d *Daemon) iceServers() []webrtc.ICEServer {
	if d.opts.Loopback {
		return nil
	}
	d.mu.Lock()
	ice, at := d.ice, d.iceAt
	d.mu.Unlock()
	if ice != nil && time.Since(at) < ICERefresh {
		return ice
	}
	var configured []webrtc.ICEServer
	for _, s := range d.cfg.ICEServers {
		configured = append(configured, webrtc.ICEServer{URLs: s.URLs, Username: s.Username, Credential: s.Credential})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ice, _, _ = transport.ResolveICE(ctx, configured, d.srv, d.cloud)
	d.mu.Lock()
	d.ice, d.iceAt = ice, time.Now()
	d.mu.Unlock()
	return ice
}

// signalLoop keeps the rendezvous connection, reconnecting with backoff.
func (d *Daemon) signalLoop(ctx context.Context) {
	backoff := time.Second
	ready := false
	for ctx.Err() == nil {
		sig, err := peerjs.Dial(ctx, d.srv, d.peerID)
		if err != nil {
			d.setOnline(nil, err)
			wait := backoff
			if errors.Is(err, peerjs.ErrIDTaken) {
				wait = 30 * time.Second
			}
			backoff = min(backoff*2, 30*time.Second)
			select {
			case <-ctx.Done():
			case <-time.After(wait):
			}
			continue
		}
		backoff = time.Second
		d.iceServers()
		ep := transport.NewEndpoint(sig, transport.Options{ICE: d.iceServers, Loopback: d.opts.Loopback, RelayOnly: d.cfg.RelayOnly})
		d.setOnline(ep, nil)
		d.logf("connected to %s as %s", d.srv.URL, d.cfg.Email)
		if !ready {
			ready = true
			close(d.readyCh)
		}
		for {
			c, err := ep.Accept(ctx)
			if err != nil {
				break
			}
			go d.serve(c)
		}
		ep.Close()
		_ = sig.Close()
		err = sig.Err()
		if err == nil && ctx.Err() == nil {
			err = errors.New("rendezvous connection closed")
		}
		d.setOnline(nil, err)
	}
}

func (d *Daemon) setOnline(ep *transport.Endpoint, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ep, d.online = ep, ep != nil
	defer d.bus.Publish(BusEvent{Type: "status"})
	if err == nil {
		d.sigErr = ""
		return
	}
	if err.Error() != d.sigErr {
		d.logf("rendezvous: %v", err)
	}
	d.sigErr = err.Error()
}

func (d *Daemon) endpoint() (*transport.Endpoint, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ep == nil {
		msg := "not connected to the rendezvous server"
		if d.sigErr != "" {
			msg += ": " + d.sigErr
		}
		return nil, errors.New(msg)
	}
	return d.ep, nil
}

// keys looks up pinned keys, including the local user's own.
func (d *Daemon) keys(email string) (ed25519.PublicKey, bool) {
	if bp.SameEmail(email, d.self.Email) {
		return d.pub, true
	}
	return d.Peers.Key(email)
}

func (d *Daemon) session(ch syncproto.Channel, peer, scope string, progress func(string)) *syncproto.Session {
	ask := func(ctx context.Context, p syncproto.Prompt) (bool, error) {
		if d.opts.Prompter != nil {
			return d.opts.Prompter(ctx, scope, p)
		}
		return d.Prompts.Ask(ctx, scope, p)
	}
	return &syncproto.Session{Ch: ch, Self: d.self.Email, Peer: peer, Keys: d.keys, Store: d.Store, Ask: ask, Now: d.now, Progress: progress}
}

// confirm runs code verification for an authenticated peer.
func (d *Daemon) confirm(s *syncproto.Session, peer transport.Peer) error {
	p, _ := d.Peers.Get(peer.Email)
	code := identity.Code(d.pub, peer.Key)
	return s.Confirm(p.Verified, code, func() error { return d.Peers.SetVerified(peer.Email, peer.Key) })
}

// serve handles an incoming connection.
func (d *Daemon) serve(c *transport.Conn) {
	defer c.Close()
	peer, err := transport.Handshake(c, d.self, d.cfg.Team, d.Peers.Pin)
	if err != nil {
		d.refuse(c, err)
		d.logf("refused a connection: %v", err)
		return
	}
	d.markPresence(peer.Email, transport.Online)
	s := d.session(c, peer.Email, "", func(m string) { d.logf("%s: %s", peer.Email, m) })
	if d.wrapStore != nil {
		s.Store = d.wrapStore(s.Store, c)
	}
	if err := d.confirm(s, peer); err != nil {
		d.logf("sync from %s stopped: %v", peer.Email, err)
		return
	}
	results, err := s.Respond()
	d.bus.Publish(BusEvent{Type: "peers"})
	for _, r := range results {
		d.bus.Publish(BusEvent{Type: "sync", Data: r})
		msg := r.Status
		if r.Reason != "" {
			msg += " (" + string(r.Reason) + ")"
		}
		d.logf("sync from %s: %q %s", peer.Email, r.Title, msg)
	}
	if err != nil {
		d.logf("sync from %s: %v", peer.Email, err)
	}
}

// refuse tells the peer why the handshake failed, where that is safe to
// say (a changed key), then the caller closes.
func (d *Daemon) refuse(c *transport.Conn, err error) {
	var kc *identity.KeyChangedError
	detail := "handshake failed"
	if errors.As(err, &kc) {
		detail = fmt.Sprintf("%s has a different key pinned for you and refuses to sync until it is re-verified", d.self.Email)
	}
	_ = c.Send(syncproto.Bye{Type: "bye", Reason: "refused", Detail: detail})
	time.Sleep(200 * time.Millisecond) // let the bye leave before closing
}

// Intent values for Sync.
const (
	IntentSync    = syncproto.IntentSync
	IntentHandoff = syncproto.IntentHandoff
)

// SyncRequest is a sync started by the local user.
type SyncRequest struct {
	Path   string `json:"path"`
	To     string `json:"to"`
	Intent string `json:"intent,omitempty"`
}

// ErrOffline is returned when the peer is not connected.
type ErrOffline struct{ Email string }

func (e *ErrOffline) Error() string {
	return fmt.Sprintf("%s is offline (not connected to the rendezvous server); nothing was queued, try again when they are online", e.Email)
}

// Sync syncs a blueprint with a peer, or hands it off (spec 7.1, 7.4).
func (d *Daemon) Sync(ctx context.Context, req SyncRequest, scope string, progress func(string)) (syncproto.Result, error) {
	var res syncproto.Result
	to := strings.TrimSpace(req.To)
	if !bp.ValidEmail(to) {
		return res, fmt.Errorf("%q is not an email address", req.To)
	}
	if bp.SameEmail(to, d.self.Email) {
		return res, errors.New("that is your own address")
	}
	if progress == nil {
		progress = func(string) {}
	}
	intent := req.Intent
	if intent == "" {
		intent = IntentSync
	}
	if intent != IntentSync && intent != IntentHandoff {
		return res, fmt.Errorf("unknown intent %q", intent)
	}
	if p, ok := d.Peers.Get(to); ok && p.KeyChanged != nil {
		return res, &identity.KeyChangedError{Email: to}
	}
	d.mu.Lock()
	d.addresses[strings.ToLower(to)] = true
	d.mu.Unlock()

	f, err := d.Store.Load(d.resolve(req.Path))
	if err != nil {
		return res, err
	}
	if f.Doc.HasErrors() {
		return res, fmt.Errorf("%s has format errors; run `%s check %s`", f.Path, name.Binary, f.Path)
	}
	if f.Doc.KalkaID() == "" {
		if f, err = d.prepare(f); err != nil {
			return res, err
		}
		progress(fmt.Sprintf("first send: added %s and owner to %s", name.FrontmatterID, f.Path))
	} else if dup, err := d.Store.Find(f.Doc.KalkaID()); err != nil {
		return res, err
	} else if dup != nil && dup.Path != f.Path {
		return res, &store.ErrDuplicate{KalkaID: f.Doc.KalkaID(), Paths: []string{f.Path, dup.Path}}
	}
	id := f.Doc.KalkaID()
	unlock, ok := d.Store.Lock(id, syncproto.LockWait)
	if !ok {
		return res, errors.New("this blueprint is busy in another sync; try again")
	}
	defer unlock()
	if f, err = d.Store.Load(f.Path); err != nil {
		return res, err
	}

	offer := f.Doc
	if intent == IntentHandoff {
		if !bp.SameEmail(f.Doc.Owner(), d.self.Email) {
			return res, fmt.Errorf("only the owner (%s) can hand this blueprint off", f.Doc.Owner())
		}
		if offer, err = d.candidate(f, to); err != nil {
			return res, err
		}
	}

	ep, err := d.endpoint()
	if err != nil {
		return res, err
	}
	progress(fmt.Sprintf("connecting to %s", to))
	dctx, cancel := context.WithTimeout(ctx, DialTimeout)
	c, err := ep.Dial(dctx, transport.PeerID(d.cfg.Team, to))
	cancel()
	if errors.Is(err, transport.ErrOffline) {
		d.markPresence(to, transport.Offline)
		return res, &ErrOffline{Email: to}
	}
	if err != nil {
		return res, fmt.Errorf("connecting to %s: %w", to, err)
	}
	defer c.Close()
	d.markPresence(to, transport.Online)

	peer, err := transport.Handshake(c, d.self, d.cfg.Team, d.Peers.Pin)
	if err != nil {
		d.refuse(c, err)
		return res, err
	}
	if !bp.SameEmail(peer.Email, to) {
		return res, fmt.Errorf("connected to %s instead of %s", peer.Email, to)
	}
	s := d.session(c, peer.Email, scope, progress)
	if err := d.confirm(s, peer); err != nil {
		return res, err
	}
	res, err = s.Initiate(syncproto.Request{File: f, Offer: offer, Intent: intent})
	d.bus.Publish(BusEvent{Type: "peers"})
	if err == nil && intent == IntentHandoff {
		d.mu.Lock()
		delete(d.pending, pendingKey(id, to))
		d.mu.Unlock()
	}
	return res, err
}

// prepare gives a blueprint its kalka_id and owner before its first send.
func (d *Daemon) prepare(f *store.File) (*store.File, error) {
	doc, err := bp.Parse(f.Raw)
	if err != nil {
		return nil, err
	}
	doc.SetFrontmatter(name.FrontmatterID, newUUID())
	if doc.Owner() == "" {
		doc.SetFrontmatter(name.FrontmatterOwner, d.self.Email)
	}
	if err := d.Store.Update(f, doc); err != nil {
		return nil, err
	}
	return d.Store.Load(f.Path)
}

func pendingKey(id, to string) string { return id + "|" + strings.ToLower(to) }

// candidate builds the handoff candidate in memory: owner set to the
// recipient plus a signed handoff entry. An earlier unacknowledged
// candidate built from the same file is reused.
func (d *Daemon) candidate(f *store.File, to string) (*bp.Document, error) {
	key, base := pendingKey(f.Doc.KalkaID(), to), sha256.Sum256(f.Raw)
	d.mu.Lock()
	p, ok := d.pending[key]
	d.mu.Unlock()
	if ok && p.base == base {
		return bp.Parse(p.data)
	}
	doc, err := bp.Parse(f.Raw)
	if err != nil {
		return nil, err
	}
	e, err := bp.NewHandoff(doc.KalkaID(), d.self.Email, to, d.now(), doc.Rev(), d.signer())
	if err != nil {
		return nil, err
	}
	doc.SetFrontmatter(name.FrontmatterOwner, to)
	if err := doc.AddEntry(e); err != nil {
		return nil, err
	}
	data := doc.Bytes()
	if doc, err = bp.Parse(data); err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.pending[key] = candidate{base: base, data: data}
	d.mu.Unlock()
	return doc, nil
}

func newUUID() string { return bp.NewKalkaID() }

func (d *Daemon) markPresence(email string, p transport.Presence) {
	d.mu.Lock()
	old := d.presence[strings.ToLower(email)].State
	d.presence[strings.ToLower(email)] = presence{State: p, Checked: time.Now()}
	d.mu.Unlock()
	if old != p {
		d.bus.Publish(BusEvent{Type: "peers"})
	}
}

// Probe checks one peer now (spec 9.3, on demand).
func (d *Daemon) Probe(ctx context.Context, email string) transport.Presence {
	ep, err := d.endpoint()
	if err != nil {
		return transport.Unknown
	}
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	p := ep.Probe(ctx, transport.PeerID(d.cfg.Team, email))
	d.markPresence(email, p)
	return p
}

// AddAddress remembers an address typed this session, for probing.
func (d *Daemon) AddAddress(email string) {
	d.mu.Lock()
	d.addresses[strings.ToLower(email)] = true
	d.mu.Unlock()
}

// probeLoop probes known peers and typed addresses, at most once a minute
// each.
func (d *Daemon) probeLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		for _, email := range d.probeTargets() {
			d.mu.Lock()
			last := d.presence[email].Checked
			d.mu.Unlock()
			if time.Since(last) >= ProbeEvery {
				go d.Probe(ctx, email)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (d *Daemon) probeTargets() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range d.Peers.List() {
		e := strings.ToLower(p.Email)
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	d.mu.Lock()
	for e := range d.addresses {
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	d.mu.Unlock()
	return out
}

// PeerView is one row of `kalka peers`.
type PeerView struct {
	Email      string             `json:"email"`
	Pinned     bool               `json:"pinned"`
	Verified   bool               `json:"verified"`
	FirstSeen  string             `json:"first_seen,omitempty"`
	KeyChanged bool               `json:"key_changed,omitempty"`
	Code       string             `json:"code,omitempty"`
	Presence   transport.Presence `json:"presence"`
	Checked    *time.Time         `json:"checked,omitempty"`
}

// PeerList lists pinned peers and addresses typed this session.
func (d *Daemon) PeerList() []PeerView {
	out := []PeerView{}
	for _, e := range d.probeTargets() {
		v := PeerView{Email: e, Presence: transport.Unknown}
		if p, ok := d.Peers.Get(e); ok {
			v.Pinned, v.Verified, v.FirstSeen, v.KeyChanged = true, p.Verified, p.FirstSeen, p.KeyChanged != nil
			if k := p.Key(); k != nil {
				v.Code = identity.Code(d.pub, k)
			}
		}
		d.mu.Lock()
		if pr, ok := d.presence[e]; ok {
			v.Presence = pr.State
			t := pr.Checked.UTC()
			v.Checked = &t
		}
		d.mu.Unlock()
		out = append(out, v)
	}
	return out
}

// Status is the daemon's state for `/api/status`.
type Status struct {
	Email      string `json:"email"`
	PeerID     string `json:"peer_id"`
	Rendezvous string `json:"rendezvous"`
	Folder     string `json:"folder"`
	Online     bool   `json:"online"`
	Error      string `json:"error,omitempty"`
	// Notice is the permanent public-cloud warning (spec 9.7).
	Notice string `json:"notice,omitempty"`
}

// CloudNotice is shown permanently in peerjs-cloud mode (spec 9.7).
const CloudNotice = "Public PeerJS cloud: a community-run demo service with no availability guarantee. " +
	"Peer ids can collide or be claimed by anyone who knows an email; only the pinned keys protect the exchange. " +
	"Not for sensitive blueprints."

// State reports the daemon's status.
func (d *Daemon) State() Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := Status{Email: d.self.Email, PeerID: d.peerID, Rendezvous: d.cfg.Rendezvous, Folder: d.Store.Dir(), Online: d.online, Error: d.sigErr}
	if d.cloud {
		s.Notice = CloudNotice
	}
	return s
}

// signer signs with the local identity key: every entry this kalka writes
// carries a v2 signature (spec 6.5).
func (d *Daemon) signer() bp.Signer {
	return func(m []byte) []byte { return ed25519.Sign(d.self.Key, m) }
}
