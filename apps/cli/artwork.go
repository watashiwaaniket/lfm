package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ArtResolver resolves a cover-art HTTPS URL for a track.
type ArtResolver interface {
	CoverURL(artist, track, album string) string
}

// AlbumArtClient looks up cover art (Last.fm → iTunes) with a small in-memory cache.
type AlbumArtClient struct {
	LastFMAPIKey string
	HTTP         *http.Client

	mu    sync.Mutex
	cache map[string]string // key → url (empty string = negative cache)
}

// NewAlbumArtClient creates a resolver. lastfmKey may be empty (iTunes-only).
func NewAlbumArtClient(lastfmKey string) *AlbumArtClient {
	return &AlbumArtClient{
		LastFMAPIKey: lastfmKey,
		HTTP:         &http.Client{Timeout: 6 * time.Second},
		cache:        make(map[string]string),
	}
}

func artCacheKey(artist, track, album string) string {
	return strings.ToLower(artist) + "\x00" + strings.ToLower(track) + "\x00" + strings.ToLower(album)
}

// CoverURL returns a public HTTPS image URL, or "" if none found.
func (a *AlbumArtClient) CoverURL(artist, track, album string) string {
	if a == nil {
		return ""
	}
	artist = strings.TrimSpace(artist)
	track = strings.TrimSpace(track)
	album = strings.TrimSpace(album)
	if artist == "" && track == "" {
		return ""
	}

	key := artCacheKey(artist, track, album)
	a.mu.Lock()
	if u, ok := a.cache[key]; ok {
		a.mu.Unlock()
		return u
	}
	a.mu.Unlock()

	var found string
	if a.LastFMAPIKey != "" {
		found = a.fromLastFM(artist, track, album)
	}
	if found == "" {
		found = a.fromITunes(artist, track, album)
	}

	a.mu.Lock()
	a.cache[key] = found
	a.mu.Unlock()
	return found
}

func (a *AlbumArtClient) httpClient() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return &http.Client{Timeout: 6 * time.Second}
}

func (a *AlbumArtClient) fromLastFM(artist, track, album string) string {
	// Prefer track.getInfo (has album images); fall back to album.getInfo.
	if track != "" && artist != "" {
		if u := a.lastfmTrackInfo(artist, track); u != "" {
			return u
		}
	}
	if album != "" && artist != "" {
		return a.lastfmAlbumInfo(artist, album)
	}
	return ""
}

func (a *AlbumArtClient) lastfmTrackInfo(artist, track string) string {
	q := url.Values{}
	q.Set("method", "track.getInfo")
	q.Set("api_key", a.LastFMAPIKey)
	q.Set("artist", artist)
	q.Set("track", track)
	q.Set("autocorrect", "1")
	q.Set("format", "json")

	var out struct {
		Track struct {
			Album struct {
				Image []struct {
					Size string `json:"size"`
					URL  string `json:"#text"`
				} `json:"image"`
			} `json:"album"`
		} `json:"track"`
	}
	if err := a.getJSON("https://ws.audioscrobbler.com/2.0/?"+q.Encode(), &out); err != nil {
		return ""
	}
	return pickLargestImage(out.Track.Album.Image)
}

func (a *AlbumArtClient) lastfmAlbumInfo(artist, album string) string {
	q := url.Values{}
	q.Set("method", "album.getInfo")
	q.Set("api_key", a.LastFMAPIKey)
	q.Set("artist", artist)
	q.Set("album", album)
	q.Set("autocorrect", "1")
	q.Set("format", "json")

	var out struct {
		Album struct {
			Image []struct {
				Size string `json:"size"`
				URL  string `json:"#text"`
			} `json:"image"`
		} `json:"album"`
	}
	if err := a.getJSON("https://ws.audioscrobbler.com/2.0/?"+q.Encode(), &out); err != nil {
		return ""
	}
	return pickLargestImage(out.Album.Image)
}

func pickLargestImage(images []struct {
	Size string `json:"size"`
	URL  string `json:"#text"`
}) string {
	// Prefer larger sizes.
	order := []string{"extralarge", "large", "medium", "small"}
	bySize := map[string]string{}
	for _, im := range images {
		u := strings.TrimSpace(im.URL)
		if u == "" {
			continue
		}
		bySize[im.Size] = u
	}
	for _, s := range order {
		if u := bySize[s]; u != "" {
			return u
		}
	}
	// any
	for _, im := range images {
		if u := strings.TrimSpace(im.URL); u != "" {
			return u
		}
	}
	return ""
}

func (a *AlbumArtClient) fromITunes(artist, track, album string) string {
	term := strings.TrimSpace(artist + " " + track)
	if term == "" {
		term = album
	}
	if term == "" {
		return ""
	}
	q := url.Values{}
	q.Set("term", term)
	q.Set("media", "music")
	q.Set("entity", "song")
	q.Set("limit", "5")

	var out struct {
		Results []struct {
			ArtistName   string `json:"artistName"`
			TrackName    string `json:"trackName"`
			Collection   string `json:"collectionName"`
			ArtworkURL100 string `json:"artworkUrl100"`
		} `json:"results"`
	}
	if err := a.getJSON("https://itunes.apple.com/search?"+q.Encode(), &out); err != nil {
		return ""
	}
	if len(out.Results) == 0 {
		return ""
	}

	// Prefer a result that roughly matches artist/track.
	pick := out.Results[0]
	al := strings.ToLower(artist)
	tl := strings.ToLower(track)
	for _, r := range out.Results {
		if al != "" && !strings.Contains(strings.ToLower(r.ArtistName), al) && !strings.Contains(al, strings.ToLower(r.ArtistName)) {
			continue
		}
		if tl != "" && !strings.EqualFold(r.TrackName, track) && !strings.Contains(strings.ToLower(r.TrackName), tl) {
			continue
		}
		pick = r
		break
	}
	return itunesHiRes(pick.ArtworkURL100)
}

// itunesHiRes upgrades 100x100 artwork to a larger square.
func itunesHiRes(u string) string {
	if u == "" {
		return ""
	}
	// Common patterns: 100x100bb, 100x100bb.jpg
	u = strings.Replace(u, "100x100bb", "600x600bb", 1)
	u = strings.Replace(u, "100x100", "600x600", 1)
	return u
}

func (a *AlbumArtClient) getJSON(rawURL string, dest any) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "lfm/2.0 (Apple Music scrobbler)")
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dest)
}

// NopArt always returns no cover.
type NopArt struct{}

func (NopArt) CoverURL(string, string, string) string { return "" }
