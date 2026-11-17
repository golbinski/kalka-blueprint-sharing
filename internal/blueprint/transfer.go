package blueprint

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/golbinski/kalka/internal/name"
)

// Transfers between teams (spec 20.7).
const (
	KindTransfer          = "transfer"
	KindTransferWithdrawn = "transfer-withdrawn"

	KeyCopiedFrom = name.Product + "_copied_from"
	KeyMovedFrom  = name.Product + "_moved_from"
	KeyMovedTo    = name.Product + "_moved_to"

	ModeCopy = "copy"
	ModeMove = "move"
)

// Transfer is the content of a transfer entry, the actor's countersignature
// of an offer (spec 20.7):
//
//	move · from:parcel-ops/<kalka_id> · source:<64 hex> · to:library-desk/<64 hex>
//	plan:<64 hex>
//	history:<64 hex> · entries: <32 hex>, <32 hex>
//
// FromTeam and FromID name the source document; Source and To are the two
// teams by hash, ToTeam the destination's name for the source's banner;
// Plan is the offered plan's hash; History the digest of the
// attested entries Entries (HistoryDigest).
type Transfer struct {
	Mode     string
	FromTeam string
	FromID   string
	Source   string
	ToTeam   string
	To       string
	Plan     string
	History  string
	Entries  []string
}

var (
	transferHeadRe = regexp.MustCompile(`^(copy|move) · from:([a-z0-9][a-z0-9-]{0,62})/(\S{1,64}) · source:([0-9a-f]{64}) · to:([a-z0-9][a-z0-9-]{0,62})/([0-9a-f]{64})$`)
	transferPlanRe = regexp.MustCompile(`^plan:([0-9a-f]{64})$`)
	transferHistRe = regexp.MustCompile(`^history:([0-9a-f]{64})(?: · entries: (.+))?$`)
	originRe       = regexp.MustCompile(`^([a-z0-9][a-z0-9-]{0,62})/([0-9a-f-]{1,64})$`)
)

// Transfer parses a transfer entry's body.
func (e *Entry) Transfer() (Transfer, bool) {
	var t Transfer
	if e.Kind != KindTransfer {
		return t, false
	}
	lines := strings.Split(Normalize(e.Body), "\n")
	if len(lines) != 3 {
		return t, false
	}
	m := transferHeadRe.FindStringSubmatch(lines[0])
	p := transferPlanRe.FindStringSubmatch(lines[1])
	h := transferHistRe.FindStringSubmatch(lines[2])
	if m == nil || p == nil || h == nil {
		return t, false
	}
	t = Transfer{Mode: m[1], FromTeam: m[2], FromID: strings.ToLower(m[3]), Source: m[4], ToTeam: m[5], To: m[6], Plan: p[1], History: h[1]}
	if h[2] != "" {
		for _, id := range strings.Split(h[2], ",") {
			id = strings.TrimSpace(id)
			if !idRe.MatchString(id) {
				return Transfer{}, false
			}
			t.Entries = append(t.Entries, id)
		}
	}
	return t, true
}

func (t Transfer) body() string {
	b := fmt.Sprintf("%s · from:%s/%s · source:%s · to:%s/%s\nplan:%s\nhistory:%s", t.Mode, t.FromTeam, t.FromID, t.Source, t.ToTeam, t.To, t.Plan, t.History)
	if len(t.Entries) > 0 {
		b += " · entries: " + strings.Join(t.Entries, ", ")
	}
	return b
}

// From is the source as `<team name>/<kalka_id>`.
func (t Transfer) From() string { return t.FromTeam + "/" + t.FromID }

// HistoryDigest is the digest a transfer entry signs over its attested
// entries (spec 20.7): SHA-256 over one line `<full digest> <sig or ->\n`
// per entry, in (timestamp, id) order. It covers the full digests and the
// original signature text.
func HistoryDigest(entries []*Entry) string {
	es := append([]*Entry(nil), entries...)
	sort.Slice(es, func(i, j int) bool { return es[i].Less(es[j]) })
	h := sha256.New()
	for _, e := range es {
		sig := e.Sig
		if sig == "" {
			sig = "-"
		}
		fmt.Fprintf(h, "%s %s\n", e.Digest(), sig)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// EntryIDs lists the ids of entries in (timestamp, id) order.
func EntryIDs(entries []*Entry) []string {
	es := append([]*Entry(nil), entries...)
	sort.Slice(es, func(i, j int) bool { return es[i].Less(es[j]) })
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.ID
	}
	return out
}

// NewTransfer builds the signed transfer entry of d, the offered document
// (or, for a move, the source document recording the start). The entry
// is signed under d's kalka_id; the caller adds it.
func NewTransfer(d *Document, author string, at time.Time, t Transfer, sign Signer) (*Entry, error) {
	if d.KalkaID() == "" {
		return nil, errors.New("a transfer needs a kalka_id")
	}
	if !sign.Ok() {
		return nil, errors.New("a transfer must be signed")
	}
	if t.Mode != ModeCopy && t.Mode != ModeMove {
		return nil, fmt.Errorf("transfer mode %q", t.Mode)
	}
	e, err := NewEntry(KindTransfer, author, at, "", d.Rev(), t.body())
	if err != nil {
		return nil, err
	}
	e.Sign(d.KalkaID(), sign)
	if _, ok := e.Transfer(); !ok {
		return nil, errors.New("the transfer entry does not parse")
	}
	return e, nil
}

// NewWithdrawal builds the signed transfer-withdrawn entry that ends the
// offer whose transfer entry is transferID (spec 20.7): `withdrawn` on the
// pending document, `move declined` or `move ended as a copy` on the
// source of a move.
func NewWithdrawal(d *Document, author string, at time.Time, transferID, text string, sign Signer) (*Entry, error) {
	if !sign.Ok() || d.KalkaID() == "" {
		return nil, errors.New("a withdrawal must be signed in a document with a kalka_id")
	}
	e, err := NewEntry(KindTransferWithdrawn, author, at, "", d.Rev(), "re: "+transferID+"\n"+text)
	if err != nil {
		return nil, err
	}
	e.Sign(d.KalkaID(), sign)
	return e, nil
}

// Withdraws is the transfer entry a transfer-withdrawn entry ends, or "".
func (e *Entry) Withdraws() string {
	if e.Kind != KindTransferWithdrawn {
		return ""
	}
	return e.ReplyTo()
}

// ParseOrigin splits `<team name>/<kalka_id>`.
func ParseOrigin(s string) (team, id string, ok bool) {
	m := originRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(s)))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// CopiedFrom, MovedFrom and MovedTo are the transfer keys (spec 6.2), as
// written.
func (d *Document) CopiedFrom() string { return d.get(KeyCopiedFrom) }
func (d *Document) MovedFrom() string  { return d.get(KeyMovedFrom) }
func (d *Document) MovedTo() string    { return d.get(KeyMovedTo) }

// TransferredFrom is the source of a copy or a move, and the mode, from
// the frontmatter; "" when the document did not come by a transfer.
func (d *Document) TransferredFrom() (mode, team, id string) {
	if s := d.MovedFrom(); s != "" {
		if t, i, ok := ParseOrigin(s); ok {
			return ModeMove, t, i
		}
	}
	if s := d.CopiedFrom(); s != "" {
		if t, i, ok := ParseOrigin(s); ok {
			return ModeCopy, t, i
		}
	}
	return "", "", ""
}
