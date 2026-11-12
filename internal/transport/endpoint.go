package transport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pion/datachannel"
	"github.com/pion/webrtc/v4"

	"github.com/golbinski/kalka/internal/peerjs"
)

// ErrOffline means the rendezvous server reported the peer absent.
var ErrOffline = errors.New("peer is offline")

// Options configure an Endpoint.
type Options struct {
	ICEServers []webrtc.ICEServer
	// RelayOnly forces TURN (tests, and networks where only relay works).
	RelayOnly bool
	// Loopback allows loopback candidates (tests on one machine).
	Loopback bool
	// AnswerTimeout bounds how long an accepted offer may take to open.
	AnswerTimeout time.Duration
}

// Endpoint dials and accepts data channels over one signaling connection.
type Endpoint struct {
	sig  *peerjs.Client
	api  *webrtc.API
	opts Options

	mu       sync.Mutex
	sessions map[string]*session // by connectionId
	incoming chan *Conn
	done     chan struct{}
	closed   bool
}

// session is a connection being set up or in use.
type session struct {
	id        string
	peer      string
	pc        *webrtc.PeerConnection
	remoteSet bool
	cands     []webrtc.ICECandidateInit
	answer    chan webrtc.SessionDescription
	fail      chan error
}

// sigPayload is the payload of OFFER, ANSWER and CANDIDATE, shaped like the
// PeerJS client's so the messages look ordinary to any PeerJS server.
type sigPayload struct {
	Type         string                     `json:"type"`
	ConnectionID string                     `json:"connectionId"`
	SDP          *webrtc.SessionDescription `json:"sdp,omitempty"`
	Candidate    *webrtc.ICECandidateInit   `json:"candidate,omitempty"`
	Label        string                     `json:"label,omitempty"`
	Reliable     bool                       `json:"reliable,omitempty"`
	// Serialization is meaningless to kalka, but the public PeerJS cloud
	// closes the sender's socket on an OFFER without label and
	// serialization (found 2026-11-12, docs/m1-transport.md).
	Serialization string `json:"serialization,omitempty"`
}

// NewEndpoint starts dispatching signaling messages from sig.
func NewEndpoint(sig *peerjs.Client, opts Options) *Endpoint {
	se := webrtc.SettingEngine{}
	se.DetachDataChannels()
	se.SetIncludeLoopbackCandidate(opts.Loopback)
	if opts.AnswerTimeout == 0 {
		opts.AnswerTimeout = 30 * time.Second
	}
	e := &Endpoint{
		sig:      sig,
		api:      webrtc.NewAPI(webrtc.WithSettingEngine(se)),
		opts:     opts,
		sessions: map[string]*session{},
		incoming: make(chan *Conn, 8),
		done:     make(chan struct{}),
	}
	go e.dispatch()
	return e
}

func (e *Endpoint) config() webrtc.Configuration {
	c := webrtc.Configuration{ICEServers: e.opts.ICEServers}
	if e.opts.RelayOnly {
		c.ICETransportPolicy = webrtc.ICETransportPolicyRelay
	}
	return c
}

// Close stops accepting and closes connections still being set up.
func (e *Endpoint) Close() {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	e.closed = true
	close(e.done)
	for _, s := range e.sessions {
		_ = s.pc.Close()
	}
	e.mu.Unlock()
}

// Accept returns the next incoming connection.
func (e *Endpoint) Accept(ctx context.Context) (*Conn, error) {
	select {
	case c := <-e.incoming:
		return c, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-e.done:
		return nil, errors.New("endpoint closed")
	}
}

func newConnID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "dc_" + hex.EncodeToString(b)
}

func (e *Endpoint) add(s *session) {
	e.mu.Lock()
	e.sessions[s.id] = s
	e.mu.Unlock()
}

func (e *Endpoint) remove(id string) {
	e.mu.Lock()
	delete(e.sessions, id)
	e.mu.Unlock()
}

func (e *Endpoint) send(typ, dst string, p sigPayload) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p.Type = "data"
	return e.sig.Send(ctx, typ, dst, p)
}

func (e *Endpoint) trickle(s *session) {
	s.pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		_ = e.send(peerjs.Candidate, s.peer, sigPayload{ConnectionID: s.id, Candidate: &init})
	})
}

