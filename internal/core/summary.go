package core

import (
	"strings"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/transport"
)

// Summary is the one-line state the tray and `kalka status` show
// (spec 22.1).
type Summary struct {
	Online bool   `json:"online"`
	Paused bool   `json:"paused"`
	Error  string `json:"error,omitempty"`
	// PeersOnline counts known peers and team members whose last probe or
	// session said online; PeersKnown counts them all (not the local user).
	PeersOnline int `json:"peers_online"`
	PeersKnown  int `json:"peers_known"`
	// LastSync is the latest replication session or one-to-one sync that
	// completed with a peer, either side; zero if none since start.
	LastSync time.Time `json:"last_sync,omitzero"`
	// Waiting: prompts to answer, joins not completed, teams with a push
	// still queued (it goes out when a member is reachable).
	Prompts int `json:"prompts"`
	Joins   int `json:"joins"`
	Pushes  int `json:"pushes"`
}

// Waiting is what needs the user: prompts and joins.
func (s Summary) Waiting() int { return s.Prompts + s.Joins }

// noteSync records a completed exchange with a peer.
func (d *Node) noteSync() {
	d.mu.Lock()
	d.lastSync = d.now().UTC()
	d.mu.Unlock()
}

// Summary reports the node's state for the tray and the status command.
func (d *Node) Summary() Summary {
	st := d.State()
	s := Summary{Online: st.Online, Paused: st.Paused, Error: st.Error, Prompts: len(d.Prompts.List())}
	known := map[string]bool{}
	for _, e := range d.probeTargets() {
		known[e] = true
	}
	for _, t := range d.teamDirs() {
		h, err := d.heldTeam(t.Team)
		if err != nil {
			continue
		}
		for e := range h.st.Members {
			if !bp.SameEmail(e, d.self.Email) {
				known[strings.ToLower(e)] = true
			}
		}
	}
	d.mu.Lock()
	for e := range known {
		if d.presence[e].State == transport.Online {
			s.PeersOnline++
		}
	}
	s.LastSync = d.lastSync
	d.mu.Unlock()
	s.PeersKnown = len(known)
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
	return s
}
