package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

const (
	remoteDirName             = "remote"
	remoteIdentityFile        = "identity"
	remoteTokenFile           = "host_token"
	DefaultRemoteThreadLimit  = 5
	DefaultRemoteSummaryChars = 280
	DefaultRemoteOpenTurns    = 6
	DefaultRemoteEventChars   = 4000
	DefaultRemoteWatchEvents  = 80
	DefaultRemoteWatchOpen    = 24
	DefaultRemoteKeepAwake    = true
)

// RemoteConfig is the phone-pairing channel. HubURL is whatever the user
// typed in Settings — never a compiled-in host. The Host Token is minted on
// this PC when pairing starts; GET /api/settings cannot echo it.
type RemoteConfig struct {
	Enabled      bool   `yaml:"enabled" json:"enabled"`
	HubURL       string `yaml:"hub_url" json:"hub_url"`
	ThreadLimit  int    `yaml:"thread_limit" json:"thread_limit"`
	SummaryChars int    `yaml:"summary_chars" json:"summary_chars"`
	OpenTurns    int    `yaml:"open_turns" json:"open_turns"`
	EventChars   int    `yaml:"event_chars" json:"event_chars"`
	WatchEvents  int    `yaml:"watch_events" json:"watch_events"`
	// KeepAwake holds a system sleep assertion while pairing is on. The
	// host process is the phone's only door; idle sleep slams it. Default
	// on. macOS honors this only on AC power (`caffeinate -s`).
	KeepAwake bool `yaml:"keep_awake" json:"keep_awake"`
}

// RemoteDir holds the host identity and Host Token (mode 0700 / 0600).
func (c *Config) RemoteDir() string {
	return filepath.Join(c.dataDir, remoteDirName)
}

func (c *Config) remoteIdentityPath() string {
	return filepath.Join(c.RemoteDir(), remoteIdentityFile)
}

func (c *Config) remoteTokenPath() string {
	return filepath.Join(c.RemoteDir(), remoteTokenFile)
}

func (c *Config) normalizeRemote() {
	c.Remote.HubURL = strings.TrimSpace(c.Remote.HubURL)
	if c.Remote.ThreadLimit <= 0 {
		c.Remote.ThreadLimit = DefaultRemoteThreadLimit
	}
	if c.Remote.SummaryChars <= 0 {
		c.Remote.SummaryChars = DefaultRemoteSummaryChars
	}
	if c.Remote.OpenTurns <= 0 {
		c.Remote.OpenTurns = DefaultRemoteOpenTurns
	}
	if c.Remote.EventChars <= 0 {
		c.Remote.EventChars = DefaultRemoteEventChars
	}
	if c.Remote.WatchEvents <= 0 {
		c.Remote.WatchEvents = DefaultRemoteWatchEvents
	}
}

// WriteHostToken stores the Host Token. An empty value deletes the file.
func (c *Config) WriteHostToken(token string) error {
	if err := os.MkdirAll(c.RemoteDir(), dirPerm); err != nil {
		return err
	}
	path := c.remoteTokenPath()
	token = strings.TrimSpace(token)
	if token == "" {
		_ = os.Remove(path)
		return nil
	}
	return os.WriteFile(path, []byte(token), filePerm)
}

// HostToken reads the stored token. Missing file is empty, not an error.
func (c *Config) HostToken() (string, error) {
	raw, err := os.ReadFile(c.remoteTokenPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func (c *Config) HasHostToken() bool {
	tok, err := c.HostToken()
	return err == nil && tok != ""
}

// EnsureHostToken returns the stored Host Token, minting a random one if the
// file is missing. The hub admits that token when open registration is on.
// This is not a Gateway Key and never goes in config.yaml.
func (c *Config) EnsureHostToken() (string, error) {
	tok, err := c.HostToken()
	if err != nil {
		return "", err
	}
	if tok != "" {
		return tok, nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok = hex.EncodeToString(raw)
	if err := c.WriteHostToken(tok); err != nil {
		return "", err
	}
	return tok, nil
}

func (c *Config) WriteRemoteIdentity(priv []byte) error {
	if err := os.MkdirAll(c.RemoteDir(), dirPerm); err != nil {
		return err
	}
	return os.WriteFile(c.remoteIdentityPath(), priv, filePerm)
}

func (c *Config) RemoteIdentity() ([]byte, error) {
	raw, err := os.ReadFile(c.remoteIdentityPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return raw, nil
}
