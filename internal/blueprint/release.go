package blueprint

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golbinski/kalka/internal/name"
)

// Frontmatter keys of published documents (spec 6.2).
const (
	KeyTeam = name.Product + "_team"
)

// Release is the content of a release entry (spec 20.4):
//
//	v7 · plan:<64 hex> · owner:alice@corp.com[ · deleted]
//	incorporates: <32 hex>, <32 hex>
//	Free text.
//
// Owner is the `kalka_owner:` of the released plan: the one who may sign the
// next version. Deleted marks a tombstone (spec 20.6); Moved, on a
// tombstone, the `<team name>/<kalka_id>` a move went to (20.7).
type Release struct {
	Version      int64
	Plan         string
	Owner        string
	Deleted      bool
	Moved        string
	Incorporates []string
	Note         string
}

// releaseRe is the first line of a release. Trailing name:value tokens
// a later kalka may add are signed with the body and ignored here.
var releaseRe = regexp.MustCompile(`^v([1-9][0-9]{0,17}) · plan:([0-9a-f]{64}) · owner:(\S+)( · deleted(?: · moved:(\S+))?)?((?: · [a-z][a-z0-9-]{0,31}:\S+)*)$`)

// Release parses a release entry's body.
func (e *Entry) Release() (Release, bool) {
	var r Release
	if e.Kind != KindRelease {
		return r, false
	}
	lines := strings.Split(Normalize(e.Body), "\n")
	m := releaseRe.FindStringSubmatch(lines[0])
	if m == nil || !emailRe.MatchString(m[3]) {
		return r, false
	}
	r.Version, _ = strconv.ParseInt(m[1], 10, 64)
	r.Plan, r.Owner, r.Deleted = m[2], m[3], m[4] != ""
	if m[5] != "" {
		if _, _, ok := ParseOrigin(m[5]); !ok {
			return Release{}, false
		}
		r.Moved = m[5]
	}
	rest := lines[1:]
	if len(rest) > 0 {
		if list, ok := strings.CutPrefix(rest[0], "incorporates: "); ok {
			for _, id := range strings.Split(list, ",") {
				if id = strings.TrimSpace(id); idRe.MatchString(id) {
					r.Incorporates = append(r.Incorporates, id)
				}
			}
			rest = rest[1:]
		}
	}
	r.Note = strings.TrimSpace(strings.Join(rest, "\n"))
	return r, true
}

func (r Release) body() string {
	b := fmt.Sprintf("v%d · plan:%s · owner:%s", r.Version, r.Plan, r.Owner)
	if r.Deleted {
		b += " · deleted"
		if r.Moved != "" {
			b += " · moved:" + r.Moved
		}
	}
	if len(r.Incorporates) > 0 {
		b += "\nincorporates: " + strings.Join(r.Incorporates, ", ")
	}
	if n := strings.TrimSpace(r.Note); n != "" {
		b += "\n" + n
	}
	return b
}

// NewRelease builds a signed release of d's current plan as version v.
// The plan hash and owner are taken from d; the caller adds the entry.
func NewRelease(d *Document, author string, at time.Time, v int64, deleted bool, incorporates []string, note string, sign Signer) (*Entry, error) {
	return newRelease(d, author, at, Release{Version: v, Deleted: deleted, Incorporates: incorporates, Note: note}, sign)
}

// NewMoveTombstone builds the signed tombstone release of a moved document
// (spec 20.6, 20.7): version v, deleted, moved to `<team name>/<kalka_id>`.
func NewMoveTombstone(d *Document, author string, at time.Time, v int64, moved string, sign Signer) (*Entry, error) {
	if _, _, ok := ParseOrigin(moved); !ok {
		return nil, fmt.Errorf("moved:%s is not <team>/<kalka_id>", moved)
	}
	return newRelease(d, author, at, Release{Version: v, Deleted: true, Moved: moved}, sign)
}

func newRelease(d *Document, author string, at time.Time, r Release, sign Signer) (*Entry, error) {
	v := r.Version
	if d.KalkaID() == "" {
		return nil, errors.New("a release needs a kalka_id")
	}
	if v < 1 {
		return nil, errors.New("release versions start at 1")
	}
	if !sign.Ok() {
		return nil, errors.New("a release must be signed")
	}
	r.Plan, r.Owner = d.PlanHash(), d.Owner()
	if !emailRe.MatchString(r.Owner) {
		return nil, fmt.Errorf("owner %q is not an email address", r.Owner)
	}
	e, err := NewEntry(KindRelease, author, at, "", d.Rev(), r.body())
	if err != nil {
		return nil, err
	}
	e.Sign(d.KalkaID(), sign)
	return e, nil
}

// PlanHash is the hex SHA-256 of the normalized frontmatter and body: the
// plan a release names.
func (d *Document) PlanHash() string {
	sum := sha256.Sum256([]byte(d.Plan()))
	return hex.EncodeToString(sum[:])
}

// Team is the `kalka_team:` frontmatter key: the team a document is published
// to, or "".
func (d *Document) Team() string {
	v, _ := d.fm.Get(KeyTeam)
	return strings.TrimSpace(v)
}

// Deleted reports a tombstone (spec 20.6): a deleted release whose id
// matches its content names this plan. A tombstone is final, so releases
// above it do not matter. The release decides, never a frontmatter key;
// merge checks its signature and chain.
func (d *Document) Deleted() bool {
	plan := d.PlanHash()
	for _, it := range d.Items() {
		if r, ok := it.Entry.Release(); ok && it.Entry.Valid() && r.Deleted && r.Plan == plan {
			return true
		}
	}
	return false
}
