package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds runtime settings. Env vars override YAML values.
type Config struct {
	LastFM struct {
		APIKey     string `yaml:"api_key"`
		APISecret  string `yaml:"api_secret"`
		SessionKey string `yaml:"session_key"`
		Username   string `yaml:"username,omitempty"`
	} `yaml:"lastfm"`

	Discord struct {
		Enabled    bool   `yaml:"enabled"`
		ClientID   string `yaml:"client_id"`
		LargeImage string `yaml:"large_image"` // Discord art asset key (optional)
		LargeText  string `yaml:"large_text"`
	} `yaml:"discord"`

	PollInterval       int     `yaml:"poll_interval"`        // seconds, default 3
	MinTrackDuration   float64 `yaml:"min_track_duration"`   // seconds, default 30
	ScrobbleThreshold  float64 `yaml:"scrobble_threshold"`   // fraction, default 0.5
	ScrobbleMaxSeconds float64 `yaml:"scrobble_max_seconds"` // default 240
	LogLevel           string  `yaml:"log_level"`            // debug/info/warn/error
}

// DefaultConfig returns sensible Last.fm scrobble defaults.
func DefaultConfig() Config {
	var c Config
	c.PollInterval = 3
	c.MinTrackDuration = 30
	c.ScrobbleThreshold = 0.5
	c.ScrobbleMaxSeconds = 240
	c.LogLevel = "info"
	return c
}

// DefaultConfigPath is ~/.config/lfm/config.yaml
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.yaml"
	}
	return filepath.Join(home, ".config", "lfm", "config.yaml")
}

// LoadConfig reads YAML from path (if it exists) then applies env overrides.
// Missing file is OK when credentials come from the environment.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return cfg, fmt.Errorf("read config %s: %w", path, err)
		}
		// file missing — continue with defaults + env
	} else {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config %s: %w", path, err)
		}
		// Re-apply defaults for zero values left unset in YAML
		cfg = applyDefaults(cfg)
	}

	applyEnv(&cfg)
	return cfg, nil
}

func applyDefaults(c Config) Config {
	d := DefaultConfig()
	if c.PollInterval <= 0 {
		c.PollInterval = d.PollInterval
	}
	if c.MinTrackDuration <= 0 {
		c.MinTrackDuration = d.MinTrackDuration
	}
	if c.ScrobbleThreshold <= 0 {
		c.ScrobbleThreshold = d.ScrobbleThreshold
	}
	if c.ScrobbleMaxSeconds <= 0 {
		c.ScrobbleMaxSeconds = d.ScrobbleMaxSeconds
	}
	if c.LogLevel == "" {
		c.LogLevel = d.LogLevel
	}
	return c
}

func applyEnv(c *Config) {
	if v := os.Getenv("LASTFM_API_KEY"); v != "" {
		c.LastFM.APIKey = v
	}
	if v := os.Getenv("LASTFM_API_SECRET"); v != "" {
		c.LastFM.APISecret = v
	}
	if v := os.Getenv("LASTFM_SESSION_KEY"); v != "" {
		c.LastFM.SessionKey = v
	}
	if v := os.Getenv("POLL_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.PollInterval = n
		}
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		c.LogLevel = strings.ToLower(v)
	}
	if v := os.Getenv("DISCORD_ENABLED"); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			c.Discord.Enabled = true
		case "0", "false", "no", "off":
			c.Discord.Enabled = false
		}
	}
	if v := os.Getenv("DISCORD_CLIENT_ID"); v != "" {
		c.Discord.ClientID = v
	}
	if v := os.Getenv("DISCORD_LARGE_IMAGE"); v != "" {
		c.Discord.LargeImage = v
	}
}

// NewPresence builds a Presence from config (Nop when disabled / incomplete).
// Uses Last.fm + iTunes to resolve album art for Discord's large image.
func (c Config) NewPresence() Presence {
	if !c.Discord.Enabled || c.Discord.ClientID == "" {
		return NopPresence{}
	}
	art := NewAlbumArtClient(c.LastFM.APIKey)
	// large_image is only a fallback portal asset key when no cover is found.
	fallback := c.Discord.LargeImage
	return NewDiscordPresence(c.Discord.ClientID, fallback, art)
}

// ValidateAuthCredentials checks api key/secret are present (session optional for auth).
func (c Config) ValidateAuthCredentials() error {
	if c.LastFM.APIKey == "" {
		return fmt.Errorf("missing lastfm.api_key (or LASTFM_API_KEY)")
	}
	if c.LastFM.APISecret == "" {
		return fmt.Errorf("missing lastfm.api_secret (or LASTFM_API_SECRET)")
	}
	return nil
}

// ValidateRun checks everything needed for the daemon.
func (c Config) ValidateRun() error {
	if err := c.ValidateAuthCredentials(); err != nil {
		return err
	}
	if c.LastFM.SessionKey == "" {
		return fmt.Errorf("missing lastfm.session_key (or LASTFM_SESSION_KEY); run `lfm auth` first")
	}
	return nil
}

// SaveSession writes session_key (and username) back into the YAML config file,
// creating the parent directory if needed. Other fields are preserved when
// the file already exists.
func SaveSession(path, sessionKey, username string) error {
	cfg := DefaultConfig()
	if data, err := os.ReadFile(path); err == nil {
		_ = yaml.Unmarshal(data, &cfg)
		cfg = applyDefaults(cfg)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read config for save: %w", err)
	}

	// Preserve env-only keys if file was empty: merge current env into what we write
	// so users who only used env still get a usable file after auth.
	applyEnv(&cfg)

	cfg.LastFM.SessionKey = sessionKey
	if username != "" {
		cfg.LastFM.Username = username
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	out, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
