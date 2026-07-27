package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Presence reports now-playing state to an external surface (e.g. Discord).
type Presence interface {
	// SetPlaying shows a track as currently playing.
	SetPlaying(track Track, startedAt time.Time) error
	// SetPaused shows a track as paused (optional; may no-op).
	SetPaused(track Track) error
	// Clear removes the presence.
	Clear() error
	// Close disconnects.
	Close() error
}

// NopPresence is a no-op Presence used when Discord is disabled.
type NopPresence struct{}

func (NopPresence) SetPlaying(Track, time.Time) error { return nil }
func (NopPresence) SetPaused(Track) error             { return nil }
func (NopPresence) Clear() error                      { return nil }
func (NopPresence) Close() error                      { return nil }

// DiscordPresence talks to the local Discord desktop client over IPC.
type DiscordPresence struct {
	ClientID string
	// FallbackImage is a static Developer Portal asset key used only when
	// album art cannot be resolved (optional).
	FallbackImage string
	Art           ArtResolver

	mu   sync.Mutex
	conn net.Conn
}

// NewDiscordPresence creates a client. Connect is lazy on first Set*.
func NewDiscordPresence(clientID, fallbackImage string, art ArtResolver) *DiscordPresence {
	if art == nil {
		art = NopArt{}
	}
	return &DiscordPresence{
		ClientID:      clientID,
		FallbackImage: fallbackImage,
		Art:           art,
	}
}

func (d *DiscordPresence) SetPlaying(track Track, startedAt time.Time) error {
	artURL := d.resolveArt(track)
	return d.setActivity(track, true, startedAt, artURL)
}

func (d *DiscordPresence) SetPaused(track Track) error {
	artURL := d.resolveArt(track)
	return d.setActivity(track, false, time.Time{}, artURL)
}

func (d *DiscordPresence) resolveArt(track Track) string {
	if d.Art == nil {
		return ""
	}
	return d.Art.CoverURL(track.Artist, track.Name, track.Album)
}

func (d *DiscordPresence) Clear() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn == nil {
		return nil
	}
	// Empty activity clears presence.
	return d.sendActivityLocked(nil)
}

func (d *DiscordPresence) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn == nil {
		return nil
	}
	err := d.conn.Close()
	d.conn = nil
	return err
}

func (d *DiscordPresence) setActivity(track Track, playing bool, startedAt time.Time, artURL string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.ensureConnectedLocked(); err != nil {
		return err
	}

	// Desired layout in Discord:
	//   [album art]  Song name          (details — primary line we control)
	//                Artist             (state)
	//                Apple Music powered by lfm  (button at bottom)
	//                elapsed timer
	//
	// Note: Discord always shows the Developer Portal application name
	// ("lfm") as the activity title under "Listening to" / "Playing".
	// Song/artist are the lines we fully control. Rename the Discord app
	// to "Apple Music" in the portal if you want that title line softer.
	song := track.Name
	if song == "" {
		song = "Unknown track"
	}
	artist := track.Artist
	if artist == "" {
		artist = "Unknown artist"
	}
	if !playing {
		artist = "Paused · " + artist
	}

	song = truncateRunes(song, 128)
	artist = truncateRunes(artist, 128)

	// type 2 = Listening (shows "Listening to <app name>" instead of "Playing")
	const activityListening = 2

	act := map[string]any{
		"type": activityListening,
		// `name` is best-effort — many Discord builds still force the
		// Developer Portal application name here ("lfm"). details/state are reliable.
		"name":    song,
		"details": song,
		"state":   artist,
		"assets":  map[string]any{},
		"buttons": []map[string]string{
			// Button labels max 32 chars; shows at the bottom of the card.
			{"label": "Apple Music powered by lfm", "url": "https://music.apple.com"},
		},
	}

	assets := act["assets"].(map[string]any)
	if artURL != "" {
		// Discord desktop accepts external HTTPS image URLs for large_image.
		assets["large_image"] = discordImageKey(artURL)
		if track.Album != "" {
			assets["large_text"] = truncateRunes(track.Album, 128)
		} else {
			assets["large_text"] = "Apple Music"
		}
	} else if d.FallbackImage != "" {
		assets["large_image"] = d.FallbackImage
		assets["large_text"] = "lfm"
	}

	if playing && !startedAt.IsZero() {
		ts := map[string]any{
			"start": startedAt.Unix(),
		}
		// End timestamp gives Discord a progress-style timer when duration known.
		if track.Duration > 1 {
			end := startedAt.Add(time.Duration(math.Round(track.Duration)) * time.Second)
			ts["end"] = end.Unix()
		}
		act["timestamps"] = ts
	}

	return d.sendActivityLocked(act)
}

