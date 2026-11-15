// Package core is the long-running kalka node (spec 4): it keeps the
// rendezvous registration, replicates teams with their members, watches
// the folder and probes member presence.
package core

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/identity"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/peerid"
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
	// Paused starts the node paused (spec 22.3; the daemon keeps the
	// state in state.yaml).
	Paused bool
	// Now is the clock for entry timestamps; nil means time.Now.
	Now func() time.Time
	// Log receives one line per event; nil discards.
	Log io.Writer
	// Loopback allows loopback ICE candidates and skips ICE discovery
	// (tests on one machine).
	Loopback bool
	// Plugins resolves references (spec 10); nil means none installed.
	Plugins Plugins

	// manualReplication, set by tests, runs replication rounds only when
	// asked (ReplicateNow), so scenarios are deterministic.
	manualReplication bool
}

// Daemon is one running kalka.
type Node struct {
	// bg tracks work started in the background (rounds, takeovers after a
	// log change) so Run returns only when it is done.
	bg sync.WaitGroup
	// work is the context of background work. It outlives Run's ctx by
	// up to DrainTimeout, so a stop lets running rounds finish.
	work context.Context
	// stopping is Run's ctx: once it ends no new connection is made, so a
	// round still dialing an offline peer does not hold a stop up.
	stopping context.Context
	cfg      *config.Config
	dir      string
	now      func() time.Time
	log      io.Writer
	opts     Options
	self     transport.Identity
	pub      ed25519.PublicKey
	keyID    string // the one peer id (spec 9.2)
	srv      peerjs.Server
	cloud    bool

	Store *store.Store
	bus   *Bus
	refs  *refCache
	teams *teams
	repl  *replication

	mu        sync.Mutex
	ep        *transport.Endpoint // on the key id
	online    bool
	sigErr    string
	ice       []webrtc.ICEServer
	iceAt     time.Time
	presence  map[string]presence
	readyCh   chan struct{}
	readyOnce sync.Once
	lastSync  time.Time // guarded by mu; see noteSync
	notes     notices

	// pause (spec 22.3): paused is the state, pauseCh is closed and
	// replaced on every change so loops can wait for one.
	pauseMu sync.Mutex
	paused  bool
	pauseCh chan struct{}
	logMu   sync.Mutex

	// wrapChan, set by tests, wraps the channel of replication sessions
	// (recording what leaves this daemon).
	wrapChan func(syncproto.Channel) syncproto.Channel
}

type presence struct {
	State   transport.Presence `json:"state"`
	Checked time.Time          `json:"checked"`
}

// New prepares a node: identity key and folder index.
func New(opts Options) (*Node, error) {
	if err := Validate(opts.Config); err != nil {
		return nil, err
	}
	key, err := identity.LoadOrCreateKey(opts.ConfigDir)
	if err != nil {
		return nil, fmt.Errorf("identity key: %w", err)
	}
	return newNode(opts, key)
}

// Open is the read-only node for views with no daemon running (spec 12):
// it reads the key, folder and team logs, creates nothing, and is
// never Run, so it makes no connection. Presence is unknown and the node
// reports itself offline. Only its view methods may be used.
func Open(opts Options) (*Node, error) {
	if err := Validate(opts.Config); err != nil {
		return nil, err
	}
	key, err := identity.LoadKey(opts.ConfigDir)
	if err != nil {
		return nil, err
	}
	return newNode(opts, key)
}

