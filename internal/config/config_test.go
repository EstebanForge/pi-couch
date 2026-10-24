package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadParsesValuesAndComments(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pi-couch")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sub, "config.toml")
	body := strings.Join([]string{
		"# Pi Couch configuration.",
		"addr = \"127.0.0.1:9999\"  # loopback",
		"",
		"# the public origin",
		"origin = \"https://box.example.com\"",
		"memory_url = \"http://10.0.0.5:3111\"",
		"uploads_dir = \"/tmp/up\"",
		"sessions_dir = \"/data/sessions\"",
		"memory_secret = \"s3cret\"",
	}, "\n")
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:9999" || cfg.Origin != "https://box.example.com" ||
		cfg.MemoryURL != "http://10.0.0.5:3111" || cfg.UploadsDir != "/tmp/up" ||
		cfg.SessionsDir != "/data/sessions" || cfg.MemorySecret != "s3cret" {
		t.Fatalf("cfg %+v", cfg)
	}
}

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "absent"))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:8090" {
		t.Fatalf("addr %q", cfg.Addr)
	}
	if cfg.UploadsDir == "" || cfg.MemoryURL == "" {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestLoadMalformedFailsLoud(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pi-couch")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "config.toml"), []byte("addr = "), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	if _, err := Load(); err == nil {
		t.Fatal("want error for malformed config")
	}
}
