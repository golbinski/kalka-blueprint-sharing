package core

import (
	"sync"
	"sync/atomic"
)

// Bus fans daemon events out to the UI's event streams.
type Bus struct {
	mu   sync.Mutex
	subs map[chan BusEvent]struct{}
	n    atomic.Int32
}

// BusEvent is one server-sent event.
type BusEvent struct {
	Type string `json:"type"` // folder | prompt | prompt-closed | sync | status | peers | teams | refs | notice | settings
	Data any    `json:"data,omitempty"`
}

func newBus() *Bus { return &Bus{subs: map[chan BusEvent]struct{}{}} }

// Publish delivers e to every subscriber that keeps up.
func (b *Bus) Publish(e BusEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Subscribe adds a stream.
func (b *Bus) Subscribe() (<-chan BusEvent, func()) {
	ch := make(chan BusEvent, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	b.n.Add(1)
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
		b.n.Add(-1)
	}
}

// Watch adds a subscriber inside the process (a presenter); unlike
// Subscribe it does not count as a connected UI.
func (b *Bus) Watch() (<-chan BusEvent, func()) {
	ch := make(chan BusEvent, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
}

// Clients is the number of connected UI streams.
func (b *Bus) Clients() int { return int(b.n.Load()) }