// Dial opens a data channel to the peer with id dst. It fails with
// ErrOffline when the server reports dst absent (after its expire timeout).
func (e *Endpoint) Dial(ctx context.Context, dst string) (*Conn, error) {
	pc, err := e.api.NewPeerConnection(e.config())
	if err != nil {
		return nil, err
	}
	s := &session{id: newConnID(), peer: dst, pc: pc, answer: make(chan webrtc.SessionDescription, 1), fail: make(chan error, 1)}
	e.add(s)
	ok := false
	defer func() {
		if !ok {
			e.remove(s.id)
			_ = pc.Close()
		}
	}()

	e.trickle(s)
	failOnState(pc, s.fail)
	dc, err := pc.CreateDataChannel(Label, nil)
	if err != nil {
		return nil, err
	}
	opened := detachOnOpen(dc)

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return nil, err
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		return nil, err
	}
	if err := e.send(peerjs.Offer, dst, sigPayload{ConnectionID: s.id, SDP: &offer, Label: Label, Reliable: true, Serialization: "raw"}); err != nil {
		return nil, err
	}

	select {
	case ans := <-s.answer:
		if err := e.setRemote(s, ans); err != nil {
			return nil, err
		}
	case err := <-s.fail:
		return nil, err
	case <-ctx.Done():
		return nil, fmt.Errorf("no answer from peer: %w", ctx.Err())
	}

	select {
	case r := <-opened:
		if r.err != nil {
			return nil, r.err
		}
		c, err := newConn(pc, r.rw, dst, func() { e.remove(s.id) })
		if err != nil {
			return nil, err
		}
		ok = true
		return c, nil
	case err := <-s.fail:
		return nil, err
	case <-ctx.Done():
		return nil, fmt.Errorf("connection to peer did not open: %w", ctx.Err())
	}
}

func (e *Endpoint) setRemote(s *session, d webrtc.SessionDescription) error {
	if err := s.pc.SetRemoteDescription(d); err != nil {
		return err
	}
	e.mu.Lock()
	s.remoteSet = true
	cands := s.cands
	s.cands = nil
	e.mu.Unlock()
	for _, c := range cands {
		_ = s.pc.AddICECandidate(c)
	}
	return nil
}

type rwd = datachannel.ReadWriteCloserDeadliner

type detached struct {
	rw  rwd
	err error
}

func detachOnOpen(dc *webrtc.DataChannel) <-chan detached {
	ch := make(chan detached, 1)
	dc.OnOpen(func() {
		rw, err := dc.DetachWithDeadline()
		ch <- detached{rw: rw, err: err}
	})
	return ch
}

func failOnState(pc *webrtc.PeerConnection, fail chan error) {
	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		if st == webrtc.PeerConnectionStateFailed {
			select {
			case fail <- errors.New("WebRTC connection failed: no usable network path (TURN may be needed)"):
			default:
			}
		}
	})
}

func (e *Endpoint) dispatch() {
	for m := range e.sig.Messages() {
		switch m.Type {
		case peerjs.Offer:
			e.onOffer(m)
		case peerjs.Answer, peerjs.Candidate:
			var p sigPayload
			if json.Unmarshal(m.Payload, &p) != nil {
				continue
			}
			e.mu.Lock()
			s := e.sessions[p.ConnectionID]
			if s == nil || s.peer != m.Src {
				e.mu.Unlock()
				continue
			}
			if m.Type == peerjs.Answer && p.SDP != nil {
				e.mu.Unlock()
				select {
				case s.answer <- *p.SDP:
				default:
				}
				continue
			}
			if p.Candidate != nil {
				if !s.remoteSet {
					s.cands = append(s.cands, *p.Candidate)
					e.mu.Unlock()
					continue
				}
				e.mu.Unlock()
				_ = s.pc.AddICECandidate(*p.Candidate)
				continue
			}
			e.mu.Unlock()
		case peerjs.Expire, peerjs.Leave:
			e.mu.Lock()
			for _, s := range e.sessions {
				if s.peer == m.Src {
					select {
					case s.fail <- ErrOffline:
					default:
					}
				}
			}
			e.mu.Unlock()
		}
	}
	e.Close()
}

func (e *Endpoint) onOffer(m peerjs.Message) {
	var p sigPayload
	if json.Unmarshal(m.Payload, &p) != nil || p.SDP == nil || p.ConnectionID == "" {
		return
	}
	pc, err := e.api.NewPeerConnection(e.config())
	if err != nil {
		return
	}
	s := &session{id: p.ConnectionID, peer: m.Src, pc: pc, fail: make(chan error, 1)}
	e.add(s)
	e.trickle(s)
	failOnState(pc, s.fail)

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != Label {
			_ = dc.Close()
			return
		}
		opened := detachOnOpen(dc)
		go func() {
			select {
			case r := <-opened:
				if r.err != nil {
					break
				}
				c, err := newConn(pc, r.rw, s.peer, func() { e.remove(s.id) })
				if err != nil {
					break
				}
				select {
				case e.incoming <- c:
					return
				case <-e.done:
				}
			case <-time.After(e.opts.AnswerTimeout):
			}
			e.remove(s.id)
			_ = pc.Close()
		}()
	})

	fail := func() { e.remove(s.id); _ = pc.Close() }
	if err := e.setRemote(s, *p.SDP); err != nil {
		fail()
		return
	}
	ans, err := pc.CreateAnswer(nil)
	if err != nil {
		fail()
		return
	}
	if err := pc.SetLocalDescription(ans); err != nil {
		fail()
		return
	}
	if err := e.send(peerjs.Answer, m.Src, sigPayload{ConnectionID: s.id, SDP: &ans}); err != nil {
		fail()
	}
	// Close a half-open session if the data channel never arrives.
	time.AfterFunc(e.opts.AnswerTimeout, func() {
		if pc.ConnectionState() != webrtc.PeerConnectionStateConnected {
			fail()
		}
	})
}
