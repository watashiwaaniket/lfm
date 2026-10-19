package main

import (
	"context"
	"log/slog"
	"math"
	"time"
)

// LastFMAPI is the subset of Last.fm methods the scrobbler needs.
type LastFMAPI interface {
	UpdateNowPlaying(artist, track, album string, durationSec int) error
	Scrobble(artist, track, album string, timestamp int64, durationSec int) error
}

// scrobbleState tracks one continuous play of a track.
type scrobbleState struct {
	current        Track
	startedAt      time.Time // when we first saw this track as playing
	playedSeconds  float64   // accumulated wall-clock playing time
	nowPlayingSent bool
	scrobbled      bool
	lastTick       time.Time // last poll where state was playing
}

// Scrobbler polls Music and applies Last.fm scrobble rules.
type Scrobbler struct {
	Music    MusicClient
	LastFM   LastFMAPI
	Presence Presence
	Cfg      Config
	Log      *slog.Logger

	// now is injectable for tests; defaults to time.Now.
	now func() time.Time

	state        scrobbleState
	lastPresence string // dedupe Discord updates
}

// NewScrobbler wires dependencies.
func NewScrobbler(music MusicClient, lastfm LastFMAPI, presence Presence, cfg Config, log *slog.Logger) *Scrobbler {
	if log == nil {
		log = slog.Default()
	}
	if presence == nil {
		presence = NopPresence{}
	}
	return &Scrobbler{
		Music:    music,
		LastFM:   lastfm,
		Presence: presence,
		Cfg:      cfg,
		Log:      log,
		now:      time.Now,
	}
}

// shouldScrobble implements official Last.fm thresholds:
// duration > min, and (played >= 50% OR played >= maxSeconds).
func shouldScrobble(played, duration, minDuration, threshold, maxSeconds float64) bool {
	if duration <= minDuration {
		return false
	}
	if played >= maxSeconds {
		return true
	}
	if duration > 0 && played >= duration*threshold {
		return true
	}
	return false
}

// minPlayBeforeTrackChange filters noise from very short seeks / track flips.
func minPlayBeforeTrackChange(cfg Config) float64 {
	// Spec: ignore track changes under ~3–5 seconds.
	return 3.0
}

// tick processes one poll observation. Exposed for unit tests.
func (s *Scrobbler) tick(track Track) {
	now := s.now()
	cfg := s.Cfg

	if track.State != "playing" {
		// Paused/stopped: do not accumulate. Keep state so resume continues.
		// Full stop with empty track: reset after we were tracking something.
		if track.State == "stopped" && (track.Name == "" && track.Artist == "") {
			if s.state.current.Name != "" || s.state.playedSeconds > 0 {
				s.Log.Debug("playback stopped; clearing state")
			}
			s.clearPresence()
			s.state = scrobbleState{}
			return
		}
		// Paused (or stopped-with-metadata): freeze accumulation.
		s.state.lastTick = time.Time{}
		if track.Name != "" || s.state.current.Name != "" {
			paused := track
			if paused.Name == "" {
				paused = s.state.current
			}
			s.updatePresencePaused(paused)
		}
		return
	}

	// Playing.
	if !s.state.current.SameTrack(track) {
		// New track. Ignore bounce if previous play was extremely short and
		// we never established it (optional noise filter for flapping).
		prev := s.state
		if prev.current.Name != "" && prev.playedSeconds < minPlayBeforeTrackChange(cfg) && !prev.scrobbled {
			s.Log.Debug("short track change ignored for scrobble bookkeeping",
				"prev", prev.current.Name, "played", prev.playedSeconds)
		}

		s.state = scrobbleState{
			current:   track,
			startedAt: now,
			lastTick:  now,
		}
		s.sendNowPlaying(track)
		s.updatePresencePlaying(track, now)
		return
	}

	// Same track still playing — accumulate wall-clock since last playing tick.
	if s.state.lastTick.IsZero() {
		// Resumed from pause.
		s.state.lastTick = now
		if !s.state.nowPlayingSent {
			s.sendNowPlaying(track)
		}
		s.updatePresencePlaying(track, s.state.startedAt)
		return
	}

	delta := now.Sub(s.state.lastTick).Seconds()
	if delta < 0 {
		delta = 0
	}
	// Cap delta to a few poll intervals so a long sleep doesn't count as listening.
	maxDelta := float64(cfg.PollInterval) * 3
	if maxDelta < 10 {
		maxDelta = 10
	}
	if delta > maxDelta {
		delta = maxDelta
	}
	s.state.playedSeconds += delta
	s.state.lastTick = now
	// Refresh metadata (position/duration may update).
	s.state.current = track

	if !s.state.scrobbled && shouldScrobble(
		s.state.playedSeconds,
		track.Duration,
		cfg.MinTrackDuration,
		cfg.ScrobbleThreshold,
		cfg.ScrobbleMaxSeconds,
	) {
		s.doScrobble(track)
	}
}

