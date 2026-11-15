package core

import (
	"time"

	"github.com/golbinski/kalka/internal/transport"
)

// Summary is the one-line state the tray and `kalka status` show
// (spec 22.1).
type Summary struct {
	Online bool   `json:"online"`
	Paused bool   `json:"paused"`
	Error  string `json:"error,omitempty"`
	// PeersOnline counts the members of all teams whose last probe or
	// session said online; PeersKnown counts them all (not the local user).
	PeersOnline int `json:"peers_online"`
	PeersKnown  int `json:"peers_known"`
	// LastSync is the latest replication session that completed with a
	// member, either side; zero if none since start.
	LastSync time.Time `json:"last_sync,omitzero"`
	// Waiting is what waits for the user, over every team (spec 11.3).
	Waiting WaitingSummary `json:"waiting"`
	// Joins in progress and teams with a push still queued (it goes out
	// when a member is reachable): not waiting items.
	Joins  int `json:"joins"`
	Pushes int `json:"pushes"`
	// Shell is "desktop" while a desktop shell is attached (spec 22).
	Shell string `json:"shell,omitempty"`
}

// noteSync records a completed exchange with a peer.
func (d *Node) noteSync() {
	d.mu.Lock()
	d.lastSync = d.now().UTC()
	d.mu.Unlock()
}

// Summary reports the node's state for the tray and the status command.
func (d *Node) Summary() Summary {
	st := d.State()
	s := Summary{Online: st.Online, Paused: st.Paused, Error: st.Error}
	known := d.memberKeys()
	d.mu.Lock()
	for e := range known {
		if d.presence[e].State == transport.Online {
			s.PeersOnline++
		}
	}
	s.LastSync = d.lastSync
	d.mu.Unlock()
	s.PeersKnown = len(known)
	if d.Bus().Shells() > 0 {
		s.Shell = "desktop"
	}
	for _, j := range d.teams.joins.list() {
		if j.State == JoinPending {
			s.Joins++
		}
	}
	d.repl.mu.Lock()
	for _, p := range d.repl.push {
		if p {
			s.Pushes++
		}
	}
	d.repl.mu.Unlock()
	s.Waiting = d.WaitingSummary()
	return s
}
