package syncproto

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/transport"
)

// pipe is an in-memory Channel pair.
type pipe struct{ in, out chan []byte }

func pipes() (*pipe, *pipe) {
	a, b := make(chan []byte, 8), make(chan []byte, 8)
	return &pipe{in: a, out: b}, &pipe{in: b, out: a}
}

func (p *pipe) Send(v any) error {
	raw, err := json.Marshal(v)
	if err == nil {
		p.out <- raw
	}
	return err
}

func (p *pipe) Receive(timeout time.Duration) (transport.Envelope, error) {
	select {
	case raw := <-p.in:
		var h struct{ Type string }
		_ = json.Unmarshal(raw, &h)
		return transport.Envelope{Type: h.Type, Raw: raw}, nil
	case <-time.After(timeout):
		return transport.Envelope{}, errors.New("timeout")
	}
}

type memStore struct {
	doc    *bp.Document
	accept bool
	writes int
}

func (m *memStore) Find(id string) (*bp.Document, error) {
	if m.doc != nil && m.doc.KalkaID() == id {
		return m.doc, nil
	}
	return nil, nil
}
func (m *memStore) Write(d *bp.Document, _ string, _ bool) error { m.doc = d; m.writes++; return nil }
func (m *memStore) AcceptNew(string, *bp.Document) bool          { return m.accept }

func noKeys(string) (ed25519.PublicKey, bool) { return nil, false }

const src = "---\nkalka_id: 6f1c2a0e-8d1b-4c57-9a43-0e2b7c9d51aa\nowner: alice@corp.com\n---\n# T\n\n## Plan\nP\n"

func parse(t *testing.T, s string) *bp.Document {
	d, err := bp.Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func run(t *testing.T, local *bp.Document, st *memStore, sender, responder string) (merge.Result, error, merge.Result, error) {
	a, b := pipes()
	type out struct {
		r   merge.Result
		err error
	}
	done := make(chan out, 1)
	go func() { r, err := Respond(b, st, sender, noKeys); done <- out{r, err} }()
	var written *bp.Document
	ri, erri := Initiate(a, local, "plan.md", responder, noKeys, func(d *bp.Document) error { written = d; return nil })
	_ = written
	o := <-done
	return ri, erri, o.r, o.err
}

func TestNewThenReviewRoundTrip(t *testing.T) {
	st := &memStore{accept: true}
	_, err, rr, rerr := run(t, parse(t, src), st, "alice@corp.com", "bob@corp.com")
	if err != nil || rerr != nil || rr.Outcome != merge.New || st.writes != 1 {
		t.Fatalf("first send: %v %v %v writes=%d", err, rerr, rr.Outcome, st.writes)
	}
	// Bob comments; Alice syncs again and receives the comment.
	e, _ := bp.NewEntry(bp.KindComment, "bob@corp.com", time.Now(), "plan", st.doc.Rev(), "hm")
	_ = st.doc.AddEntry(e)
	st.doc, _ = st.doc.Reparse()
	ri, err, _, rerr := run(t, parse(t, src), st, "alice@corp.com", "bob@corp.com")
	if err != nil || rerr != nil || ri.Outcome != merge.Updated || !strings.Contains(string(ri.Doc.Bytes()), "hm") {
		t.Fatalf("second sync: %v %v %v", err, rerr, ri.Outcome)
	}
}

func TestDeclinedAndNotOwner(t *testing.T) {
	_, err, _, _ := run(t, parse(t, src), &memStore{accept: false}, "alice@corp.com", "bob@corp.com")
	var rej *Rejected
	if !errors.As(err, &rej) || rej.Reason != merge.Declined {
		t.Fatalf("declined: %v", err)
	}
	st := &memStore{doc: parse(t, src)}
	edited := parse(t, strings.Replace(src, "P\n", "Q\n", 1))
	_, err, _, _ = run(t, edited, st, "bob@corp.com", "alice@corp.com")
	if !errors.As(err, &rej) || rej.Reason != merge.NotOwner || rej.Content != src {
		t.Fatalf("not-owner must carry the responder's file: %v", err)
	}
}