func newNode(opts Options, key ed25519.PrivateKey) (*Node, error) {
	cfg := opts.Config
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	// The Personal folder: files there never leave this machine (spec 14).
	_ = os.Mkdir(filepath.Join(cfg.Folder, store.PersonalDir), 0o755)
	st, err := store.Open(cfg.Folder)
	if err != nil {
		return nil, fmt.Errorf("blueprint folder: %w", err)
	}
	srv, _ := peerjs.ParseRendezvous(cfg.Rendezvous)
	log := opts.Log
	if log == nil {
		log = io.Discard
	}
	return &Node{
		cfg: cfg, dir: opts.ConfigDir, now: now, log: log, opts: opts,
		self:  transport.Identity{Email: cfg.Email, Key: key},
		pub:   key.Public().(ed25519.PublicKey),
		keyID: transport.KeyPeerID(key.Public().(ed25519.PublicKey)),
		srv:   srv, cloud: cfg.Rendezvous == config.PeerJSCloud,
		Store: st, bus: newBus(), refs: newRefCache(opts.Plugins, now),
		teams:    newTeams(opts.ConfigDir),
		repl:     newReplication(),
		presence: map[string]presence{},
		readyCh:  make(chan struct{}),
		paused:   opts.Paused,
		pauseCh:  make(chan struct{}),
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
func (d *Node) Email() string { return d.self.Email }

// PublicKey is the local identity key.
func (d *Node) PublicKey() ed25519.PublicKey { return d.pub }

// Logf writes one line to the node's log.
func (d *Node) Logf(format string, a ...any) { d.logf(format, a...) }

func (d *Node) logf(format string, a ...any) {
	d.logMu.Lock()
	defer d.logMu.Unlock()
	fmt.Fprintf(d.log, "%s %s\n", d.now().Format("15:04:05"), fmt.Sprintf(format, a...))
}

// DrainTimeout bounds how long a stopping node waits for running
// background work (replication rounds) before it cancels it.
var DrainTimeout = 30 * time.Second

// Run runs the node (rendezvous, replication, joins, probes) until
// ctx ends. It serves nothing; the daemon around it does (spec 4). When
// ctx ends, no new work starts; running rounds finish, still connected to
// the rendezvous, for up to DrainTimeout (spec 12, `kalka stop`).
func (d *Node) Run(ctx context.Context) error {
	work, abort := context.WithCancel(context.WithoutCancel(ctx))
	defer abort()
	d.work, d.stopping = work, ctx
	d.refs.ctx = ctx // plugin calls end with the daemon
	go d.forward(ctx)
	var wg, sig sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = d.Store.Watch(ctx) }()
	sig.Add(1)
	go func() { defer sig.Done(); d.network(ctx, work) }()
	if !d.opts.manualReplication {
		wg.Add(1)
		go func() { defer wg.Done(); d.replicateLoop(ctx) }()
	}
	<-ctx.Done()
	wg.Wait() // nothing new starts from here
	drained := make(chan struct{})
	go func() { d.bg.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-time.After(DrainTimeout):
		d.logf("stopping: background work still running after %s, cancelling it", DrainTimeout)
		abort()
		<-drained
	}
	abort()
	sig.Wait()
	return nil
}

// network runs the loops that use the network while not paused: the
// rendezvous registration (on work, so a stop drains sessions) and the
// join and probe loops (which also end at a stop). A pause ends them all;
// a resume starts them again.
func (d *Node) network(stop, work context.Context) {
	for work.Err() == nil {
		paused, changed := d.pauseState()
		if paused {
			d.markReady() // started, and deliberately offline
			select {
			case <-changed:
			case <-work.Done():
			}
			continue
		}
		nctx, cancel := context.WithCancel(work)
		lctx, lcancel := context.WithCancel(nctx)
		unstop := context.AfterFunc(stop, lcancel)
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); d.signalLoop(nctx) }()
		go func() { defer wg.Done(); d.joinLoop(lctx) }()
		go func() { defer wg.Done(); d.probeLoop(lctx) }()
		select {
		case <-changed:
		case <-work.Done():
		}
		cancel()
		wg.Wait()
		unstop()
		lcancel()
	}
}

func (d *Node) markReady() { d.readyOnce.Do(func() { close(d.readyCh) }) }

// ErrPaused is returned by actions that need the network while paused.
var ErrPaused = errors.New(name.Product + " is paused: resume sync to do this")

