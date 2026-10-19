package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if c.PollInterval != 3 {
		t.Fatalf("poll = %d", c.PollInterval)
	}
	if c.MinTrackDuration != 30 || c.ScrobbleThreshold != 0.5 || c.ScrobbleMaxSeconds != 240 {
		t.Fatalf("scrobble defaults: %+v", c)
	}
	if c.LogLevel != "info" {
		t.Fatalf("log = %q", c.LogLevel)
	}
}

func TestLoadConfig_FromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `
lastfm:
  api_key: "k1"
  api_secret: "s1"
  session_key: "sk1"
poll_interval: 5
min_track_duration: 40
scrobble_threshold: 0.6
scrobble_max_seconds: 200
log_level: debug
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	// Clear env that might override
	t.Setenv("LASTFM_API_KEY", "")
	t.Setenv("LASTFM_API_SECRET", "")
	t.Setenv("LASTFM_SESSION_KEY", "")
	t.Setenv("POLL_INTERVAL", "")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastFM.APIKey != "k1" || cfg.LastFM.APISecret != "s1" || cfg.LastFM.SessionKey != "sk1" {
		t.Fatalf("lastfm: %+v", cfg.LastFM)
	}
	if cfg.PollInterval != 5 || cfg.MinTrackDuration != 40 {
		t.Fatalf("poll/min: %d %v", cfg.PollInterval, cfg.MinTrackDuration)
	}
	if cfg.ScrobbleThreshold != 0.6 || cfg.ScrobbleMaxSeconds != 200 {
		t.Fatalf("threshold: %v %v", cfg.ScrobbleThreshold, cfg.ScrobbleMaxSeconds)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("log = %q", cfg.LogLevel)
	}
}

func TestLoadConfig_EnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
lastfm:
  api_key: "file-key"
  api_secret: "file-secret"
  session_key: "file-sk"
poll_interval: 3
`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LASTFM_API_KEY", "env-key")
	t.Setenv("LASTFM_API_SECRET", "env-secret")
	t.Setenv("LASTFM_SESSION_KEY", "env-sk")
	t.Setenv("POLL_INTERVAL", "7")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastFM.APIKey != "env-key" {
		t.Fatalf("api_key = %q", cfg.LastFM.APIKey)
	}
	if cfg.LastFM.APISecret != "env-secret" {
		t.Fatalf("secret = %q", cfg.LastFM.APISecret)
	}
	if cfg.LastFM.SessionKey != "env-sk" {
		t.Fatalf("sk = %q", cfg.LastFM.SessionKey)
	}
	if cfg.PollInterval != 7 {
		t.Fatalf("poll = %d", cfg.PollInterval)
	}
}

func TestLoadConfig_MissingFile_EnvOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.yaml")
	t.Setenv("LASTFM_API_KEY", "k")
	t.Setenv("LASTFM_API_SECRET", "s")
	t.Setenv("LASTFM_SESSION_KEY", "sk")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ValidateRun(); err != nil {
		t.Fatal(err)
	}
	if cfg.PollInterval != 3 {
		t.Fatalf("default poll expected, got %d", cfg.PollInterval)
	}
}

func TestLoadConfig_PartialYAML_FillsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`
lastfm:
  api_key: "k"
  api_secret: "s"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LASTFM_API_KEY", "")
	t.Setenv("LASTFM_API_SECRET", "")
	t.Setenv("LASTFM_SESSION_KEY", "")
	t.Setenv("POLL_INTERVAL", "")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PollInterval != 3 || cfg.ScrobbleMaxSeconds != 240 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestValidateRun(t *testing.T) {
	c := DefaultConfig()
	if err := c.ValidateAuthCredentials(); err == nil {
		t.Fatal("expected missing key error")
	}
	c.LastFM.APIKey = "k"
	c.LastFM.APISecret = "s"
	if err := c.ValidateAuthCredentials(); err != nil {
		t.Fatal(err)
	}
	if err := c.ValidateRun(); err == nil {
		t.Fatal("expected missing session")
	}
	c.LastFM.SessionKey = "sk"
	if err := c.ValidateRun(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfig_DiscordEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.yaml")
	t.Setenv("LASTFM_API_KEY", "k")
	t.Setenv("LASTFM_API_SECRET", "s")
	t.Setenv("LASTFM_SESSION_KEY", "sk")
	t.Setenv("DISCORD_ENABLED", "true")
	t.Setenv("DISCORD_CLIENT_ID", "999")
	t.Setenv("DISCORD_LARGE_IMAGE", "cover")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Discord.Enabled || cfg.Discord.ClientID != "999" || cfg.Discord.LargeImage != "cover" {
		t.Fatalf("discord: %+v", cfg.Discord)
	}
	p := cfg.NewPresence()
	dp, ok := p.(*DiscordPresence)
	if !ok {
		t.Fatalf("expected DiscordPresence, got %T", p)
	}
	if dp.Art == nil {
		t.Fatal("expected art resolver")
	}
}

func TestNewPresence_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Discord.Enabled = false
	if _, ok := cfg.NewPresence().(NopPresence); !ok {
		t.Fatal("expected NopPresence")
	}
}

func TestSaveSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")
	// Pre-seed API credentials
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`
lastfm:
  api_key: "k"
  api_secret: "s"
poll_interval: 4
`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LASTFM_API_KEY", "")
	t.Setenv("LASTFM_API_SECRET", "")
	t.Setenv("LASTFM_SESSION_KEY", "")

	if err := SaveSession(path, "new-sk", "alice"); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastFM.SessionKey != "new-sk" {
		t.Fatalf("sk = %q", cfg.LastFM.SessionKey)
	}
	if cfg.LastFM.Username != "alice" {
		t.Fatalf("user = %q", cfg.LastFM.Username)
	}
	if cfg.LastFM.APIKey != "k" || cfg.PollInterval != 4 {
		t.Fatalf("should preserve other fields: %+v", cfg)
	}
}
