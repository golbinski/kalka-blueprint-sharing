package blueprint

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/golbinski/kalka/internal/name"
)

// Built-in entry kinds (spec 6.5). Other kinds matching kindRe are preserved.
const (
	KindComment   = "comment"
	KindDeviation = "deviation"
	KindRelease   = "release" // spec 20.4
	KindBudded    = "budded"  // spec 6.11
)

// Sep separates header fields: space, U+00B7, space.
const Sep = " · "

// TimeLayout is the timestamp format of entry headers.
const TimeLayout = "2006-01-02T15:04:05Z"

// MaxEntryBody is the largest entry body accepted (spec 6.9).
const MaxEntryBody = 64 << 10

var (
	kindRe   = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	emailRe  = regexp.MustCompile(`^[^\s@\x{00b7}]+@[^\s@\x{00b7}]+$`)
	tsRe     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)
	revRe    = regexp.MustCompile(`^[0-9a-f]{8}$`)
	idRe     = regexp.MustCompile(`^[0-9a-f]{12}$`)
	sigRe    = regexp.MustCompile(`^[A-Za-z0-9_-]{86}$`)
	nodeIDRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
)

// Entry is one record in the review log. Entries are immutable once written.
type Entry struct {
	Kind      string
	Author    string // as written; compare with SameEmail
	Timestamp string // TimeLayout, UTC
	Anchor    string // without the leading §; "" for a general entry
	Rev       string
	ID        string
	Sig       string // handoff signature, base64url without padding; "" if none

	// Body is the entry body as found on disk, LF line endings, without
	// leading or trailing blank lines. Trailing spaces are kept: they can be
	// Markdown hard breaks.
	Body string
}

// ParseHeader parses a strict entry header line (spec 6.5). The line may
// carry its terminator and trailing whitespace.
func ParseHeader(line string) (*Entry, error) {
	line = strings.TrimRight(content(line), " \t")
	rest, ok := strings.CutPrefix(line, "### ")
	if !ok {
		return nil, errors.New("entry header must start with \"### \"")
	}
	f := strings.Split(rest, Sep)
	if len(f) != 6 && len(f) != 7 {
		return nil, fmt.Errorf("entry header has %d fields separated by \" · \", want 6 (7 with sig)", len(f))
	}
	e := &Entry{Kind: f[0], Author: f[1], Timestamp: f[2]}
	if !kindRe.MatchString(e.Kind) {
		return nil, fmt.Errorf("kind %q must match [a-z][a-z0-9-]*", e.Kind)
	}
	if !emailRe.MatchString(e.Author) {
		return nil, fmt.Errorf("author %q is not an email address", e.Author)
	}
	if !tsRe.MatchString(e.Timestamp) {
		return nil, fmt.Errorf("timestamp %q must be UTC RFC 3339 with second precision and Z", e.Timestamp)
	}
	if _, err := time.Parse(TimeLayout, e.Timestamp); err != nil {
		return nil, fmt.Errorf("timestamp %q is not a valid time", e.Timestamp)
	}
	anchor, ok := strings.CutPrefix(f[3], "§")
	if !ok {
		return nil, fmt.Errorf("anchor field %q must start with §", f[3])
	}
	if err := validAnchor(anchor); err != nil {
		return nil, err
	}
	e.Anchor = anchor
	if e.Rev, ok = strings.CutPrefix(f[4], "rev:"); !ok || !revRe.MatchString(e.Rev) {
		return nil, fmt.Errorf("rev field %q must be rev:<8 hex>", f[4])
	}
	if e.ID, ok = strings.CutPrefix(f[5], "id:"); !ok || !idRe.MatchString(e.ID) {
		return nil, fmt.Errorf("id field %q must be id:<12 hex>", f[5])
	}
	if len(f) == 7 {
		if e.Sig, ok = strings.CutPrefix(f[6], "sig:"); !ok || !sigRe.MatchString(e.Sig) {
			return nil, fmt.Errorf("sig field %q must be sig:<86 base64url characters>", f[6])
		}
	}
	return e, nil
}

