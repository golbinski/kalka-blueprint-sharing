// Package identity holds the local key pair, the pinned keys of known peers
// and the verification code (spec 8.1-8.3).
package identity

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/name"
)

// KeyFile and PeersFile live in the config directory.
const (
	KeyFile   = "identity.key"
	PeersFile = "peers.yaml"
)

const keyHeader = name.Product + "-identity-v1"

// LoadOrCreateKey reads identity.key from dir, generating it on first run.
func LoadOrCreateKey(dir string) (ed25519.PrivateKey, error) {
	path := filepath.Join(dir, KeyFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		seed := make([]byte, ed25519.SeedSize)
		if _, err := rand.Read(seed); err != nil {
			return nil, err
		}
		data := keyHeader + "\n" + base64.StdEncoding.EncodeToString(seed) + "\n"
		if err := fsutil.WritePrivate(path, []byte(data)); err != nil {
			return nil, err
		}
		return ed25519.NewKeyFromSeed(seed), nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Fields(string(raw))
	if len(lines) != 2 || lines[0] != keyHeader {
		return nil, fmt.Errorf("%s is not a %s identity key", path, name.Product)
	}
	seed, err := base64.StdEncoding.DecodeString(lines[1])
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s is damaged", path)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// Code is the verification code for a pair of keys (spec 8.3): the same on
// both sides, shown as XXXX-XXXX.
func Code(a, b ed25519.PublicKey) string {
	lo, hi := a, b
	if bytes.Compare(lo, hi) > 0 {
		lo, hi = hi, lo
	}
	h := sha256.New()
	h.Write([]byte(name.Product + "-sas-v1"))
	h.Write(lo)
	h.Write(hi)
	s := base32.StdEncoding.EncodeToString(h.Sum(nil))[:8]
	return s[:4] + "-" + s[4:]
}

// Peer is one entry of peers.yaml.
type Peer struct {
	Email     string `yaml:"email" json:"email"`
	PubKey    string `yaml:"pubkey" json:"pubkey"`
	Verified  bool   `yaml:"verified" json:"verified"`
	FirstSeen string `yaml:"first_seen" json:"first_seen"`
	// KeyChanged records a different key presented later. While it is set,
	// syncs with this email are refused (spec 8.2).
	KeyChanged *Changed `yaml:"key_changed,omitempty" json:"key_changed,omitempty"`
	// Caps are the capabilities the peer announced at the last contact
	// (spec 9.5); none for an older kalka.
	Caps []string `yaml:"caps,omitempty" json:"caps,omitempty"`
}

// Changed is a refused key.
type Changed struct {
	PubKey string `yaml:"pubkey" json:"pubkey"`
	Seen   string `yaml:"seen" json:"seen"`
}

// Key decodes the pinned key.
func (p Peer) Key() ed25519.PublicKey {
	k, err := base64.StdEncoding.DecodeString(p.PubKey)
	if err != nil || len(k) != ed25519.PublicKeySize {
		return nil
	}
	return k
}

// KeyChangedError means a peer presented a key other than the pinned one.
type KeyChangedError struct{ Email string }

func (e *KeyChangedError) Error() string {
	return fmt.Sprintf("%s presented a different identity key than the one pinned at first contact. "+
		"This happens after a reinstall, or when someone else is using that email. Syncs with %[1]s are blocked: "+
		"confirm with them in person or by phone, then remove the old pin (`%s peers forget %[1]s`) and verify again", e.Email, name.Binary)
}

// Peers is the pinned key store backed by peers.yaml.
type Peers struct {
	path string
	now  func() time.Time

	mu    sync.Mutex
	peers []Peer
}

type peersFile struct {
	Peers []Peer `yaml:"peers"`
}

// OpenPeers loads peers.yaml from dir; a missing file is an empty store.
func OpenPeers(dir string, now func() time.Time) (*Peers, error) {
	if now == nil {
		now = time.Now
	}
	p := &Peers{path: filepath.Join(dir, PeersFile), now: now}
	raw, err := os.ReadFile(p.path)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return nil, err
	}
	var f peersFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", p.path, err)
	}
	p.peers = f.Peers
	return p, nil
}

func (p *Peers) find(email string) int {
	return slices.IndexFunc(p.peers, func(x Peer) bool { return strings.EqualFold(x.Email, email) })
}

func (p *Peers) save() error {
	data, err := yaml.Marshal(peersFile{Peers: p.peers})
	if err != nil {
		return err
	}
	return fsutil.WritePrivate(p.path, data)
}

// Get returns the entry for email.
func (p *Peers) Get(email string) (Peer, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if i := p.find(email); i >= 0 {
		return p.peers[i], true
	}
	return Peer{}, false
}

