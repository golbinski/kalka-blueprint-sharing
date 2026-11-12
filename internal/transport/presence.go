package transport

import (
	"context"
	"strings"

	"github.com/pion/webrtc/v4"

	"github.com/golbinski/kalka/internal/peerjs"
)

// Presence is what a probe learned about a peer (spec 9.3).
type Presence string

const (
	Online  Presence = "online"
	Offline Presence = "offline"
	// Unknown: the server neither reported the id absent nor did the peer
	// answer before the probe gave up.
	Unknown Presence = "unknown"
)

const (
	probePrefix = "probe_"
	pongPrefix  = "pong_"
)

type probe struct {
	peer   string
	result chan Presence
}

// endOfCandidates is the payload of a probe: a well-formed CANDIDATE that
// any WebRTC stack treats as harmless, sent under a connection id no
// session uses.
func endOfCandidates() *webrtc.ICECandidateInit {
	mid, idx := "0", uint16(0)
	return &webrtc.ICECandidateInit{Candidate: "", SDPMid: &mid, SDPMLineIndex: &idx}
}

// Probe asks whether dst is connected to the rendezvous, without setting up
// a peer connection. A kalka peer answers at once; a server reports an
// absent id with EXPIRE after its expire timeout (about 5 s for stock
// peerjs-server, at once for the public cloud). Give ctx a deadline.
func (e *Endpoint) Probe(ctx context.Context, dst string) Presence {
	id := probePrefix + newConnID()[3:]
	pr := &probe{peer: dst, result: make(chan Presence, 1)}
	e.mu.Lock()
	e.probes[id] = pr
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.probes, id)
		e.mu.Unlock()
	}()
	if err := e.send(peerjs.Candidate, dst, sigPayload{Type: "data", ConnectionID: id, Candidate: endOfCandidates()}); err != nil {
		return Unknown
	}
	select {
	case r := <-pr.result:
		return r
	case <-ctx.Done():
		return Unknown
	case <-e.done:
		return Unknown
	}
}

// onProbe handles probe and pong candidates; it reports whether m was one.
func (e *Endpoint) onProbe(src, connID string) bool {
	switch {
	case strings.HasPrefix(connID, probePrefix):
		e.mu.Lock()
		_, ours := e.sessions[connID]
		e.mu.Unlock()
		if !ours {
			_ = e.send(peerjs.Candidate, src, sigPayload{Type: "data", ConnectionID: pongPrefix + connID[len(probePrefix):], Candidate: endOfCandidates()})
		}
		return true
	case strings.HasPrefix(connID, pongPrefix):
		e.mu.Lock()
		pr := e.probes[probePrefix+connID[len(pongPrefix):]]
		e.mu.Unlock()
		if pr != nil && pr.peer == src {
			select {
			case pr.result <- Online:
			default:
			}
		}
		return true
	}
	return false
}
