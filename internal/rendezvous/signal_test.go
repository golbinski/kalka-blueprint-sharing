package rendezvous

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/peerid"
	"github.com/golbinski/kalka/internal/peerjs"
)

func serve(t *testing.T, s *Server) peerjs.Server {
	t.Helper()
	mux := http.NewServeMux()
	s.Mount(mux, "/")
	hs := httptest.NewServer(mux)
	t.Cleanup(func() { hs.Close(); s.Close() })
	info, err := peerjs.ParseRendezvous(hs.URL)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func dial(t *testing.T, info peerjs.Server, id string) (*peerjs.Client, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := peerjs.Dial(ctx, info, id)
	if err == nil {
		t.Cleanup(func() { c.Close() })
	}
	return c, err
}

func recv(c *peerjs.Client, d time.Duration) (peerjs.Message, bool) {
	select {
	case m, ok := <-c.Messages():
		return m, ok
	case <-time.After(d):
		return peerjs.Message{}, false
	}
}

const sdpOffer = "v=0\r\no=- 1 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\nc=IN IP4 0.0.0.0\r\na=mid:0\r\n"

func offer(label, ser *string, sdp string) map[string]any {
	p := map[string]any{"type": "data", "connectionId": "dc_1", "sdp": map[string]string{"type": "offer", "sdp": sdp}}
	if label != nil {
		p["label"] = *label
	}
	if ser != nil {
		p["serialization"] = *ser
	}
	return p
}

// The public cloud's OFFER rules: a bad OFFER closes the sender and is
// not relayed; a good one is relayed with src set by the server.
func TestOfferRules(t *testing.T) {
	info := serve(t, &Server{})
	label, ser := "kalka/1", "raw"
	for name, p := range map[string]map[string]any{
		"no label":         offer(nil, &ser, sdpOffer),
		"no serialization": offer(&label, nil, sdpOffer),
		"bad sdp":          offer(&label, &ser, "not sdp"),
		"no media":         offer(&label, &ser, "v=0\r\no=- 1 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n"),
	} {
		t.Run(name, func(t *testing.T) {
			a, err := dial(t, info, "a-"+strings.ReplaceAll(name, " ", "-"))
			if err != nil {
				t.Fatal(err)
			}
			b, err := dial(t, info, "b-"+strings.ReplaceAll(name, " ", "-"))
			if err != nil {
				t.Fatal(err)
			}
			_ = a.Send(context.Background(), peerjs.Offer, b.ID(), p)
			if _, ok := recv(a, 3*time.Second); ok {
				t.Fatal("sender got a message instead of being closed")
			}
			if m, ok := recv(b, 300*time.Millisecond); ok {
				t.Fatalf("relayed: %+v", m)
			}
		})
	}

	a, _ := dial(t, info, "good-a")
	b, _ := dial(t, info, "good-b")
	_ = a.Send(context.Background(), peerjs.Offer, b.ID(), offer(&label, &ser, sdpOffer))
	m, ok := recv(b, 3*time.Second)
	if !ok || m.Type != peerjs.Offer || m.Src != "good-a" {
		t.Fatalf("good offer: %+v %v", m, ok)
	}
}

func TestRefusals(t *testing.T) {
	info := serve(t, &Server{MaxClients: 2})
	if _, err := dial(t, info, "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := dial(t, info, "two"); err != nil {
		t.Fatal(err)
	}
	if _, err := dial(t, info, "three"); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("over the limit: %v", err)
	}
	if _, err := dial(t, info, "one"); !errors.Is(err, peerjs.ErrIDTaken) {
		t.Fatalf("taken: %v", err)
	}
	bad := info
	bad.Key = "nope"
	if _, err := dial(t, bad, "four"); err == nil || !strings.Contains(err.Error(), "Invalid key") {
		t.Fatalf("key: %v", err)
	}
}

// Messages for an absent id are queued up to MaxQueue, then dropped; the
// sender learns EXPIRE once.
func TestQueueCap(t *testing.T) {
	s := &Server{MaxQueue: 3, ExpireTimeout: time.Hour}
	info := serve(t, s)
	a, _ := dial(t, info, "sender")
	for i := 0; i < 10; i++ {
		_ = a.Send(context.Background(), peerjs.Candidate, "absent", map[string]int{"n": i})
	}
	time.Sleep(300 * time.Millisecond)
	s.mu.Lock()
	n, total := len(s.queue["absent"]), s.queued
	s.mu.Unlock()
	if n != 3 || total != 3 {
		t.Fatalf("queued %d (total %d)", n, total)
	}
	b, _ := dial(t, info, "absent")
	for i := 0; i < 3; i++ {
		m, ok := recv(b, 2*time.Second)
		var p struct{ N int }
		_ = json.Unmarshal(m.Payload, &p)
		if !ok || m.Type != peerjs.Candidate || p.N != i {
			t.Fatalf("queued message %d: %+v", i, m)
		}
	}
	s.mu.Lock()
	total = s.queued
	s.mu.Unlock()
	if total != 0 {
		t.Fatalf("queued count after delivery: %d", total)
	}
}

// A key peer id registers only with a valid proof of the key (spec 9.6);
// email ids register as before.
func TestKeyIDProof(t *testing.T) {
	now := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	info := serve(t, &Server{Now: func() time.Time { return now }})
	_, key, _ := ed25519.GenerateKey(nil)
	_, other, _ := ed25519.GenerateKey(nil)
	id, proof := peerid.Proof(key, now)
	_, otherProof := peerid.Proof(other, now)
	_, stale := peerid.Proof(key, now.Add(-time.Hour))
	try := func(p string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var q url.Values
		if p != "" {
			q = url.Values{peerid.ProofParam: {p}}
		}
		c, err := peerjs.DialQuery(ctx, info, id, q)
		if err == nil {
			t.Cleanup(func() { c.Close() })
		}
		return err
	}
	for name, p := range map[string]string{"none": "", "another key": otherProof, "stale": stale, "junk": "a.b.c"} {
		if err := try(p); err == nil || !strings.Contains(err.Error(), "Key id refused") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := try(proof); err != nil {
		t.Fatalf("valid proof: %v", err)
	}
	if _, err := dial(t, info, peerid.Email("", "desk@library.example")); err != nil {
		t.Fatalf("email id: %v", err)
	}
	// A stock server takes the key id from anyone.
	loose := serve(t, &Server{NoKeyProof: true})
	if _, err := dial(t, loose, id); err != nil {
		t.Fatalf("stock-server mode: %v", err)
	}
}
