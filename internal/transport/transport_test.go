package transport

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/golbinski/kalka/internal/peerjs"
	"github.com/golbinski/kalka/internal/rendezvous"
)

type harness struct {
	srv  *rendezvous.Server
	http *httptest.Server
	info peerjs.Server
}

func newHarness(t *testing.T, srv *rendezvous.Server) *harness {
	t.Helper()
	mux := http.NewServeMux()
	srv.Mount(mux, "/")
	hs := httptest.NewServer(mux)
	t.Cleanup(func() { hs.Close(); srv.Close() })
	info, err := peerjs.ParseRendezvous(hs.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &harness{srv: srv, http: hs, info: info}
}

type node struct {
	id  Identity
	sig *peerjs.Client
	ep  *Endpoint
}

func (h *harness) node(t *testing.T, email string, opts Options) *node {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sig, err := peerjs.Dial(ctx, h.info, PeerID("", email))
	if err != nil {
		t.Fatal(err)
	}
	opts.Loopback = true
	ep := NewEndpoint(sig, opts)
	t.Cleanup(func() { ep.Close(); sig.Close() })
	return &node{id: Identity{Email: email, Key: priv}, sig: sig, ep: ep}
}

// connect dials from a to b and runs the handshake on both sides.
func connect(t *testing.T, a, b *node) (ca, cb *Conn, errA, errB error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c, err := b.ep.Accept(ctx)
		if err != nil {
			errB = err
			return
		}
		cb = c
		_, errB = Handshake(c, b.id, "", nil)
	}()
	c, err := a.ep.Dial(ctx, b.sig.ID())
	if err != nil {
		cancel()
		wg.Wait()
		return nil, nil, err, errB
	}
	ca = c
	_, errA = Handshake(c, a.id, "", nil)
	wg.Wait()
	return
}

