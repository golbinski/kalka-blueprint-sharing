// Package syncproto holds the messages of a team session on an
// authenticated channel (spec 9.5, 20.3): log operations, inventories and
// documents, and the bye reasons of replication and joins. Nothing on the
// wire asks a person anything.
package syncproto

import (
	"encoding/json"
	"time"

	"github.com/golbinski/kalka/internal/transport"
)

// Timing (spec 9.5). Variables so tests can shorten them.
var (
	// Silence is how long either side waits for the next message.
	Silence = 30 * time.Second
	// ByeWait is how long the initiator waits for the closing bye.
	ByeWait = 5 * time.Second
	// LockWait is how long a write waits for a document busy in another
	// session or command.
	LockWait = 10 * time.Second
	// KeepaliveEvery is how often a side busy applying what it received
	// tells the other it is still there; well below Silence.
	KeepaliveEvery = 10 * time.Second
)

// MaxWant is the most documents one team_want names (spec 9.5). A side
// that wants more asks for the rest in the next session, which follows at
// once. A variable so tests can lower it.
var MaxWant = 200

// Limits of one session (spec 9.5).
const (
	// MaxWantAccepted is the most a side serves: a team_want over it
	// ends the session as invalid.
	MaxWantAccepted = 1000
)

// Channel is the part of transport.Conn the protocol uses.
type Channel interface {
	Send(v any) error
	Receive(timeout time.Duration) (transport.Envelope, error)
}

// Keepalive keeps a slow session open (spec 9.5).
type Keepalive struct {
	Type string `json:"type"`
}

// Bye ends a session; Reason and Detail say why when it ends early.
type Bye struct {
	Type   string `json:"type"`
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// TeamLog carries team log operations, one canonical encoding each: in a
// replication session, and once by an owner who admits a joiner (spec
// 19.7 step 5).
type TeamLog struct {
	Type string            `json:"type"`
	Team string            `json:"team"`
	Ops  []json.RawMessage `json:"ops"`
}

// TeamSync opens a replication session: the log heads and one digest over
// the inventory (spec 20.3).
type TeamSync struct {
	Type   string   `json:"type"`
	Team   string   `json:"team"`
	Heads  []string `json:"heads"`
	Digest string   `json:"digest"`
}

// InvItem is one inventory line: a team document's head release and the
// set of its replicated entries.
type InvItem struct {
	ID      string `json:"id"`
	Version int64  `json:"v"`
	Plan    string `json:"plan"`
	Entries string `json:"entries"`
	Deleted bool   `json:"deleted,omitempty"`
	// Pending marks a pending transfer (spec 20.7): no release yet;
	// with Deleted, its stub after a withdrawal or a decline.
	Pending bool `json:"pending,omitempty"`
}

// TeamInv carries the sender's inventory.
type TeamInv struct {
	Type  string    `json:"type"`
	Team  string    `json:"team"`
	Items []InvItem `json:"items"`
}

// TeamWant names the documents the sender wants; each follows as a
// team_doc.
type TeamWant struct {
	Type string   `json:"type"`
	Team string   `json:"team"`
	IDs  []string `json:"ids"`
}

// TeamDoc is one wanted document. An empty Content means unavailable: the
// same answer for a Personal document, another team's, or none at all.
type TeamDoc struct {
	Type     string `json:"type"`
	Team     string `json:"team"`
	KalkaID  string `json:"kalka_id"`
	Filename string `json:"filename"`
	Content  string `json:"content"`
	Pending  bool   `json:"pending,omitempty"` // a pending transfer or its stub
}

// known are the message types of protocol version 1. Any other type is
// one of a newer kalka, and is ignored (spec 9.5).
var known = map[string]bool{"hello": true, "auth": true, "bye": true, "keepalive": true,
	"team_sync": true, "team_log": true, "team_inv": true, "team_want": true, "team_doc": true}

// Known reports whether typ is a message type this kalka speaks.
func Known(typ string) bool { return known[typ] }

// Replication session outcomes, as bye reasons.
const (
	ByeDone      = "done"
	ByeNotMember = "not-member"
)

// Join outcomes, as the owner's bye reason (spec 19.7 step 6).
const (
	ByeInviteUnknown = "invite-unknown"
	ByeInviteUsed    = "invite-used"
	ByeInviteExpired = "invite-expired"
	ByeInviteProof   = "invite-proof"
	ByeInviteTeam    = "invite-team"
	ByeInviteEmail   = "invite-email"
	ByeInviteNoOwner = "invite-not-owner"
	ByeInviteKey     = "invite-key-clash"
	ByeJoined        = "joined"
)

// PeerEnded means the peer closed the session with a bye.
type PeerEnded struct{ Reason, Detail string }

func (e *PeerEnded) Error() string {
	if e.Detail == "" {
		return "the peer ended the session"
	}
	return "the peer ended the session: " + e.Detail
}

// Prepend returns ch with env put back in front, for a responder that
// read the first message to choose the session type (spec 9.5).
func Prepend(ch Channel, env transport.Envelope) Channel {
	return &prepended{Channel: ch, first: &env}
}

type prepended struct {
	Channel
	first *transport.Envelope
}

func (p *prepended) Receive(timeout time.Duration) (transport.Envelope, error) {
	if e := p.first; e != nil {
		p.first = nil
		return *e, nil
	}
	return p.Channel.Receive(timeout)
}