func (s *Scrobbler) sendNowPlaying(track Track) {
	dur := int(math.Round(track.Duration))
	if err := s.LastFM.UpdateNowPlaying(track.Artist, track.Name, track.Album, dur); err != nil {
		s.Log.Warn("updateNowPlaying failed", "err", err, "track", track.Name, "artist", track.Artist)
		return
	}
	s.state.nowPlayingSent = true
	s.Log.Info("now playing", "artist", track.Artist, "track", track.Name)
}

func (s *Scrobbler) doScrobble(track Track) {
	// Timestamp should be when playback started (Last.fm convention).
	ts := s.state.startedAt.Unix()
	if ts <= 0 {
		ts = s.now().Unix()
	}
	dur := int(math.Round(track.Duration))
	if err := s.LastFM.Scrobble(track.Artist, track.Name, track.Album, ts, dur); err != nil {
		s.Log.Warn("scrobble failed", "err", err, "track", track.Name, "artist", track.Artist)
		return
	}
	s.state.scrobbled = true
	s.Log.Info("scrobbled",
		"artist", track.Artist,
		"track", track.Name,
		"played_seconds", int(s.state.playedSeconds),
		"duration", int(track.Duration),
	)
}

func (s *Scrobbler) updatePresencePlaying(track Track, startedAt time.Time) {
	key := presenceKey(track, true)
	if key == s.lastPresence {
		return
	}
	if err := s.Presence.SetPlaying(track, startedAt); err != nil {
		s.Log.Debug("discord presence", "err", err)
		return
	}
	s.lastPresence = key
	s.Log.Debug("discord presence", "status", "playing", "track", track.Name)
}

func (s *Scrobbler) updatePresencePaused(track Track) {
	key := presenceKey(track, false)
	if key == s.lastPresence {
		return
	}
	if err := s.Presence.SetPaused(track); err != nil {
		s.Log.Debug("discord presence", "err", err)
		return
	}
	s.lastPresence = key
	s.Log.Debug("discord presence", "status", "paused", "track", track.Name)
}

func (s *Scrobbler) clearPresence() {
	if s.lastPresence == "" {
		return
	}
	if err := s.Presence.Clear(); err != nil {
		s.Log.Debug("discord clear", "err", err)
	}
	s.lastPresence = ""
}

// Run polls until ctx is cancelled (SIGINT/SIGTERM).
func (s *Scrobbler) Run(ctx context.Context) error {
	interval := time.Duration(s.Cfg.PollInterval) * time.Second
	if interval <= 0 {
		interval = 3 * time.Second
	}

	s.Log.Info("scrobbler started",
		"poll_interval", interval.String(),
		"discord", s.Cfg.Discord.Enabled && s.Cfg.Discord.ClientID != "",
	)

	// Immediate first tick, then ticker.
	s.pollOnce()

	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			s.Log.Info("scrobbler stopping")
			s.clearPresence()
			_ = s.Presence.Close()
			return nil
		case <-t.C:
			s.pollOnce()
		}
	}
}

func (s *Scrobbler) pollOnce() {
	track, err := s.Music.GetCurrentTrack()
	if err != nil {
		// Music client soft-fails; still be defensive.
		s.Log.Debug("music poll error", "err", err)
		track = Track{State: "stopped"}
	}
	s.tick(track)
}
