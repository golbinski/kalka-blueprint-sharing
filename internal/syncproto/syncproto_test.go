package syncproto

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/merge"
	"github.com/golbinski/kalka/internal/store"
	"github.com/golbinski/kalka/internal/transport"
)

// pipe is an in-memory Channel pair.
type pipe struct {
	in, out chan []byte
}

func pipes() (*pipe, *pipe) {
	a, b := make(chan []byte, 16), make(chan []byte, 16)
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
	case raw, ok := <-p.in:
		if !ok {
			return transport.Envelope{}, io.EOF
		}
		var h struct{ Type string }
		_ = json.Unmarshal(raw, &h)
		return transport.Envelope{Type: h.Type, Raw: raw}, nil
	case <-time.After(timeout):
		return transport.Envelope{}, errors.New("timeout")
	}
}

func noKeys(string) (ed25519.PublicKey, bool) { return nil, false }

const src = "---\nkalka_id: 6f1c2a0e-8d1b-4c57-9a43-0e2b7c9d51aa\nowner: alice@corp.com\n---\n# T\n\n## Plan\nP\n"

func folder(t *testing.T, files map[string]string) *store.Store {
	t.Helper()
	dir := t.TempDir()
	for n, c := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func answer(ok bool, seen *[]Prompt) Prompter {
	return func(_ context.Context, p Prompt) (bool, error) {
		if seen != nil {
			*seen = append(*seen, p)
		}
		return ok, nil
	}
}

type side struct {
	st    *store.Store
	email string
	ask   Prompter
}

func sync(t *testing.T, from, to side, file, intent string, offer *bp.Document) (Result, error, []Result, error) {
	t.Helper()
	a, b := pipes()
	type out struct {
		r   []Result
		err error
	}
	done := make(chan out, 1)
	go func() {
		s := &Session{Ch: b, Self: to.email, Peer: from.email, Keys: noKeys, Store: to.st, Ask: to.ask}
		r, err := s.Respond()
		done <- out{r, err}
	}()
	f, err := from.st.Load(filepath.Join(from.st.Dir(), file))
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{Ch: a, Self: from.email, Peer: to.email, Keys: noKeys, Store: from.st, Ask: from.ask}
	ri, erri := s.Initiate(Request{File: f, Offer: offer, Intent: intent})
	close(a.out)
	o := <-done
	return ri, erri, o.r, o.err
}

func read(t *testing.T, st *store.Store, name string) string {
	b, err := os.ReadFile(filepath.Join(st.Dir(), name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestNewDeclinedThenAccepted(t *testing.T) {
	alice := side{folder(t, map[string]string{"plan.md": src}), "alice@corp.com", nil}
	var seen []Prompt
	bob := side{folder(t, nil), "bob@corp.com", answer(false, &seen)}
	_, err, _, _ := sync(t, alice, bob, "plan.md", IntentSync, nil)
	var rej *Rejected
	if !errors.As(err, &rej) || rej.Reason != merge.Declined || len(seen) != 1 || seen[0].Kind != PromptNew {
		t.Fatalf("declined: %v %+v", err, seen)
	}
	bob.ask = answer(true, nil)
	ri, err, rr, rerr := sync(t, alice, bob, "plan.md", IntentSync, nil)
	if err != nil || rerr != nil || rr[0].Status != "new" || ri.Status != "unchanged" {
		t.Fatalf("%v %v %+v %+v", err, rerr, rr, ri)
	}
	if read(t, bob.st, "plan.md") != src {
		t.Fatal("bob's file differs")
	}
}

func TestConvergenceSavesConflict(t *testing.T) {
	edited := strings.Replace(src, "P\n", "Bob's edit\n", 1)
	e, _ := bp.NewEntry(bp.KindComment, "bob@corp.com", time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC), "plan", "00000000", "keep me")
	d, _ := bp.Parse([]byte(edited))
	_ = d.AddEntry(e)
	bob := side{folder(t, map[string]string{"plan.md": string(d.Bytes())}), "bob@corp.com", answer(true, nil)}
	alice := side{folder(t, map[string]string{"plan.md": src}), "alice@corp.com", nil}
	ri, err, rr, rerr := sync(t, bob, alice, "plan.md", IntentSync, nil)
	if err != nil || rerr != nil {
		t.Fatal(err, rerr)
	}
	if len(rr) != 2 || rr[0].Reason != merge.NotOwner || rr[1].Status != "updated" || ri.Conflict == "" {
		t.Fatalf("%+v %+v", rr, ri)
	}
	b, a := read(t, bob.st, "plan.md"), read(t, alice.st, "plan.md")
	if b != a || strings.Contains(b, "Bob's edit") || !strings.Contains(b, "keep me") {
		t.Fatalf("not converged:\n%s\n---\n%s", b, a)
	}
	if c, _ := os.ReadFile(ri.Conflict); !strings.Contains(string(c), "Bob's edit") {
		t.Fatal("conflict copy lacks the displaced edit")
	}
}

func TestDiverged(t *testing.T) {
	// Each side's copy names a different owner, and neither has a handoff
	// chain: no one can accept the other's plan.
	mine := strings.Replace(src, "P\n", "carol's plan\n", 1)
	mine = strings.Replace(mine, "owner: alice", "owner: carol", 1)
	carol := side{folder(t, map[string]string{"plan.md": mine}), "carol@corp.com", answer(true, nil)}
	alice := side{folder(t, map[string]string{"plan.md": src}), "alice@corp.com", nil}
	_, err, _, rerr := sync(t, carol, alice, "plan.md", IntentSync, nil)
	var dv *Diverged
	if !errors.As(err, &dv) || !strings.Contains(err.Error(), "plans diverged") {
		t.Fatalf("initiator: %v", err)
	}
	if rerr != nil {
		t.Fatalf("responder: %v", rerr)
	}
}

func TestConfirmWithKeepalives(t *testing.T) {
	old := [3]time.Duration{Silence, KeepaliveEvery, PromptTimeout}
	Silence, KeepaliveEvery = 400*time.Millisecond, 40*time.Millisecond
	defer func() { Silence, KeepaliveEvery, PromptTimeout = old[0], old[1], old[2] }()

	a, b := pipes()
	slow := func(context.Context, Prompt) (bool, error) { time.Sleep(time.Second); return true, nil }
	marked := 0
	errs := make(chan error, 1)
	go func() {
		s := &Session{Ch: b, Peer: "alice@corp.com", Ask: slow}
		errs <- s.Confirm(false, "ABCD-EFGH", func() error { marked++; return nil })
	}()
	s := &Session{Ch: a, Peer: "bob@corp.com", Ask: answer(true, nil)}
	if err := s.Confirm(true, "ABCD-EFGH", nil); err != nil {
		t.Fatalf("waiting side gave up despite keep-alives: %v", err)
	}
	if err := <-errs; err != nil || marked != 1 {
		t.Fatal(err, marked)
	}

	// A declined code fails both sides.
	a, b = pipes()
	go func() {
		s := &Session{Ch: b, Peer: "alice@corp.com", Ask: answer(false, nil)}
		errs <- s.Confirm(false, "X", nil)
	}()
	s = &Session{Ch: a, Peer: "bob@corp.com", Ask: answer(true, nil)}
	if err := s.Confirm(false, "X", func() error { return nil }); !errors.Is(err, ErrNotVerified) {
		t.Fatal(err)
	}
	if err := <-errs; !errors.Is(err, ErrNotVerified) {
		t.Fatal(err)
	}
}