func TestConnectAndTransfer(t *testing.T) {
	h := newHarness(t, &rendezvous.Server{})
	alice, bob := h.node(t, "alice@corp.com", Options{}), h.node(t, "bob@corp.com", Options{})
	ca, cb, errA, errB := connect(t, alice, bob)
	if errA != nil || errB != nil {
		t.Fatalf("handshake: %v / %v", errA, errB)
	}
	defer ca.Close()
	defer cb.Close()

	big := strings.Repeat("é\"\n", 700_000) // ~2.8 MB of JSON after escaping
	done := make(chan error, 1)
	go func() {
		env, err := cb.Receive(10 * time.Second)
		if err != nil {
			done <- err
			return
		}
		var m struct{ Content string }
		if err := env.Decode(&m); err != nil || env.Type != "sync_offer" || m.Content != big {
			done <- errors.New("content mismatch")
			return
		}
		done <- cb.Send(map[string]string{"type": "sync_reply", "status": "merged"})
	}()
	if err := ca.Send(map[string]string{"type": "sync_offer", "content": big}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	env, err := ca.Receive(10 * time.Second)
	if err != nil || env.Type != "sync_reply" {
		t.Fatalf("reply: %v %v", env.Type, err)
	}
}

func TestOfflinePeer(t *testing.T) {
	h := newHarness(t, &rendezvous.Server{ExpireTimeout: time.Second})
	alice := h.node(t, "alice@corp.com", Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	_, err := alice.ep.Dial(ctx, PeerID("", "nobody@corp.com"))
	if !errors.Is(err, ErrOffline) {
		t.Fatalf("dial offline peer: %v", err)
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("offline detection took %v", d)
	}
}

func TestIDTaken(t *testing.T) {
	h := newHarness(t, &rendezvous.Server{})
	h.node(t, "alice@corp.com", Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := peerjs.Dial(ctx, h.info, PeerID("", "alice@corp.com")); !errors.Is(err, peerjs.ErrIDTaken) {
		t.Fatalf("second registration: %v", err)
	}
}

func TestWrongEmailForPeerID(t *testing.T) {
	h := newHarness(t, &rendezvous.Server{})
	alice, bob := h.node(t, "alice@corp.com", Options{}), h.node(t, "bob@corp.com", Options{})
	bob.id.Email = "carol@corp.com" // registered as bob, claims to be carol
	_, _, errA, _ := connect(t, alice, bob)
	if errA == nil || !strings.Contains(errA.Error(), "connected as") {
		t.Fatalf("impersonation not detected: %v", errA)
	}
}

// mitm is a malicious rendezvous: it answers the offerer itself, dials the
// real answerer with its own offer, and relays data channel bytes between
// the two DTLS sessions it terminates.
type mitm struct {
	t   *testing.T
	srv *rendezvous.Server
	api *webrtc.API

	mu    sync.Mutex
	toA   *webrtc.PeerConnection // MITM's side of the offerer's connection
	toB   *webrtc.PeerConnection // MITM's offer to the answerer
	a, b  string                 // peer ids
	idA   string                 // offerer's connectionId
	idB   string                 // MITM's connectionId towards b
	rwA   chan rwd
	rwB   chan rwd
	seenB bool
}

func (m *mitm) intercept(msg peerjs.Message) *peerjs.Message {
	var p sigPayload
	_ = json.Unmarshal(msg.Payload, &p)
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case msg.Type == peerjs.Offer && m.toA == nil:
		m.a, m.b, m.idA = msg.Src, msg.Dst, p.ConnectionID
		go m.hijack(*p.SDP)
		return nil
	case p.ConnectionID == m.idA && msg.Type == peerjs.Candidate && msg.Src == m.a:
		go func() { m.waitPC(&m.toA).AddICECandidate(*p.Candidate) }()
		return nil
	case p.ConnectionID == m.idB && msg.Src == m.b:
		pc := m.toB
		if msg.Type == peerjs.Answer {
			go func() { _ = pc.SetRemoteDescription(*p.SDP) }()
		} else if msg.Type == peerjs.Candidate {
			go func() { time.Sleep(50 * time.Millisecond); _ = pc.AddICECandidate(*p.Candidate) }()
		}
		return nil
	}
	return &msg
}

func (m *mitm) waitPC(pp **webrtc.PeerConnection) *webrtc.PeerConnection {
	for {
		m.mu.Lock()
		pc := *pp
		m.mu.Unlock()
		if pc != nil {
			return pc
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (m *mitm) forge(typ, src, dst, connID string, p sigPayload) {
	p.Type, p.ConnectionID = "data", connID
	raw, _ := json.Marshal(p)
	m.srv.Deliver(peerjs.Message{Type: typ, Src: src, Dst: dst, Payload: raw})
}

func (m *mitm) hijack(offer webrtc.SessionDescription) {
	// Answer the offerer, pretending to be b.
	toA, _ := m.api.NewPeerConnection(webrtc.Configuration{})
	toA.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			init := c.ToJSON()
			m.forge(peerjs.Candidate, m.b, m.a, m.idA, sigPayload{Candidate: &init})
		}
	})
	toA.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() { rw, _ := dc.DetachWithDeadline(); m.rwA <- rw })
	})
	_ = toA.SetRemoteDescription(offer)
	ans, _ := toA.CreateAnswer(nil)
	_ = toA.SetLocalDescription(ans)
	m.mu.Lock()
	m.toA = toA
	m.mu.Unlock()
	m.forge(peerjs.Answer, m.b, m.a, m.idA, sigPayload{SDP: &ans})

	// Offer to b, pretending to be a.
	toB, _ := m.api.NewPeerConnection(webrtc.Configuration{})
	m.mu.Lock()
	m.toB, m.idB = toB, newConnID()
	m.mu.Unlock()
	toB.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			init := c.ToJSON()
			m.forge(peerjs.Candidate, m.a, m.b, m.idB, sigPayload{Candidate: &init})
		}
	})
	dc, _ := toB.CreateDataChannel(Label, nil)
	dc.OnOpen(func() { rw, _ := dc.DetachWithDeadline(); m.rwB <- rw })
	off, _ := toB.CreateOffer(nil)
	_ = toB.SetLocalDescription(off)
	m.forge(peerjs.Offer, m.a, m.b, m.idB, sigPayload{SDP: &off, Label: Label})

	a, b := <-m.rwA, <-m.rwB
	pipe := func(dst, src rwd) {
		buf := make([]byte, 64<<10)
		for {
			n, err := src.Read(buf)
			if err != nil {
				return
			}
			if _, err := dst.Write(buf[:n]); err != nil {
				return
			}
		}
	}
	go pipe(a, b)
	go pipe(b, a)
}

func TestChannelBindingDetectsMITM(t *testing.T) {
	se := webrtc.SettingEngine{}
	se.DetachDataChannels()
	se.SetIncludeLoopbackCandidate(true)
	m := &mitm{t: t, api: webrtc.NewAPI(webrtc.WithSettingEngine(se)), rwA: make(chan rwd, 1), rwB: make(chan rwd, 1)}
	srv := &rendezvous.Server{Intercept: m.intercept}
	m.srv = srv
	h := newHarness(t, srv)
	alice, bob := h.node(t, "alice@corp.com", Options{}), h.node(t, "bob@corp.com", Options{})

	ca, cb, errA, errB := connect(t, alice, bob)
	if ca == nil || cb == nil {
		t.Fatalf("the relay did not come up, so the test proves nothing: %v / %v", errA, errB)
	}
	// Both data channels opened through the relay: hello passed through.
	// The signatures cannot match the fingerprints each side sees.
	for _, err := range []error{errA, errB} {
		if err == nil || !strings.Contains(err.Error(), "channel binding failed") {
			t.Fatalf("MITM not detected: alice=%v bob=%v", errA, errB)
		}
	}
	la, ra := ca.Fingerprints()
	lb, rb := cb.Fingerprints()
	if ra == lb || rb == la {
		t.Fatal("fingerprints match end to end; the relay was not in the middle")
	}
}

