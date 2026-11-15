package teamlog

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/golbinski/kalka/internal/name"
)

// Invite is what an invite token carries (spec 19.7). The issuing owner
// keeps the same record in invites.yaml.
type Invite struct {
	TeamName   string    `json:"name"`
	Team       string    `json:"team"`
	Owner      string    `json:"owner"`   // the inviting owner's public key
	Inviter    string    `json:"inviter"` // the inviting owner's email: a claim until the join completes
	Email      string    `json:"email"`
	Secret     []byte    `json:"secret"`
	Expires    time.Time `json:"expires"`
	Rendezvous string    `json:"rendezvous"`
}

// DefaultInviteTTL is how long an invite is valid unless --expires says
// otherwise.
const DefaultInviteTTL = 72 * time.Hour

var tokenEnc = base32.StdEncoding.WithPadding(base32.NoPadding)

// Invite errors; the joiner's UI shows which one ended the attempt.
var (
	ErrInviteExpired   = errors.New("the invite has expired")
	ErrInviteProof     = errors.New("the invite proof does not match")
	ErrInviteWrongTeam = errors.New("the invite is for another team")
	ErrInviteToken     = errors.New("not a valid invite token")
)

// Token encodes the invite as one line of base32: the context, the JSON
// record and a 4-byte checksum, so a token damaged in transit is refused
// before anything is dialed.
func (iv Invite) Token() string {
	body, _ := json.Marshal(iv)
	msg := append([]byte(name.InviteContext+"\n"), body...)
	sum := sha256.Sum256(msg)
	return tokenEnc.EncodeToString(append(msg, sum[:4]...))
}

// ParseToken decodes and checks a token. Whitespace and case are ignored,
// since tokens are pasted from chat and mail.
func ParseToken(s string) (Invite, error) {
	var iv Invite
	s = strings.ToUpper(strings.Join(strings.Fields(s), ""))
	raw, err := tokenEnc.DecodeString(s)
	if err != nil || len(raw) < 4 {
		return iv, ErrInviteToken
	}
	msg, check := raw[:len(raw)-4], raw[len(raw)-4:]
	sum := sha256.Sum256(msg)
	if subtle.ConstantTimeCompare(sum[:4], check) != 1 {
		return iv, ErrInviteToken
	}
	body, ok := bytes.CutPrefix(msg, []byte(name.InviteContext+"\n"))
	if !ok {
		return iv, ErrInviteToken
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&iv); err != nil {
		return iv, ErrInviteToken
	}
	if len(iv.Secret) != 32 || !ValidTeamName(iv.TeamName) || !hashRe.MatchString(iv.Team) || !emailRe.MatchString(iv.Email) || !emailRe.MatchString(iv.Inviter) {
		return iv, ErrInviteToken
	}
	if _, err := DecodeKey(iv.Owner); err != nil {
		return iv, ErrInviteToken
	}
	return iv, nil
}

// LinkPrefix starts a join link (spec 19.7): kalka://join/<token>.
const LinkPrefix = "kalka://join/"

// MaxJoinInput bounds what ParseJoin reads: a link or a token is pasted
// from anywhere, and raised by any web page.
const MaxJoinInput = 4 << 10

// Link is the invite as a kalka://join/ link.
func (iv Invite) Link() string { return LinkPrefix + iv.Token() }

// NormalizeJoin reduces what a user gives, a kalka://join/<token> link or
// a bare token, to the token in upper case (spec 19.7, "Parsing"): any
// letter case, surrounding whitespace, angle brackets as chat tools add
// them, and line breaks inside, as mail wraps long lines. A single
// trailing slash, which some systems add to a link, is dropped. Another
// scheme, another path, extra path parts, a query, a fragment, or input
// over 4 KiB are refused. The checksum is ParseJoin's.
func NormalizeJoin(s string) (string, error) {
	if len(s) > MaxJoinInput {
		return "", ErrInviteToken
	}
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	s = strings.Join(strings.Fields(s), "")
	if i := strings.Index(s, ":"); i >= 0 {
		if !strings.EqualFold(s[:min(len(s), len(LinkPrefix))], LinkPrefix) {
			return "", ErrInviteToken
		}
		s = strings.TrimSuffix(s[len(LinkPrefix):], "/")
	}
	if s == "" {
		return "", ErrInviteToken
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '2' && c <= '7') {
			return "", ErrInviteToken
		}
	}
	return strings.ToUpper(s), nil
}

// ParseJoin parses a join link or a bare token: NormalizeJoin, then the
// token's checksum and record. `kalka join`, the paste box and a link
// opened from the system all go through it.
func ParseJoin(s string) (Invite, error) {
	t, err := NormalizeJoin(s)
	if err != nil {
		return Invite{}, err
	}
	return ParseToken(t)
}

// ID is the invite's public identifier: it appears in add-member and in
// the joiner's hello, and reveals nothing about the secret.
func (iv Invite) ID() string {
	sum := sha256.Sum256(append([]byte(name.InviteContext+" id\n"), iv.Secret...))
	return hex.EncodeToString(sum[:16])
}

// Proof is what the joiner sends in hello: an HMAC under the secret over
// the team and both public keys, so it is bound to this connection's
// parties (spec 19.7 step 4).
func Proof(secret []byte, team, ownerKey, joinerKey string) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(name.InviteContext + " proof\n" + team + "\n" + ownerKey + "\n" + joinerKey))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Check is the owner's side: the invite record it stored, the team and
// keys of the connection, and the proof the joiner sent. Whether the
// invite was already used is the caller's (invites.yaml) and the replay's
// (rule 7) concern.
func (iv Invite) Check(team, ownerKey, joinerKey, proof string, now time.Time) error {
	if team != iv.Team {
		return ErrInviteWrongTeam
	}
	if !now.Before(iv.Expires) {
		return ErrInviteExpired
	}
	want := Proof(iv.Secret, iv.Team, ownerKey, joinerKey)
	if ownerKey != iv.Owner || !hmac.Equal([]byte(want), []byte(proof)) {
		return ErrInviteProof
	}
	return nil
}
