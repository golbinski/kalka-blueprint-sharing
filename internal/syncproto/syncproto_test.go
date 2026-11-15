package syncproto

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/transport"
)

type fakeChan struct{ in []transport.Envelope }

func (f *fakeChan) Send(any) error { return nil }

func (f *fakeChan) Receive(time.Duration) (transport.Envelope, error) {
	if len(f.in) == 0 {
		return transport.Envelope{}, errors.New("closed")
	}
	e := f.in[0]
	f.in = f.in[1:]
	return e, nil
}

func envelope(t *testing.T, v any) transport.Envelope {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var head struct{ Type string }
	if err := json.Unmarshal(b, &head); err != nil {
		t.Fatal(err)
	}
	return transport.Envelope{Type: head.Type, Raw: b}
}

// The responder reads the first message to choose the session, then the
// session reads it again (spec 9.5).
func TestPrepend(t *testing.T) {
	first := envelope(t, TeamSync{Type: "team_sync", Team: "t"})
	second := envelope(t, TeamDoc{Type: "team_doc", KalkaID: "k"})
	ch := Prepend(&fakeChan{in: []transport.Envelope{second}}, first)
	for _, want := range []string{"team_sync", "team_doc"} {
		e, err := ch.Receive(time.Second)
		if err != nil || e.Type != want {
			t.Fatalf("got %q %v, want %q", e.Type, err, want)
		}
	}
	if _, err := ch.Receive(time.Second); err == nil {
		t.Fatal("read past the end")
	}
}

// Only the messages of spec 9.5 exist: no offer, reply, ack or verify.
func TestTeamDocShape(t *testing.T) {
	b, _ := json.Marshal(TeamDoc{Type: "team_doc", Team: "h", KalkaID: "k", Filename: "a.md", Content: "x"})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"type", "team", "kalka_id", "filename", "content"} {
		if _, ok := m[k]; !ok {
			t.Errorf("team_doc has no %q", k)
		}
	}
	if len(m) != 5 {
		t.Errorf("team_doc fields: %v", m)
	}
}