// Paused reports whether kalka is paused (spec 22.3).
func (d *Node) Paused() bool {
	p, _ := d.pauseState()
	return p
}

func (d *Node) pauseState() (bool, <-chan struct{}) {
	d.pauseMu.Lock()
	defer d.pauseMu.Unlock()
	return d.paused, d.pauseCh
}

// SetPaused takes kalka offline or back online. Local work goes on while
// paused; pushes queued meanwhile go out after the resume.
func (d *Node) SetPaused(p bool) {
	d.pauseMu.Lock()
	if d.paused == p {
		d.pauseMu.Unlock()
		return
	}
	d.paused = p
	close(d.pauseCh)
	d.pauseCh = make(chan struct{})
	d.pauseMu.Unlock()
	if p {
		d.logf("paused: offline until resumed")
	} else {
		d.logf("resumed")
		d.kickTeam("")
	}
	d.bus.Publish(BusEvent{Type: "status"})
}

// isStopping reports whether Run's ctx has ended.
func (d *Node) isStopping() bool { return d.stopping != nil && d.stopping.Err() != nil }

// background runs f in a goroutine that Run waits for.
func (d *Node) background(f func()) {
	d.bg.Add(1)
	go func() { defer d.bg.Done(); f() }()
}

// forward relays folder changes to the bus, whose subscribers are the
// UI's event streams and the presenters around the core.
func (d *Node) forward(ctx context.Context) {
	folder, stop := d.Store.Subscribe()
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-folder:
			d.bus.Publish(BusEvent{Type: "folder", Data: e})
		}
	}
}

// Bus carries the node's events (folder changes, teams, status).
func (d *Node) Bus() *Bus { return d.bus }

// ConfigDir is the configuration directory.
func (d *Node) ConfigDir() string { return d.dir }

// Ready is closed when the daemon first registers with the rendezvous.
func (d *Node) Ready() <-chan struct{} { return d.readyCh }

