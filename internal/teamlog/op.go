// Package teamlog implements the team log (spec 19): a signed,
// append-only set of operations whose state is computed by a deterministic
// replay. It does no I/O and holds no clock: callers pass times in.
//
// docs/teamlog.md fixes the canonical encoding and the replay rules.
package teamlog

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/golbinski/kalka/internal/name"
)

// RuleVersion is the replay rule version of every operation this package
// writes. An operation with another version is held as unsupported.
const RuleVersion = 1

// Operation kinds (spec 19.1).
const (
	CreateTeam       = "create-team"
	AddMember        = "add-member"
	RemoveMember     = "remove-member"
	AddOwner         = "add-owner"
	RemoveOwner      = "remove-owner"
	AssignDomain     = "assign-domain"
	ReassignDocument = "reassign-document"
)

// MaxLine bounds one encoded operation, so a hostile peer cannot hand over
// a huge line.
const MaxLine = 8 << 10

// Args are the arguments of an operation. Which fields an operation
// carries is fixed by its kind (argKeys); After is meaningful only for
// assign-domain and reassign-document, where 0 means "no release seen".
type Args struct {
	Name   string
	Email  string
	PubKey string
	Invite string
	Domain string
	Owner  string
	Doc    string
	From   string
	After  int64
}

// argKeys lists the required keys of each kind; "invite" is the only
// optional key (add-member).
var argKeys = map[string][]string{
	CreateTeam:       {"email", "name"},
	AddMember:        {"email", "pubkey"},
	RemoveMember:     {"email"},
	AddOwner:         {"email"},
	RemoveOwner:      {"email"},
	AssignDomain:     {"after", "domain", "owner"},
	ReassignDocument: {"after", "doc", "from", "owner"},
}

// Op is one team log operation.
type Op struct {
	V      int
	Team   string // the team hash; empty in create-team
	Kind   string
	Args   Args
	Prev   []string // hashes, sorted, no duplicates
	Signer string   // ed25519 public key, base64url without padding
	At     string   // the signer's clock, RFC 3339; display only
	Sig    string   // base64url without padding
}

var (
	teamNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	hashRe     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	inviteIDRe = regexp.MustCompile(`^[0-9a-f]{32}$`)
	emailRe    = regexp.MustCompile(`^[^\s@]+@[^\s@]+$`)
)

// ValidTeamName reports whether s can name a team (spec 19.2).
func ValidTeamName(s string) bool { return teamNameRe.MatchString(s) }

// ValidDomainName uses the team name rule: it appears in file names and
// in `domain:` references.
func ValidDomainName(s string) bool { return teamNameRe.MatchString(s) }

func value(a Args, key string) any {
	switch key {
	case "name":
		return a.Name
	case "email":
		return a.Email
	case "pubkey":
		return a.PubKey
	case "invite":
		return a.Invite
	case "domain":
		return a.Domain
	case "owner":
		return a.Owner
	case "doc":
		return a.Doc
	case "from":
		return a.From
	case "after":
		return a.After
	}
	return nil
}

// canonical writes the canonical encoding (docs/teamlog.md): JSON, keys in
// lexicographic byte order, no insignificant whitespace, no HTML escaping,
// integers only. withSig adds the signature, for the file line.
func (o *Op) canonical(withSig bool) []byte {
	var b bytes.Buffer
	str := func(s string) {
		if plainASCII(s) {
			b.WriteByte('"')
			b.WriteString(s)
			b.WriteByte('"')
			return
		}
		var e bytes.Buffer
		enc := json.NewEncoder(&e)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(s)
		b.Write(bytes.TrimSuffix(e.Bytes(), []byte("\n")))
	}
	keys := append([]string(nil), argKeys[o.Kind]...)
	if o.Kind == AddMember && o.Args.Invite != "" {
		keys = append(keys, "invite")
	}
	sort.Strings(keys)
	b.WriteString(`{"args":{`)
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		str(k)
		b.WriteByte(':')
		switch v := value(o.Args, k).(type) {
		case string:
			str(v)
		case int64:
			b.WriteString(strconv.FormatInt(v, 10))
		}
	}
	b.WriteString(`},"at":`)
	str(o.At)
	b.WriteString(`,"op":`)
	str(o.Kind)
	b.WriteString(`,"prev":[`)
	for i, p := range o.Prev {
		if i > 0 {
			b.WriteByte(',')
		}
		str(p)
	}
	b.WriteString(`]`)
	if withSig {
		b.WriteString(`,"sig":`)
		str(o.Sig)
	}
	b.WriteString(`,"signer":`)
	str(o.Signer)
	b.WriteString(`,"team":`)
	str(o.Team)
	b.WriteString(`,"v":`)
	b.WriteString(strconv.Itoa(o.V))
	b.WriteByte('}')
	return b.Bytes()
}

