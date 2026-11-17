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

	"github.com/golbinski/kalka/internal/addr"
	"github.com/golbinski/kalka/internal/name"
)

// RuleVersion is the replay rule version of every operation this package
// writes and replays. An operation of a higher version is verified, kept
// as its line and passed on, and pending with everything built on it.
const RuleVersion = 2

// Operation kinds (spec 19.1).
const (
	CreateTeam       = "create-team"
	AddMember        = "add-member"
	RemoveMember     = "remove-member"
	AddOwner         = "add-owner"
	RemoveOwner      = "remove-owner"
	ReassignDocument = "reassign-document"
	AcceptTransfer   = "accept-transfer"
	DeclineTransfer  = "decline-transfer"
)

// MaxLine bounds one encoded operation, so a hostile peer cannot hand over
// a huge line.
const MaxLine = 8 << 10

// Args are the arguments of an operation. Which fields an operation
// carries is fixed by its kind (argKeys); After and Plan are meaningful
// only for reassign-document: the highest release the signer held and its
// plan hash, 0 and "" when it held none. Transfer is the digest of an
// offer's transfer entry (spec 20.7).
type Args struct {
	Name     string
	Email    string
	PubKey   string
	Invite   string
	Owner    string
	Doc      string
	From     string
	After    int64
	Plan     string
	Transfer string
}

// argKeys lists the required keys of each kind; "invite" is the only
// optional key (add-member).
var argKeys = map[string][]string{
	CreateTeam:       {"email", "name"},
	AddMember:        {"email", "pubkey"},
	RemoveMember:     {"email"},
	AddOwner:         {"email"},
	RemoveOwner:      {"email"},
	ReassignDocument: {"after", "doc", "from", "owner", "plan"},
	AcceptTransfer:   {"doc", "owner", "transfer"},
	DeclineTransfer:  {"doc", "transfer"},
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

	// raw is the line of an operation of a rule version this package does
	// not implement, kept byte for byte; Kind and Args are then empty.
	raw []byte
}

// Supported reports whether the operation's rule version is the one this
// package implements; only then are Kind and Args set.
func (o *Op) Supported() bool { return o.raw == nil }

var (
	teamNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	hashRe     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	inviteIDRe = regexp.MustCompile(`^[0-9a-f]{32}$`)
	emailRe    = regexp.MustCompile(`^[^\s@]+@[^\s@]+$`)
)

// ValidTeamName reports whether s can name a team (spec 19.2).
func ValidTeamName(s string) bool { return teamNameRe.MatchString(s) }

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
	case "plan":
		return a.Plan
	case "owner":
		return a.Owner
	case "doc":
		return a.Doc
	case "from":
		return a.From
	case "after":
		return a.After
	case "transfer":
		return a.Transfer
	}
	return nil
}