func validAnchor(a string) error {
	if a == "" {
		return nil
	}
	slug, node, hasNode := strings.Cut(a, "#")
	if slug != BaseSlug(slug) || strings.ContainsAny(slug, " ") {
		return fmt.Errorf("anchor %q is not a heading slug", a)
	}
	if hasNode && !nodeIDRe.MatchString(node) {
		return fmt.Errorf("anchor %q names an invalid trace node id", a)
	}
	return nil
}

// Header renders the strict header line, without a terminator.
func (e *Entry) Header() string {
	h := "### " + strings.Join([]string{
		e.Kind, e.Author, e.Timestamp, "§" + e.Anchor, "rev:" + e.Rev, "id:" + e.ID,
	}, Sep)
	if e.Sig != "" {
		h += Sep + "sig:" + e.Sig
	}
	return h
}

// ComputeID returns the content hash the id field must equal (spec 6.5).
// The anchor is hashed without its § prefix and the author as written.
func (e *Entry) ComputeID() string { return e.Digest()[:12] }

// Digest is the full SHA-256 of the id input, in hex: what a v2
// signature covers.
func (e *Entry) Digest() string {
	in := strings.Join([]string{e.Kind, e.Author, e.Timestamp, e.Anchor, e.Rev, Normalize(e.Body)}, "\n")
	sum := sha256.Sum256([]byte(in))
	return hex.EncodeToString(sum[:])
}

// Valid reports whether the id matches the content.
func (e *Entry) Valid() bool { return e.ID == e.ComputeID() }

// Less orders entries canonically by (timestamp, id).
func (e *Entry) Less(o *Entry) bool {
	if e.Timestamp != o.Timestamp {
		return e.Timestamp < o.Timestamp
	}
	return e.ID < o.ID
}

// Section returns the heading slug part of the anchor.
func (e *Entry) Section() string {
	s, _, _ := strings.Cut(e.Anchor, "#")
	return s
}

// Node returns the trace node id part of the anchor, if any.
func (e *Entry) Node() string {
	_, n, _ := strings.Cut(e.Anchor, "#")
	return n
}

// ReplyTo returns the id named on a leading "re: <id>" line.
func (e *Entry) ReplyTo() string {
	first, _, _ := strings.Cut(Normalize(e.Body), "\n")
	if id, ok := strings.CutPrefix(first, "re: "); ok && idRe.MatchString(strings.TrimSpace(id)) {
		return strings.TrimSpace(id)
	}
	return ""
}

// Quote returns the text of the leading blockquote, after any re: line.
func (e *Entry) Quote() string {
	lines := strings.Split(Normalize(e.Body), "\n")
	if len(lines) > 0 && e.ReplyTo() != "" {
		lines = lines[1:]
	}
	var q []string
	for _, l := range lines {
		t, ok := strings.CutPrefix(l, ">")
		if !ok {
			break
		}
		q = append(q, strings.TrimPrefix(t, " "))
	}
	return strings.Join(q, "\n")
}

// SigMessage is the message every entry signature covers (spec 6.5): the
// full digest of the entry.
func SigMessage(kalkaID, digest string) []byte {
	return []byte(name.EntrySigContext + "\n" + kalkaID + "\n" + digest)
}

// SigState is the result of checking an entry's signature.
type SigState int

const (
	Unsigned SigState = iota // no sig field
	SigBad                   // a sig that does not verify
	SigOK                    // a valid signature
)

func (s SigState) String() string {
	return [...]string{"unsigned", "invalid", "valid"}[s]
}

// Verify checks the signature against pub. An entry whose id does not
// match its content never verifies.
func (e *Entry) Verify(kalkaID string, pub ed25519.PublicKey) SigState {
	if e.Sig == "" {
		return Unsigned
	}
	sig, err := base64.RawURLEncoding.DecodeString(e.Sig)
	if err != nil || len(pub) != ed25519.PublicKeySize || !e.Valid() {
		return SigBad
	}
	if ed25519.Verify(pub, SigMessage(kalkaID, e.Digest()), sig) {
		return SigOK
	}
	return SigBad
}

// VerifySig reports whether the entry's signature verifies against pub.
func (e *Entry) VerifySig(kalkaID string, pub ed25519.PublicKey) bool {
	return e.Verify(kalkaID, pub) == SigOK
}

// Sign sets a v2 signature. Only entries the local user writes are signed
// (spec 6.5).
func (e *Entry) Sign(kalkaID string, sign Signer) {
	e.Sig = base64.RawURLEncoding.EncodeToString(sign(SigMessage(kalkaID, e.Digest())))
}

