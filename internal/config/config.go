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
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, FileName), data, 0o600)
}
