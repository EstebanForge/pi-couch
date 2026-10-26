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

func TestTokenDefaultsEnforced(t *testing.T) {
	cfg := Defaults()
	if !cfg.EnforceToken {
		t.Fatal("enforce_token must default true")
	}
}

func TestAutoUpdateDefaultsOn(t *testing.T) {
	if !Defaults().AutoUpdate {
		t.Fatal("auto_update must default true")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "pi-couch"), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "pi-couch", "config.toml")
	if err := os.WriteFile(file, []byte("auto_update = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AutoUpdate {
		t.Fatal("auto_update = false must be parsed")
	}
}

func TestLoadParsesToken(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pi-couch")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sub, "config.toml")
	body := "access_token = \"from-file\"\nenforce_token = false\n"
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("PICOUCH_ACCESS_TOKEN", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AccessToken != "from-file" || cfg.EnforceToken {
		t.Fatalf("cfg token=%q enforce=%v", cfg.AccessToken, cfg.EnforceToken)
	}
}

func TestLoadEnvTokenWins(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pi-couch")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sub, "config.toml")
	if err := os.WriteFile(file, []byte("access_token = \"from-file\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("PICOUCH_ACCESS_TOKEN", "from-env")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AccessToken != "from-env" {
		t.Fatalf("env must win, got %q", cfg.AccessToken)
	}
}