// plainASCII reports whether encoding/json would write s unchanged
// between quotes.
func plainASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c > 0x7e || c == '"' || c == '\\' {
			return false
		}
	}
	return true
}

// Hash is the operation's identity: hex SHA-256 of its canonical encoding
// without the signature.
func (o *Op) Hash() string {
	sum := sha256.Sum256(o.canonical(false))
	return hex.EncodeToString(sum[:])
}

// SigMessage is the message an operation's signature covers.
func SigMessage(hash string) []byte {
	return []byte(name.TeamLogSigContext + "\n" + hash)
}

// EncodeKey is how public keys appear in the log.
func EncodeKey(k ed25519.PublicKey) string { return base64.RawURLEncoding.EncodeToString(k) }

// DecodeKey parses a public key of the log.
func DecodeKey(s string) (ed25519.PublicKey, error) {
	k, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(k) != ed25519.PublicKeySize {
		return nil, errors.New("not an ed25519 public key")
	}
	return k, nil
}

// Sign sets Signer and Sig. Prev is normalized first, since it is part of
// the hash.
func (o *Op) Sign(priv ed25519.PrivateKey) {
	o.Prev = normPrev(o.Prev)
	o.Signer = EncodeKey(priv.Public().(ed25519.PublicKey))
	o.Sig = base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, SigMessage(o.Hash())))
}