// canonical writes the canonical encoding (docs/teamlog.md): JSON, keys in
// lexicographic byte order, no insignificant whitespace, no HTML escaping,
// integers only. withSig adds the signature, for the file line.
func (o *Op) canonical(withSig bool) []byte {
	var b bytes.Buffer
	str := func(s string) { writeString(&b, s) }
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

// Hash is the operation's identity: hex SHA-256 of its line without the
// sig member. Lines are canonical byte for byte, so for an operation this
// package implements that is its canonical encoding without the
// signature, and for any other it is computed without knowing the kind.
func (o *Op) Hash() string {
	var sum [32]byte
	if o.raw != nil {
		sum = sha256.Sum256(withoutSig(o.raw))
	} else {
		sum = sha256.Sum256(o.canonical(false))
	}
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
func (o *Op) Encode() []byte {
	if o.raw != nil {
		return append([]byte(nil), o.raw...)
	}
	return o.canonical(true)
}

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
	// One line per operation: whitespace, escaping, key order and
	// duplicate keys cannot make two lines of one operation.
	c, err := canonicalJSON(line)
	if err != nil {
		return o, fmt.Errorf("operation: %w", err)
	}
	if !bytes.Equal(c, line) {
		return o, errors.New("operation: not in canonical form")
	}
	if v, err := ruleVersion(line); err != nil {
		return o, err
	} else if v > RuleVersion {
		return decodeNewer(line, v)
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
		return o, fmt.Errorf("operation: rule version %d is no longer read", v)
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
		case "plan":
			o.Args.Plan = s
		case "owner":
			o.Args.Owner = s
		case "doc":
			o.Args.Doc = s
		case "from":
			o.Args.From = s
		case "transfer":
			o.Args.Transfer = s
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

// ruleVersion reads v from a line.
func ruleVersion(line []byte) (int, error) {
	var w struct {
		V *json.Number `json:"v"`
	}
	d := json.NewDecoder(bytes.NewReader(line))
	d.UseNumber()
	if err := d.Decode(&w); err != nil || w.V == nil {
		return 0, errors.New("operation: missing rule version")
	}
	v, err := strconv.Atoi(w.V.String())
	if err != nil || v < 1 {
		return 0, errors.New("operation: bad rule version")
	}
	return v, nil
}

// decodeNewer reads an operation of a rule version above RuleVersion:
// only what verifying, storing and passing it on needs. Kind and Args are
// left empty; replay holds it as pending.
func decodeNewer(line []byte, v int) (Op, error) {
	var w struct {
		Team   *string  `json:"team"`
		Prev   []string `json:"prev"`
		Signer *string  `json:"signer"`
		At     string   `json:"at"`
		Sig    *string  `json:"sig"`
	}
	if err := json.Unmarshal(line, &w); err != nil {
		return Op{}, fmt.Errorf("operation: %w", err)
	}
	if w.Team == nil || w.Prev == nil || w.Signer == nil || w.Sig == nil {
		return Op{}, errors.New("operation: missing field")
	}
	o := Op{V: v, Team: *w.Team, Prev: w.Prev, Signer: *w.Signer, At: w.At, Sig: *w.Sig, raw: append([]byte(nil), line...)}
	if _, err := DecodeKey(o.Signer); err != nil {
		return Op{}, errors.New("operation: bad signer key")
	}
	if !hashRe.MatchString(o.Team) {
		return Op{}, errors.New("operation: bad team hash")
	}
	for _, p := range o.Prev {
		if !hashRe.MatchString(p) {
			return Op{}, errors.New("operation: bad prev hash")
		}
	}
	if !equalStrings(o.Prev, normPrev(o.Prev)) {
		return Op{}, errors.New("operation: prev not sorted or has duplicates")
	}
	return o, nil
}

// withoutSig is a canonical line with its sig member removed.
func withoutSig(line []byte) []byte {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(line, &m); err != nil {
		return line
	}
	delete(m, "sig")
	b, _ := json.Marshal(m) // only to rebuild; canonicalJSON fixes the form
	c, err := canonicalJSON(b)
	if err != nil {
		return line
	}
	return c
}

var intRe = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

// canonicalJSON re-encodes one JSON value in the canonical encoding of
// docs/teamlog.md: object keys in byte order, no whitespace, strings as
// the canonical encoder writes them, integers only. A duplicate key is an
// error.
func canonicalJSON(in []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(in))
	dec.UseNumber()
	var b bytes.Buffer
	if err := canonValue(dec, &b); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err == nil {
		return nil, errors.New("trailing data")
	}
	return b.Bytes(), nil
}

func canonValue(dec *json.Decoder, b *bytes.Buffer) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			members := map[string][]byte{}
			var keys []string
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return err
				}
				k := kt.(string)
				if _, dup := members[k]; dup {
					return fmt.Errorf("key %q appears twice", k)
				}
				var v bytes.Buffer
				if err := canonValue(dec, &v); err != nil {
					return err
				}
				members[k] = v.Bytes()
				keys = append(keys, k)
			}
			if _, err := dec.Token(); err != nil {
				return err
			}
			sort.Strings(keys)
			b.WriteByte('{')
			for i, k := range keys {
				if i > 0 {
					b.WriteByte(',')
				}
				writeString(b, k)
				b.WriteByte(':')
				b.Write(members[k])
			}
			b.WriteByte('}')
		case '[':
			b.WriteByte('[')
			for i := 0; dec.More(); i++ {
				if i > 0 {
					b.WriteByte(',')
				}
				if err := canonValue(dec, b); err != nil {
					return err
				}
			}
			if _, err := dec.Token(); err != nil {
				return err
			}
			b.WriteByte(']')
		}
	case string:
		writeString(b, t)
	case json.Number:
		if !intRe.MatchString(t.String()) {
			return fmt.Errorf("number %s is not an integer", t)
		}
		b.WriteString(t.String())
	case bool:
		b.WriteString(strconv.FormatBool(t))
	case nil:
		b.WriteString("null")
	}
	return nil
}

// writeString writes s as the canonical encoding does: as Go's
// encoding/json with HTML escaping off.
func writeString(b *bytes.Buffer, s string) {
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
		// One spelling per address: replay compares emails as bytes.
		if !addr.IsCanonical(s) {
			return fmt.Errorf("operation: email %q is not in canonical form (ASCII letters in lower case)", s)
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
	case AcceptTransfer, DeclineTransfer:
		if !validDoc(a.Doc) {
			return errors.New("operation: bad document id")
		}
		if !hashRe.MatchString(a.Transfer) {
			return errors.New("operation: bad transfer digest")
		}
		if o.Kind == AcceptTransfer {
			return email(a.Owner)
		}
	case ReassignDocument:
		if !validDoc(a.Doc) {
			return errors.New("operation: bad document id")
		}
		// The plan hash of release after; none before the first release.
		if (a.After == 0) != (a.Plan == "") || (a.Plan != "" && !hashRe.MatchString(a.Plan)) {
			return errors.New("operation: bad plan hash for the cut-off")
		}
		if err := email(a.From); err != nil {
			return err
		}
		return email(a.Owner)
	}
	return nil
}

func validDoc(s string) bool {
	return s != "" && len(s) <= 64 && !strings.ContainsAny(s, " \t\r\n")
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