// Signer signs a message with the local identity key.
type Signer func(msg []byte) []byte

// NewEntry builds an entry written now by author. The body is prepared as
// user input (PrepareBody) and the id is computed.
func NewEntry(kind, author string, at time.Time, anchor, rev, body string) (*Entry, error) {
	if !kindRe.MatchString(kind) {
		return nil, fmt.Errorf("kind %q must match [a-z][a-z0-9-]*", kind)
	}
	if !emailRe.MatchString(author) {
		return nil, fmt.Errorf("author %q is not an email address", author)
	}
	if err := validAnchor(anchor); err != nil {
		return nil, err
	}
	if !revRe.MatchString(rev) {
		return nil, fmt.Errorf("rev %q is not 8 hex characters", rev)
	}
	b, err := PrepareBody(body)
	if err != nil {
		return nil, err
	}
	e := &Entry{
		Kind: kind, Author: author, Timestamp: at.UTC().Format(TimeLayout),
		Anchor: anchor, Rev: rev, Body: b,
	}
	e.ID = e.ComputeID()
	return e, nil
}

// PrepareBody turns user input into an entry body: LF endings, outer blank
// lines removed, headings shallower than level 4 demoted to level 4, an
// unclosed code fence closed, size limit enforced.
func PrepareBody(body string) (string, error) {
	lines := strings.Split(toLF(strings.TrimPrefix(body, "\uFEFF")), "\n")
	var out []string
	var fence fenceState
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if fence.step(l) || fence.open {
			out = append(out, l)
			continue
		}
		if m := atxRe.FindStringSubmatch(l); m != nil && len(m[1]) < 4 {
			out = append(out, "#### "+strings.TrimSpace(m[2]))
			continue
		}
		// Setext heading: a text line followed by === or ---.
		if i+1 < len(lines) && strings.TrimSpace(l) != "" && setextRe.MatchString(lines[i+1]) && isParagraphLine(l) {
			out = append(out, "#### "+strings.TrimSpace(l))
			i++
			continue
		}
		out = append(out, l)
	}
	out = trimBlankLines(out)
	if fence.open {
		out = append(out, fence.closer())
	}
	b := strings.Join(out, "\n")
	if len(b) > MaxEntryBody {
		return "", fmt.Errorf("entry body is %d bytes, the limit is %d", len(b), MaxEntryBody)
	}
	if strings.TrimSpace(b) == "" {
		return "", errors.New("entry body is empty")
	}
	return b, nil
}

var (
	atxRe    = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?[ \t]*$`)
	setextRe = regexp.MustCompile(`^ {0,3}(=+|-+)[ \t]*$`)
	fenceRe  = regexp.MustCompile("^( {0,3})(`{3,}|~{3,})(.*)$")
)

func isParagraphLine(l string) bool {
	t := strings.TrimLeft(l, " ")
	if len(l)-len(t) >= 4 {
		return false
	}
	for _, p := range []string{">", "- ", "* ", "+ ", "#", "|"} {
		if strings.HasPrefix(t, p) {
			return false
		}
	}
	return true
}

// fenceState tracks fenced code blocks line by line (CommonMark rules for
// opening and closing fences).
type fenceState struct {
	open bool
	char byte
	n    int
}

// step consumes one line. It returns true if the line opened or closed a fence.
func (f *fenceState) step(line string) bool {
	m := fenceRe.FindStringSubmatch(content(line))
	if m == nil {
		return false
	}
	run := m[2]
	if !f.open {
		if run[0] == '`' && strings.Contains(m[3], "`") {
			return false
		}
		f.open, f.char, f.n = true, run[0], len(run)
		return true
	}
	if run[0] == f.char && len(run) >= f.n && strings.TrimSpace(m[3]) == "" {
		f.open = false
		return true
	}
	return false
}

func (f *fenceState) closer() string { return strings.Repeat(string(f.char), f.n) }

// SameEmail compares email addresses case-insensitively (spec 6.2).
func SameEmail(a, b string) bool { return strings.EqualFold(a, b) }

// ValidEmail reports whether s looks like an email address.
func ValidEmail(s string) bool { return emailRe.MatchString(s) }
