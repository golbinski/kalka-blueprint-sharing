package main

// Where the plugin keeps its settings: never in a blueprint and never in
// kalka's own configuration. The site, flavor and email go into
// config.json in the plugin's folder; the token goes into the system's
// credential store (Keychain, Windows Credential Manager, the Secret
// Service through secret-tool), or, where there is none, into that file,
// readable by the user only.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// service names the plugin's entries in the credential store.
const service = "kalka-plugin-jira"

// siteConfig is config.json.
type siteConfig struct {
	Site   string `json:"site"`
	Flavor string `json:"flavor"`
	Email  string `json:"email,omitempty"`
	// TokenIn is "keychain" when the system holds the token, "file"
	// when Token below holds it.
	TokenIn string `json:"token_in"`
	Token   string `json:"token,omitempty"`
}

// fieldsFile caches each site's sprint field id. It is apart from
// config.json so that a lookup, which writes it, never writes back a
// configuration it read before a login or a logout changed it: only
// login and logout write config.json.
const fieldsFile = "fields.json"

type fieldCache struct {
	ID string    `json:"id"`
	At time.Time `json:"at"`
}

type store struct {
	dir string
	// keyring is the system's credential store; nil means none.
	keyring keyring
	cfg     *siteConfig
}

// keyring is a system credential store.
type keyring interface {
	set(account, secret string) error
	get(account string) (string, error)
	del(account string) error
}

// openStore opens the plugin's folder: $KALKA_PLUGIN_JIRA_DIR, or
// kalka-plugin-jira in the user's configuration folder.
func openStore() (*store, error) {
	dir := os.Getenv("KALKA_PLUGIN_JIRA_DIR")
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(base, service)
	}
	st := &store{dir: dir}
	if os.Getenv("KALKA_PLUGIN_JIRA_NO_KEYRING") == "" {
		st.keyring = systemKeyring()
	}
	return st, nil
}

func (s *store) path() string { return filepath.Join(s.dir, "config.json") }

func (s *store) load() (*siteConfig, error) {
	if s.cfg != nil {
		return s.cfg, nil
	}
	raw, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, errNotLoggedIn
	}
	if err != nil {
		return nil, err
	}
	var c siteConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %v", s.path(), err)
	}
	if c.Site == "" {
		return nil, errNotLoggedIn
	}
	s.cfg = &c
	return &c, nil
}

func (s *store) save(c *siteConfig) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(c, "", "  ")
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	s.cfg = c
	return os.Rename(tmp, s.path())
}

var errNotLoggedIn = errors.New("not logged in to Jira: run `kalka-plugin-jira login`")

// token reads the stored token for the configured site.
func (s *store) token(c *siteConfig) (string, error) {
	if c.TokenIn == "file" {
		if c.Token == "" {
			return "", errNotLoggedIn
		}
		return c.Token, nil
	}
	if s.keyring == nil {
		return "", errors.New("the token is in the system's credential store, which cannot be reached; run `kalka-plugin-jira login` again")
	}
	t, err := s.keyring.get(c.Site)
	if err != nil || t == "" {
		return "", fmt.Errorf("no token for %s in the system's credential store; run `kalka-plugin-jira login`", c.Site)
	}
	return t, nil
}

// login stores the site and token: in the credential store when there is
// one that works, else in config.json.
func (s *store) login(c *siteConfig, token string) (where string, err error) {
	c.Token, c.TokenIn = "", "file"
	if s.keyring != nil {
		if err := s.keyring.set(c.Site, token); err == nil {
			c.TokenIn = "keychain"
		}
	}
	if c.TokenIn == "file" {
		c.Token = token
	}
	if old, err := s.load(); err == nil {
		if old.Site != c.Site && old.TokenIn == "keychain" && s.keyring != nil {
			_ = s.keyring.del(old.Site)
		}
	}
	if err := s.save(c); err != nil {
		return "", err
	}
	if c.TokenIn == "keychain" {
		return "the system's credential store", nil
	}
	return s.path() + " (readable by you only)", nil
}

func (s *store) logout() error {
	c, err := s.load()
	if err != nil {
		return err
	}
	if c.TokenIn == "keychain" && s.keyring != nil {
		_ = s.keyring.del(c.Site)
	}
	s.cfg = nil
	_ = os.Remove(filepath.Join(s.dir, fieldsFile))
	return os.Remove(s.path())
}

func (s *store) fields() map[string]fieldCache {
	raw, err := os.ReadFile(filepath.Join(s.dir, fieldsFile))
	if err != nil {
		return nil
	}
	var m map[string]fieldCache
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

func (s *store) cachedField(site string) (string, bool) {
	f, ok := s.fields()[site]
	return f.ID, ok && time.Since(f.At) < 24*time.Hour
}

// saveField records a site's sprint field id in fields.json, written
// whole and renamed into place; config.json is not touched.
func (s *store) saveField(site, id string) {
	m := s.fields()
	if m == nil {
		m = map[string]fieldCache{}
	}
	m[site] = fieldCache{ID: id, At: time.Now().UTC()}
	if os.MkdirAll(s.dir, 0o700) != nil {
		return
	}
	raw, _ := json.MarshalIndent(m, "", "  ")
	tmp, err := os.CreateTemp(s.dir, fieldsFile+".*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Chmod(tmp.Name(), 0o600) != nil || os.Rename(tmp.Name(), filepath.Join(s.dir, fieldsFile)) != nil {
		_ = os.Remove(tmp.Name())
	}
}

// tokenRe is what an API token or personal access token looks like; it
// also keeps the token safe to pass to the credential store's tools.
var tokenRe = regexp.MustCompile(`^[A-Za-z0-9+/=_.:-]{8,1000}$`)

// checkSite validates the site's address: https, or http on this machine
// only (a test server).
func checkSite(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%q is not a site address such as https://example.atlassian.net", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if ip := net.ParseIP(u.Hostname()); u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return "", errors.New("the site must use https")
		}
	default:
		return "", errors.New("the site must use https")
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/"), nil
}
