// Package identity holds the local key pair (spec 8.1). Trust in other
// keys comes from team logs only (spec 8.2).
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/name"
)

// KeyFile lives in the config directory.
const KeyFile = "identity.key"

const keyHeader = name.Product + "-identity-v1"

// ErrNoKey means identity.key does not exist yet: kalka has not run here.
var ErrNoKey = errors.New("no identity key yet: " + name.Product + " has not run on this machine")

// LoadKey reads identity.key from dir; it never creates one.
func LoadKey(dir string) (ed25519.PrivateKey, error) {
	path := filepath.Join(dir, KeyFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoKey
	}
	if err != nil {
		return nil, err
	}
	return parseKey(path, raw)
}

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
	return parseKey(path, raw)
}

func parseKey(path string, raw []byte) (ed25519.PrivateKey, error) {
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