// TestTURNOverTCPRelayOnly forces both peers onto TURN over TCP, as on a
// network that blocks UDP, with credentials fetched from /kalka/ice.
func TestTURNOverTCPRelayOnly(t *testing.T) {
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ts, err := rendezvous.StartTURN(rendezvous.TURNConfig{Secret: "s3cret", RelayIP: net.ParseIP("127.0.0.1"), RelayBind: "127.0.0.1", TCP: tcp})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ts.Close() })
	turnURL := "turn:" + tcp.Addr().String() + "?transport=tcp"

	srv := &rendezvous.Server{}
	mux := http.NewServeMux()
	srv.Mount(mux, "/")
	mux.Handle("/kalka/ice", rendezvous.ICEHandler("s3cret", []string{turnURL}, nil, time.Hour))
	hs := httptest.NewServer(mux)
	t.Cleanup(func() { hs.Close(); srv.Close() })
	info, _ := peerjs.ParseRendezvous(hs.URL)
	h := &harness{srv: srv, http: hs, info: info}

	ice, src, err := ResolveICE(context.Background(), nil, info, false)
	if err != nil || src != ICEFromRendezvous || len(ice) != 1 || ice[0].Username == "" {
		t.Fatalf("ICE from rendezvous: %v %v %+v", src, err, ice)
	}
	opts := Options{ICEServers: ice, RelayOnly: true}
	alice, bob := h.node(t, "alice@corp.com", opts), h.node(t, "bob@corp.com", opts)
	ca, cb, errA, errB := connect(t, alice, bob)
	if errA != nil || errB != nil {
		t.Fatalf("handshake over TURN: %v / %v", errA, errB)
	}
	defer ca.Close()
	defer cb.Close()
	pair, err := ca.pc.SCTP().Transport().ICETransport().GetSelectedCandidatePair()
	if err != nil || pair == nil || pair.Local.Typ != webrtc.ICECandidateTypeRelay {
		t.Fatalf("selected pair is not relayed: %v %v", pair, err)
	}
}

func TestResolveICEFallbacks(t *testing.T) {
	hs := httptest.NewServer(http.NotFoundHandler())
	defer hs.Close()
	info, _ := peerjs.ParseRendezvous(hs.URL)
	if s, src, _ := ResolveICE(context.Background(), nil, info, false); src != ICEDefault || s[0].URLs[0] != DefaultSTUN.URLs[0] {
		t.Fatalf("404 should fall back to default STUN: %v %v", src, s)
	}
	manual := []webrtc.ICEServer{{URLs: []string{"stun:example.org:3478"}}}
	if _, src, _ := ResolveICE(context.Background(), manual, info, false); src != ICEFromConfig {
		t.Fatalf("configured servers must win: %v", src)
	}
	if s, _, _ := ResolveICE(context.Background(), nil, peerjs.Server{}, true); len(s) != 2 {
		t.Fatalf("peerjs-cloud defaults: %v", s)
	}
}

func TestPeerID(t *testing.T) {
	a, b := PeerID("", "Alice@Corp.com"), PeerID("", "alice@corp.com")
	if a != b || len(a) != len("kalka-")+32 || !peerjs.ValidID(a) {
		t.Fatalf("PeerID: %q %q", a, b)
	}
	if PeerID("team", "alice@corp.com") == a {
		t.Fatal("team string not mixed in")
	}
}

// TestOfferShapeForPeerJSCloud pins the OFFER fields the public PeerJS
// cloud requires: without label and serialization it drops the sender.
func TestOfferShapeForPeerJSCloud(t *testing.T) {
	var offer map[string]any
	var mu sync.Mutex
	srv := &rendezvous.Server{Intercept: func(m peerjs.Message) *peerjs.Message {
		if m.Type == peerjs.Offer {
			mu.Lock()
			_ = json.Unmarshal(m.Payload, &offer)
			mu.Unlock()
		}
		return &m
	}}
	h := newHarness(t, srv)
	alice, bob := h.node(t, "alice@corp.com", Options{}), h.node(t, "bob@corp.com", Options{})
	if _, _, errA, errB := connect(t, alice, bob); errA != nil || errB != nil {
		t.Fatalf("%v / %v", errA, errB)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, k := range []string{"type", "connectionId", "sdp", "label", "serialization"} {
		if _, ok := offer[k]; !ok {
			t.Errorf("OFFER payload lacks %q: %v", k, offer)
		}
	}
}

func TestProbe(t *testing.T) {
	h := newHarness(t, &rendezvous.Server{ExpireTimeout: 500 * time.Millisecond})
	a := h.node(t, "alice@corp.com", Options{})
	b := h.node(t, "bob@corp.com", Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	t0 := time.Now()
	if p := a.ep.Probe(ctx, b.sig.ID()); p != Online {
		t.Fatalf("online peer: %s", p)
	}
	if d := time.Since(t0); d > 500*time.Millisecond {
		t.Fatalf("online probe took %v", d)
	}
	if p := a.ep.Probe(ctx, PeerID("", "nobody@corp.com")); p != Offline {
		t.Fatalf("absent peer: %s", p)
	}
	// The probe must not disturb a real connection afterwards.
	if _, _, ea, eb := connect(t, a, b); ea != nil || eb != nil {
		t.Fatal(ea, eb)
	}
}