// List returns all entries sorted by email.
func (p *Peers) List() []Peer {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := slices.Clone(p.peers)
	slices.SortFunc(out, func(a, b Peer) int { return strings.Compare(strings.ToLower(a.Email), strings.ToLower(b.Email)) })
	return out
}

// Key returns the pinned key for email.
func (p *Peers) Key(email string) (ed25519.PublicKey, bool) {
	e, ok := p.Get(email)
	if !ok {
		return nil, false
	}
	k := e.Key()
	return k, k != nil
}

// Pin accepts key for email: it is stored unverified on first contact, and
// must match on every later contact. A different key is recorded and
// refused with *KeyChangedError until the pin is removed.
func (p *Peers) Pin(email string, key ed25519.PublicKey) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	enc := base64.StdEncoding.EncodeToString(key)
	i := p.find(email)
	if i < 0 {
		p.peers = append(p.peers, Peer{Email: strings.ToLower(email), PubKey: enc, FirstSeen: p.now().UTC().Format("2006-01-02")})
		return p.save()
	}
	if p.peers[i].PubKey == enc {
		if p.peers[i].KeyChanged != nil {
			return &KeyChangedError{Email: email}
		}
		return nil
	}
	p.peers[i].KeyChanged = &Changed{PubKey: enc, Seen: p.now().UTC().Format(time.RFC3339)}
	if err := p.save(); err != nil {
		return err
	}
	return &KeyChangedError{Email: email}
}

// SetVerified marks the pinned key as confirmed by code comparison. key
// must be the pinned key, so a confirmation cannot apply to a key that
// replaced it in the meantime.
func (p *Peers) SetVerified(email string, key ed25519.PublicKey) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	i := p.find(email)
	if i < 0 || p.peers[i].PubKey != base64.StdEncoding.EncodeToString(key) {
		return fmt.Errorf("no pinned key for %s matches", email)
	}
	if p.peers[i].KeyChanged != nil {
		return &KeyChangedError{Email: email}
	}
	if p.peers[i].Verified {
		return nil
	}
	p.peers[i].Verified = true
	return p.save()
}

// Forget removes the pin for email; the next contact pins afresh.
func (p *Peers) Forget(email string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	i := p.find(email)
	if i < 0 {
		return false, nil
	}
	p.peers = slices.Delete(p.peers, i, i+1)
	return true, p.save()
}

// SetCaps records the capabilities a peer announced, for the pinned key
// only; nothing is written when they did not change.
func (p *Peers) SetCaps(email string, key ed25519.PublicKey, caps []string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	i := p.find(email)
	if i < 0 || p.peers[i].PubKey != base64.StdEncoding.EncodeToString(key) {
		return fmt.Errorf("no pinned key for %s matches", email)
	}
	c := slices.Clone(caps)
	slices.Sort(c)
	c = slices.Compact(c)
	if len(c) == 0 {
		c = nil
	}
	if slices.Equal(p.peers[i].Caps, c) {
		return nil
	}
	p.peers[i].Caps = c
	return p.save()
}

// Has reports whether the peer announced capability c.
func (p Peer) Has(c string) bool { return slices.Contains(p.Caps, c) }

// ErrPinClash means peers.yaml pins another key for an email than the one
// an invite or a team log vouches for (spec 8.2). Nothing is changed: the
// pinned key stays for one-to-one syncs and the UI shows the mismatch.
type ErrPinClash struct{ Email string }

func (e *ErrPinClash) Error() string {
	return fmt.Sprintf("the key pinned for %s differs from the one the team vouches for; "+
		"compare fingerprints with them, then remove the old pin (`%s peers forget %[1]s`) if the team's key is right", e.Email, name.Binary)
}

// PinVerified pins key for email as verified, for a key vouched for by an
// invite (spec 19.7): new pins are stored verified, a matching pin becomes
// verified, and a different pin is left alone with *ErrPinClash.
func (p *Peers) PinVerified(email string, key ed25519.PublicKey) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	enc := base64.StdEncoding.EncodeToString(key)
	i := p.find(email)
	if i < 0 {
		p.peers = append(p.peers, Peer{Email: strings.ToLower(email), PubKey: enc, Verified: true, FirstSeen: p.now().UTC().Format("2006-01-02")})
		return p.save()
	}
	if p.peers[i].PubKey != enc || p.peers[i].KeyChanged != nil {
		return &ErrPinClash{Email: email}
	}
	if p.peers[i].Verified {
		return nil
	}
	p.peers[i].Verified = true
	return p.save()
}
