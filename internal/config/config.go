// Package config loads Pi Couch's machine config from
// ~/.config/pi-couch/config.toml. Every key is optional; missing file
// means defaults, malformed file means a loud failure.
package config

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config carries every machine-specific value. Zero-value fields are
// filled from Defaults by Load, so callers never see "" for a key with
// a default.
type Config struct {
	// Listen address. Loopback by default; a tunnel fronts TLS.
	Addr string `toml:"addr"`
	// Public https origin the phone uses. The cross-site POST guard
	// accepts exactly this host. Empty keeps the server loopback-only.
	Origin string `toml:"origin"`
	// pi sessions root. Defaults to ~/.pi/agent/sessions.
	SessionsDir string `toml:"sessions_dir"`
	// Where uploaded attachments land. Must be readable by the pi
	// agents (they receive the path inside the prompt).
	UploadsDir string `toml:"uploads_dir"`
	// agentmemory health probe behind the composer's memory chip.
	// Defaults to localhost:3111; the AGENTMEMORY_URL env var wins.
	MemoryURL string `toml:"memory_url"`
	// Bearer secret for the memory probe. Prefer the
	// AGENTMEMORY_SECRET env var so the secret stays out of this file.
	MemorySecret string `toml:"memory_secret"`
	// Shared access token gating /api and /stream. The
	// PICOUCH_ACCESS_TOKEN env var wins over this key.
	AccessToken string `toml:"access_token"`
	// Refuse to start when AccessToken is empty. True by default so a
	// tokenless deployment is always a deliberate choice.
	EnforceToken bool `toml:"enforce_token"`
}

// Defaults returns the config used when the file is absent or a key is
// unset.
func Defaults() Config {
	return Config{
		Addr:         "127.0.0.1:8090",
		UploadsDir:   "/tmp/pi-couch-uploads",
		MemoryURL:    "http://localhost:3111",
		EnforceToken: true,
	}
}

// Path is the config file location: $XDG_CONFIG_HOME or ~/.config,
// then pi-couch/config.toml.
func Path() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "pi-couch", "config.toml"), nil
}

// Load reads the config file, filling every unset key from Defaults.
// A missing file is fine; a malformed one is an error.
func Load() (Config, error) {
	cfg := Defaults()
	path, err := Path()
	if err != nil {
		return cfg, err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if cfg.SessionsDir == "" {
			cfg.SessionsDir = defaultSessions()
		}
		cfg.AccessToken = cmp.Or(os.Getenv("PICOUCH_ACCESS_TOKEN"), cfg.AccessToken)
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if _, err := toml.NewDecoder(bytes.NewReader(raw)).Decode(&cfg); err != nil {
		return Defaults(), fmt.Errorf("%s: %w", path, err)
	}
	if cfg.SessionsDir == "" {
		cfg.SessionsDir = defaultSessions()
	}
	cfg.AccessToken = cmp.Or(os.Getenv("PICOUCH_ACCESS_TOKEN"), cfg.AccessToken)
	return cfg, nil
}

func defaultSessions() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi", "agent", "sessions")
}
