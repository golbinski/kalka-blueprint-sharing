package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/golbinski/kalka/internal/syncproto"
)

// Pending is a prompt waiting for the user. Scope is the operation that
// asked (a sync started through the API), or empty for incoming syncs.
type Pending struct {
	ID      string    `json:"id"`
	Scope   string    `json:"scope,omitempty"`
	Created time.Time `json:"created"`
	syncproto.Prompt

	answer chan bool
}

// Hub holds open prompts. Any front end (the daemon's terminal, the CLI
// that started a sync, the web UI) may answer; the first answer counts.
type Hub struct {
	mu      sync.Mutex
	pending map[string]*Pending
	subs    map[chan Notice]string // subscriber -> scope filter ("" = all)
}

// Notice tells subscribers a prompt opened or closed.
type Notice struct {
	Prompt *Pending
	Closed bool
}

func newHub() *Hub {
	return &Hub{pending: map[string]*Pending{}, subs: map[chan Notice]string{}}
}

// ErrNoPrompt means the prompt was answered already or never existed.
var ErrNoPrompt = errors.New("no such open prompt")

// Ask opens a prompt and waits for an answer or for ctx to end.
func (h *Hub) Ask(ctx context.Context, scope string, p syncproto.Prompt) (bool, error) {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	pp := &Pending{ID: hex.EncodeToString(b), Scope: scope, Created: time.Now().UTC(), Prompt: p, answer: make(chan bool, 1)}
	h.mu.Lock()
	h.pending[pp.ID] = pp
	h.mu.Unlock()
	h.notify(Notice{Prompt: pp})
	defer func() {
		h.mu.Lock()
		delete(h.pending, pp.ID)
		h.mu.Unlock()
		h.notify(Notice{Prompt: pp, Closed: true})
	}()
	select {
	case ok := <-pp.answer:
		return ok, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// Answer answers an open prompt.
func (h *Hub) Answer(id string, ok bool) error {
	h.mu.Lock()
	p := h.pending[id]
	h.mu.Unlock()
	if p == nil {
		return ErrNoPrompt
	}
	select {
	case p.answer <- ok:
		return nil
	default:
		return ErrNoPrompt
	}
}

// List returns open prompts, oldest first.
func (h *Hub) List() []*Pending {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*Pending, 0, len(h.pending))
	for _, p := range h.pending {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b *Pending) int { return a.Created.Compare(b.Created) })
	return out
}

// Subscribe delivers notices for prompts of scope, or all if scope is "".
func (h *Hub) Subscribe(scope string) (<-chan Notice, func()) {
	ch := make(chan Notice, 16)
	h.mu.Lock()
	h.subs[ch] = scope
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

func (h *Hub) notify(n Notice) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch, scope := range h.subs {
		if scope != "" && scope != n.Prompt.Scope {
			continue
		}
		select {
		case ch <- n:
		default:
		}
	}
}