// discordImageKey formats an image reference for Discord Rich Presence assets.
// External HTTPS URLs are passed through; Discord proxies them for display.
func discordImageKey(artURL string) string {
	artURL = strings.TrimSpace(artURL)
	if artURL == "" {
		return ""
	}
	// Already a portal asset key (no scheme).
	if !strings.Contains(artURL, "://") {
		return artURL
	}
	return artURL
}

func (d *DiscordPresence) sendActivityLocked(activity map[string]any) error {
	payload := map[string]any{
		"cmd":   "SET_ACTIVITY",
		"nonce": fmt.Sprintf("%d", time.Now().UnixNano()),
		"args": map[string]any{
			"pid":      os.Getpid(),
			"activity": activity,
		},
	}
	return d.writeFrameLocked(1, payload)
}

func (d *DiscordPresence) ensureConnectedLocked() error {
	if d.conn != nil {
		return nil
	}
	conn, err := dialDiscordIPC()
	if err != nil {
		return fmt.Errorf("discord ipc: %w", err)
	}
	d.conn = conn

	hs := map[string]any{
		"v":         1,
		"client_id": d.ClientID,
	}
	if err := d.writeFrameLocked(0, hs); err != nil {
		d.conn.Close()
		d.conn = nil
		return fmt.Errorf("discord handshake: %w", err)
	}
	// Read handshake ack (best-effort; ignore body).
	_ = d.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, _ = readFrame(d.conn)
	_ = d.conn.SetReadDeadline(time.Time{})
	return nil
}

func (d *DiscordPresence) writeFrameLocked(opcode uint32, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var hdr [8]byte
	binary.LittleEndian.PutUint32(hdr[0:4], opcode)
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(len(body)))
	if _, err := d.conn.Write(hdr[:]); err != nil {
		d.conn.Close()
		d.conn = nil
		return err
	}
	if _, err := d.conn.Write(body); err != nil {
		d.conn.Close()
		d.conn = nil
		return err
	}
	return nil
}

func dialDiscordIPC() (net.Conn, error) {
	// Discord exposes discord-ipc-0 .. discord-ipc-9 under the temp dir.
	// Prefer TMPDIR first so tests (and Discord) can pin the socket location.
	var candidates []string
	if t := os.Getenv("TMPDIR"); t != "" {
		candidates = append(candidates, t)
	}
	if base := os.TempDir(); base != "" {
		candidates = append(candidates, base)
	}
	// Dedup
	seen := map[string]bool{}
	var dirs []string
	for _, d := range candidates {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		dirs = append(dirs, d)
	}

	var last error
	for _, dir := range dirs {
		for i := 0; i < 10; i++ {
			path := filepath.Join(dir, fmt.Sprintf("discord-ipc-%d", i))
			conn, err := net.DialTimeout("unix", path, 500*time.Millisecond)
			if err == nil {
				return conn, nil
			}
			last = err
		}
	}
	if last == nil {
		last = fmt.Errorf("no discord-ipc socket found")
	}
	return nil, last
}

func readFrame(r io.Reader) (opcode uint32, body []byte, err error) {
	var hdr [8]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	opcode = binary.LittleEndian.Uint32(hdr[0:4])
	n := binary.LittleEndian.Uint32(hdr[4:8])
	if n > 1<<20 {
		return 0, nil, fmt.Errorf("discord frame too large: %d", n)
	}
	body = make([]byte, n)
	if _, err = io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	return opcode, body, nil
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// presenceKey helps avoid spamming Discord with identical payloads.
func presenceKey(track Track, playing bool) string {
	var b bytes.Buffer
	b.WriteString(track.Name)
	b.WriteByte('|')
	b.WriteString(track.Artist)
	b.WriteByte('|')
	b.WriteString(track.Album)
	if playing {
		b.WriteString("|play")
	} else {
		b.WriteString("|pause")
	}
	return b.String()
}