func (d *Node) iceServers() []webrtc.ICEServer {
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

// signalLoop keeps the one rendezvous registration, on the key id with a
// fresh proof each time (spec 9.2, 9.6; other servers ignore the proof),
// reconnecting with backoff.
func (d *Node) signalLoop(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		id, proof := peerid.Proof(d.self.Key, time.Now())
		sig, err := peerjs.DialQuery(ctx, d.srv, id, url.Values{peerid.ProofParam: {proof}})
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
		d.logf("connected to %s as %s (key id %s)", d.srv.URL, d.cfg.Email, sig.ID())
		d.markReady()
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

func (d *Node) setOnline(ep *transport.Endpoint, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	defer d.bus.Publish(BusEvent{Type: "status"})
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	d.ep, d.online = ep, ep != nil
	if msg != "" && msg != d.sigErr {
		d.logf("rendezvous: %v", err)
	}
	d.sigErr = msg
}

func (d *Node) endpoint() (*transport.Endpoint, error) {
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

// serve handles an incoming connection. Only members are peers (spec
// 8.2): after the handshake, a joiner's invite goes to the join and a
// team_sync opens a replication session, which admits the caller only if
// both sides are members once the log operations it brings are merged
// (20.3). Anything else is told not-member; no file is read for it.
func (d *Node) serve(c *transport.Conn) {
	defer c.Close()
	peer, err := transport.Handshake(c, d.self, nil)
	if err != nil {
		d.logf("refused a connection: %v", err)
		return
	}
	if peer.Invite != nil {
		d.markPresence(peer.Email, transport.Online)
		d.redeem(c, peer)
		return
	}
	// The responder dispatches on the first message (spec 9.5).
	first, err := c.Receive(syncproto.Silence)
	if err != nil {
		d.logf("connection from %s: %v", peer.Email, err)
		return
	}
	d.markPresence(peer.Email, transport.Online)
	if first.Type != "team_sync" {
		d.notMember(c)
		d.logf("refused a connection from %s: it opened with %q", peer.Email, first.Type)
		return
	}
	d.replicaRespond(c, peer, first)
}

// notMember ends a connection with the not-member bye, then the caller
// closes.
func (d *Node) notMember(c *transport.Conn) {
	_ = c.Send(syncproto.Bye{Type: "bye", Reason: syncproto.ByeNotMember})
	time.Sleep(200 * time.Millisecond) // let the bye leave before closing
}

// ErrOffline is returned when a member is not connected.
type ErrOffline struct{ Email string }

func (e *ErrOffline) Error() string {
	return fmt.Sprintf("%s is offline (not connected to the rendezvous server)", e.Email)
}

func newUUID() string { return bp.NewKalkaID() }

func (d *Node) markPresence(email string, p transport.Presence) {
	d.mu.Lock()
	old := d.presence[strings.ToLower(email)].State
	d.presence[strings.ToLower(email)] = presence{State: p, Checked: time.Now()}
	d.mu.Unlock()
	if old != p {
		d.bus.Publish(BusEvent{Type: "teams"})
	}
	if p == transport.Online && old != transport.Online {
		d.memberOnline(email)
	}
}

// memberKeys is every member of the teams held here, but the local user,
// with the key its team log binds (spec 9.2: a member is reached by the
// key id of that key).
func (d *Node) memberKeys() map[string]ed25519.PublicKey {
	out := map[string]ed25519.PublicKey{}
	for _, t := range d.teamDirs() {
		h, err := d.heldTeam(t.Team)
		if err != nil {
			continue
		}
		for e := range h.st.Members {
			if bp.SameEmail(e, d.self.Email) {
				continue
			}
			if k, ok := h.memberKey(e); ok {
				out[strings.ToLower(e)] = k
			}
		}
	}
	return out
}

// Probe checks one member now (spec 9.3, on demand).
func (d *Node) Probe(ctx context.Context, email string) transport.Presence {
	key, ok := d.memberKeys()[strings.ToLower(email)]
	ep, err := d.endpoint()
	if !ok || err != nil {
		return transport.Unknown
	}
	pctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	p := ep.Probe(pctx, transport.KeyPeerID(key))
	cancel()
	d.markPresence(email, p)
	return p
}

// probeLoop probes the members of all teams, at most once a minute each.
func (d *Node) probeLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		for email := range d.memberKeys() {
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

// Status is the daemon's state for `/api/status`.
type Status struct {
	Email string `json:"email"`
	// PeerID is the key id this node registers (spec 9.2).
	PeerID string `json:"peer_id"`
	// Fingerprint of the local identity key, grouped for reading aloud.
	Fingerprint string `json:"fingerprint"`
	Rendezvous  string `json:"rendezvous"`
	Folder      string `json:"folder"`
	Online      bool   `json:"online"`
	Paused      bool   `json:"paused"`
	Error       string `json:"error,omitempty"`
	// Notice is the permanent public-cloud warning (spec 9.7).
	Notice string `json:"notice,omitempty"`
}

// CloudNotice is shown permanently in peerjs-cloud mode (spec 9.7).
const CloudNotice = "Public PeerJS cloud: a community-run demo service with no availability guarantee. " +
	"A peer id can be claimed by anyone who knows the public key, which keeps its owner offline; " +
	"only the team logs protect the exchange. Not for sensitive blueprints."

// State reports the daemon's status.
func (d *Node) State() Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := Status{Email: d.self.Email, PeerID: d.keyID, Fingerprint: peerid.Display(peerid.Fingerprint(d.pub)),
		Rendezvous: d.cfg.Rendezvous, Folder: d.Store.Dir(), Online: d.online, Error: d.sigErr, Paused: d.Paused()}
	if d.cloud {
		s.Notice = CloudNotice
	}
	return s
}

// signer signs with the local identity key: every entry this kalka writes
// carries a v2 signature (spec 6.5).
func (d *Node) signer() bp.Signer {
	return func(m []byte) []byte { return ed25519.Sign(d.self.Key, m) }
}
