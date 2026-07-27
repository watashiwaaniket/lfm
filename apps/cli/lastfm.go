package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const lastfmAPIEndpoint = "https://ws.audioscrobbler.com/2.0/"

// LastFMClient talks to the official Last.fm API.
type LastFMClient struct {
	APIKey     string
	APISecret  string
	SessionKey string
	HTTPClient *http.Client
	Endpoint   string // overridable for tests
}

// NewLastFMClient creates a client with a sensible HTTP timeout.
func NewLastFMClient(apiKey, apiSecret, sessionKey string) *LastFMClient {
	return &LastFMClient{
		APIKey:     apiKey,
		APISecret:  apiSecret,
		SessionKey: sessionKey,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		Endpoint:   lastfmAPIEndpoint,
	}
}

// apiSig builds the Last.fm api_sig: sorted key+value pairs concatenated,
// secret appended, then MD5 hex digest. Do not include "format" or "api_sig".
func apiSig(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "format" || k == "api_sig" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(params[k])
	}
	b.WriteString(secret)

	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// lastfmError is the JSON error shape returned by Last.fm.
type lastfmError struct {
	Code    int    `json:"error"`
	Message string `json:"message"`
}

func (e *lastfmError) Error() string {
	return fmt.Sprintf("last.fm error %d: %s", e.Code, e.Message)
}

// callPOST sends a signed form-urlencoded POST and decodes JSON into dest.
// dest may be nil if only success/failure matters.
func (c *LastFMClient) callPOST(method string, params map[string]string, dest any) error {
	if params == nil {
		params = make(map[string]string)
	}
	params["method"] = method
	params["api_key"] = c.APIKey
	params["format"] = "json"
	params["api_sig"] = apiSig(params, c.APISecret)

	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}

	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = lastfmAPIEndpoint
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http post %s: %w", method, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	// Last.fm returns HTTP 200 even for API errors; check JSON "error" field.
	var apiErr lastfmError
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Code != 0 {
		return &apiErr
	}

	if dest != nil {
		if err := json.Unmarshal(body, dest); err != nil {
			return fmt.Errorf("decode response for %s: %w (body=%s)", method, err, truncate(string(body), 200))
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// GetToken calls auth.getToken.
func (c *LastFMClient) GetToken() (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	if err := c.callPOST("auth.getToken", nil, &out); err != nil {
		return "", err
	}
	if out.Token == "" {
		return "", fmt.Errorf("auth.getToken: empty token")
	}
	return out.Token, nil
}

// AuthURL returns the browser URL where the user authorizes the app.
func (c *LastFMClient) AuthURL(token string) string {
	return fmt.Sprintf("https://www.last.fm/api/auth/?api_key=%s&token=%s",
		url.QueryEscape(c.APIKey), url.QueryEscape(token))
}

// GetSession exchanges an authorized token for a session key.
func (c *LastFMClient) GetSession(token string) (username, sessionKey string, err error) {
	var out struct {
		Session struct {
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"session"`
	}
	params := map[string]string{"token": token}
	if err := c.callPOST("auth.getSession", params, &out); err != nil {
		return "", "", err
	}
	if out.Session.Key == "" {
		return "", "", fmt.Errorf("auth.getSession: empty session key")
	}
	return out.Session.Name, out.Session.Key, nil
}

// UpdateNowPlaying posts track.updateNowPlaying.
func (c *LastFMClient) UpdateNowPlaying(artist, track, album string, durationSec int) error {
	params := map[string]string{
		"sk":     c.SessionKey,
		"artist": artist,
		"track":  track,
	}
	if album != "" {
		params["album"] = album
	}
	if durationSec > 0 {
		params["duration"] = strconv.Itoa(durationSec)
	}
	return c.callPOST("track.updateNowPlaying", params, nil)
}

// Scrobble posts track.scrobble. Uses artist[0]/track[0]/timestamp[0]
// even for a single track (Last.fm batch form).
func (c *LastFMClient) Scrobble(artist, track, album string, timestamp int64, durationSec int) error {
	params := map[string]string{
		"sk":             c.SessionKey,
		"artist[0]":      artist,
		"track[0]":       track,
		"timestamp[0]":   strconv.FormatInt(timestamp, 10),
	}
	if album != "" {
		params["album[0]"] = album
	}
	if durationSec > 0 {
		params["duration[0]"] = strconv.Itoa(durationSec)
	}
	return c.callPOST("track.scrobble", params, nil)
}
