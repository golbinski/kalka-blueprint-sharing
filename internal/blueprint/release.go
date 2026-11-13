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
)

// Frontmatter keys of published documents (spec 6.2).
const (
	KeyTeam    = "team"
	KeyDeleted = "deleted"
)

// Release is the content of a release entry (spec 20.4):
//
//	v7 · plan:<64 hex> · owner:alice@corp.com[ · deleted]
//	incorporates: 7c2e91ab04f3, a41b77c09e12
//	Free text.
//
// Owner is the `owner:` of the released plan: the one who may sign the
// next version. Deleted marks a tombstone (spec 20.6).
type Release struct {
	Version      int64
	Plan         string
	Owner        string
	Deleted      bool
	Incorporates []string
	Note         string
}

var releaseRe = regexp.MustCompile(`^v([1-9][0-9]{0,17}) · plan:([0-9a-f]{64}) · owner:(\S+)( · deleted)?$`)

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
	if d.KalkaID() == "" {
		return nil, errors.New("a release needs a kalka_id")
	}
	if v < 1 {
		return nil, errors.New("release versions start at 1")
	}
	if sign == nil {
		return nil, errors.New("a release must be signed")
	}
	r := Release{Version: v, Plan: d.PlanHash(), Owner: d.Owner(), Deleted: deleted, Incorporates: incorporates, Note: note}
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

// Team is the `team:` frontmatter key: the team a document is published
// to, or "".
func (d *Document) Team() string {
	v, _ := d.fm.Get(KeyTeam)
	return strings.TrimSpace(v)
}

// Deleted reports a tombstone stub (`deleted: true`).
func (d *Document) Deleted() bool {
	v, _ := d.fm.Get(KeyDeleted)
	return strings.TrimSpace(v) == "true"
}
