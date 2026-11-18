// Package config loads kalka's configuration (spec section 5).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"go.yaml.in/yaml/v3"

	"github.com/golbinski/kalka/internal/name"
)

// FileName is the configuration file inside the config directory.
const FileName = "config.yaml"

// DirEnv overrides the config directory (tests, portable installs).
const DirEnv = "KALKA_CONFIG_DIR"

// PeerJSCloud is the literal rendezvous value for the public PeerJS cloud.
const PeerJSCloud = "peerjs-cloud"

// Config holds the three required settings and the optional ones.
type Config struct {
	Email      string      `yaml:"email"`
	Rendezvous string      `yaml:"rendezvous"`
	Folder     string      `yaml:"folder"`
	UIPort     int         `yaml:"ui_port,omitempty"`
	ICEServers []ICEServer `yaml:"ice_servers,omitempty"`
	// RelayOnly forces TURN relay candidates (testing on restricted
	// networks). Not part of setup.
	RelayOnly bool `yaml:"relay_only,omitempty"`
}

// ICEServer is a manual ICE override entry.
type ICEServer struct {
	URLs       []string `yaml:"urls"`
	Username   string   `yaml:"username,omitempty"`
	Credential string   `yaml:"credential,omitempty"`
}

// DefaultUIPort is the localhost port of the web UI.
const DefaultUIPort = 7717

// ErrMissing means no config file exists yet: the UI shows setup.
var ErrMissing = errors.New("kalka is not set up yet: no " + FileName)

// Dir returns the config directory for this platform.
func Dir() (string, error) {
	if d := os.Getenv(DirEnv); d != "" {
		return d, nil
	}
	if runtime.GOOS == "windows" {
		if d := os.Getenv("APPDATA"); d != "" {
			return filepath.Join(d, name.Product), nil
		}
		return "", errors.New("APPDATA is not set")
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, name.Product), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", name.Product), nil
}

// Load reads the config file. It returns ErrMissing if there is none.
func Load() (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, FileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrMissing
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.UIPort == 0 {
		c.UIPort = DefaultUIPort
	}
	return &c, nil
}

// Save writes the config file, creating the config directory.
func Save(c *Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return SaveIn(dir, c)
}

// SaveIn writes the config file in dir, creating it.
func SaveIn(dir string, c *Config) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, FileName), data, 0o600)
}

// SetRendezvous changes the rendezvous in the config file in dir and
// nothing else: comments, the order of keys and settings left out stay as
// they are. It returns ErrMissing if there is no file.
func SetRendezvous(dir, server string) error {
	path := filepath.Join(dir, FileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ErrMissing
	}
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%s: not a mapping of settings", path)
	}
	m := doc.Content[0]
	set := false
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == "rendezvous" {
			v := m.Content[i+1]
			v.Kind, v.Tag, v.Style, v.Value = yaml.ScalarNode, "!!str", 0, server
			set = true
		}
	}
	if !set {
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "rendezvous"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: server})
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	// Written beside and renamed, so a failure never leaves half a file.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