// VerifySig checks the signature against Signer.
func (o *Op) VerifySig() bool {
	k, err := DecodeKey(o.Signer)
	if err != nil {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(o.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(k, SigMessage(o.Hash()), sig)
}

func normPrev(p []string) []string {
	out := append([]string(nil), p...)
	sort.Strings(out)
	n := 0
	for i, h := range out {
		if i == 0 || h != out[n-1] {
			out[n] = h
			n++
		}
	}
	return out[:n]
}

// Encode returns the file line of an operation, without newline.
func (o *Op) Encode() []byte { return o.canonical(true) }

type wireOp struct {
	V      *json.Number               `json:"v"`
	Team   *string                    `json:"team"`
	Op     *string                    `json:"op"`
	Args   map[string]json.RawMessage `json:"args"`
	Prev   []string                   `json:"prev"`
	Signer *string                    `json:"signer"`
	At     *string                    `json:"at"`
	Sig    *string                    `json:"sig"`
}

// Decode parses one line. It checks the shape of the operation (fields,
// argument keys and types, hash and key formats) but not its signature
// or whether it is valid in the log; Verify and Replay do that. An
// operation whose rule version is not RuleVersion decodes, with its
// arguments unchecked beyond their JSON types, so it can be kept and
// passed on.
func Decode(line []byte) (Op, error) {
	var o Op
	if len(line) > MaxLine {
		return o, errors.New("operation too long")
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	var w wireOp
	if err := dec.Decode(&w); err != nil {
		return o, fmt.Errorf("operation: %w", err)
	}
	if dec.More() {
		return o, errors.New("operation: trailing data")
	}
	if w.V == nil || w.Team == nil || w.Op == nil || w.Args == nil || w.Prev == nil || w.Signer == nil || w.At == nil || w.Sig == nil {
		return o, errors.New("operation: missing field")
	}
	v, err := strconv.Atoi(w.V.String())
	if err != nil || v < 1 {
		return o, errors.New("operation: bad rule version")
	}
	o = Op{V: v, Team: *w.Team, Kind: *w.Op, Prev: w.Prev, Signer: *w.Signer, At: *w.At, Sig: *w.Sig}
	keys, known := argKeys[o.Kind]
	if v != RuleVersion {
		return o, errUnsupported
	}
	if !known {
		return o, fmt.Errorf("operation: unknown kind %q", o.Kind)
	}
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	for k := range w.Args {
		if !want[k] && !(o.Kind == AddMember && k == "invite") {
			return o, fmt.Errorf("operation: unexpected argument %q", k)
		}
	}
	for _, k := range keys {
		if _, ok := w.Args[k]; !ok {
			return o, fmt.Errorf("operation: missing argument %q", k)
		}
	}
	for k, raw := range w.Args {
		if k == "after" {
			var n json.Number
			d := json.NewDecoder(bytes.NewReader(raw))
			d.UseNumber()
			if err := d.Decode(&n); err != nil {
				return o, errors.New("operation: after is not a number")
			}
			i, err := strconv.ParseInt(n.String(), 10, 64)
			if err != nil || i < 0 {
				return o, errors.New("operation: after is not a version")
			}
			o.Args.After = i
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return o, fmt.Errorf("operation: argument %q is not a string", k)
		}
		switch k {
		case "name":
			o.Args.Name = s
		case "email":
			o.Args.Email = s
		case "pubkey":
			o.Args.PubKey = s
		case "invite":
			o.Args.Invite = s
		case "domain":
			o.Args.Domain = s
		case "owner":
			o.Args.Owner = s
		case "doc":
			o.Args.Doc = s
		case "from":
			o.Args.From = s
		}
	}
	if err := o.checkShape(); err != nil {
		return o, err
	}
	// Prev must already be canonical: a reordered list would hash the
	// same and let two lines stand for one operation.
	if !equalStrings(o.Prev, normPrev(o.Prev)) {
		return o, errors.New("operation: prev not sorted or has duplicates")
	}
	return o, nil
}

var errUnsupported = errors.New("operation: unsupported rule version")

// IsUnsupported reports whether Decode refused an operation only for its
// rule version.
func IsUnsupported(err error) bool { return errors.Is(err, errUnsupported) }

func (o *Op) checkShape() error {
	if _, err := DecodeKey(o.Signer); err != nil {
		return errors.New("operation: bad signer key")
	}
	for _, p := range o.Prev {
		if !hashRe.MatchString(p) {
			return errors.New("operation: bad prev hash")
		}
	}
	if o.Kind == CreateTeam {
		if o.Team != "" || len(o.Prev) != 0 {
			return errors.New("operation: create-team has a team or predecessors")
		}
		if !ValidTeamName(o.Args.Name) {
			return errors.New("operation: bad team name")
		}
	} else {
		if !hashRe.MatchString(o.Team) || len(o.Prev) == 0 {
			return errors.New("operation: missing team or predecessors")
		}
	}
	a := o.Args
	email := func(s string) error {
		if len(s) > 254 || !emailRe.MatchString(s) {
			return fmt.Errorf("operation: bad email %q", s)
		}
		return nil
	}
	switch o.Kind {
	case CreateTeam, RemoveMember, AddOwner, RemoveOwner:
		return email(a.Email)
	case AddMember:
		if _, err := DecodeKey(a.PubKey); err != nil {
			return errors.New("operation: bad member key")
		}
		if a.Invite != "" && !inviteIDRe.MatchString(a.Invite) {
			return errors.New("operation: bad invite id")
		}
		return email(a.Email)
	case AssignDomain:
		if !ValidDomainName(a.Domain) {
			return errors.New("operation: bad domain name")
		}
		return email(a.Owner)
	case ReassignDocument:
		if a.Doc == "" || len(a.Doc) > 64 || strings.ContainsAny(a.Doc, " \t\r\n") {
			return errors.New("operation: bad document id")
		}
		if err := email(a.From); err != nil {
			return err
		}
		return email(a.Owner)
	}
	return nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
