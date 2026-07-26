package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAPISig_DeterministicAndExcludesFormat(t *testing.T) {
	params := map[string]string{
		"method":  "auth.getToken",
		"api_key": "mykey",
		"format":  "json", // must NOT be included in signature
	}
	secret := "mysecret"

	// Manual expected: alphabetically sorted key+value, then secret.
	// api_key + mykey + method + auth.getToken + mysecret
	raw := "api_key" + "mykey" + "method" + "auth.getToken" + secret
	sum := md5.Sum([]byte(raw))
	want := hex.EncodeToString(sum[:])

	got := apiSig(params, secret)
	if got != want {
		t.Fatalf("apiSig = %q, want %q", got, want)
	}
}

func TestAPISig_SortsKeysAlphabetically(t *testing.T) {
	// Insertion order should not matter.
	a := apiSig(map[string]string{"z": "1", "a": "2", "m": "3"}, "s")
	b := apiSig(map[string]string{"m": "3", "z": "1", "a": "2"}, "s")
	if a != b {
		t.Fatalf("signature not stable under key reordering: %q vs %q", a, b)
	}

	raw := "a2m3z1s"
	sum := md5.Sum([]byte(raw))
	want := hex.EncodeToString(sum[:])
	if a != want {
		t.Fatalf("apiSig = %q, want %q", a, want)
	}
}

func TestAPISig_SkipsApiSigKey(t *testing.T) {
	params := map[string]string{
		"method":  "x",
		"api_key": "k",
		"api_sig": "should-be-ignored",
	}
	got := apiSig(params, "sec")
	raw := "api_key" + "k" + "method" + "x" + "sec"
	sum := md5.Sum([]byte(raw))
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("apiSig = %q, want %q", got, want)
	}
}

func TestGetToken_Success(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		vals, _ := url.ParseQuery(string(body))
		gotMethod = vals.Get("method")
		if vals.Get("api_sig") == "" {
			t.Error("missing api_sig")
		}
		if vals.Get("format") != "json" {
			t.Errorf("format = %q", vals.Get("format"))
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok123"})
	}))
	defer srv.Close()

	c := NewLastFMClient("key", "secret", "")
	c.Endpoint = srv.URL
	c.HTTPClient = srv.Client()

	tok, err := c.GetToken()
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if tok != "tok123" {
		t.Fatalf("token = %q, want tok123", tok)
	}
	if gotMethod != "auth.getToken" {
		t.Fatalf("method = %q", gotMethod)
	}
}

func TestGetToken_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":   10,
			"message": "Invalid API key",
		})
	}))
	defer srv.Close()

	c := NewLastFMClient("bad", "secret", "")
	c.Endpoint = srv.URL
	c.HTTPClient = srv.Client()

	_, err := c.GetToken()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "10") {
		t.Fatalf("error should mention code: %v", err)
	}
}

func TestGetSession_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		vals, _ := url.ParseQuery(string(body))
		if vals.Get("method") != "auth.getSession" {
			t.Errorf("method = %q", vals.Get("method"))
		}
		if vals.Get("token") != "tok" {
			t.Errorf("token = %q", vals.Get("token"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session": map[string]string{
				"name": "listener",
				"key":  "sess-abc",
			},
		})
	}))
	defer srv.Close()

	c := NewLastFMClient("key", "secret", "")
	c.Endpoint = srv.URL
	c.HTTPClient = srv.Client()

	user, sk, err := c.GetSession("tok")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if user != "listener" || sk != "sess-abc" {
		t.Fatalf("user=%q sk=%q", user, sk)
	}
}

func TestAuthURL(t *testing.T) {
	c := NewLastFMClient("my key", "sec", "")
	u := c.AuthURL("tok&1")
	if !strings.Contains(u, "api_key=my+key") && !strings.Contains(u, "api_key=my%20key") {
		// url.QueryEscape uses + for spaces
		if !strings.Contains(u, "api_key=") {
			t.Fatalf("missing api_key in %q", u)
		}
	}
	if !strings.Contains(u, "token=tok") {
		t.Fatalf("missing token in %q", u)
	}
	if !strings.HasPrefix(u, "https://www.last.fm/api/auth/") {
		t.Fatalf("unexpected URL: %q", u)
	}
}

func TestUpdateNowPlaying_AndScrobble(t *testing.T) {
	var methods []string
	var lastForm url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		vals, _ := url.ParseQuery(string(body))
		methods = append(methods, vals.Get("method"))
		lastForm = vals
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := NewLastFMClient("key", "secret", "sk-1")
	c.Endpoint = srv.URL
	c.HTTPClient = srv.Client()

	if err := c.UpdateNowPlaying("Artist", "Song", "Album", 200); err != nil {
		t.Fatalf("UpdateNowPlaying: %v", err)
	}
	if methods[0] != "track.updateNowPlaying" {
		t.Fatalf("method = %q", methods[0])
	}

	if err := c.Scrobble("Artist", "Song", "Album", 1700000000, 200); err != nil {
		t.Fatalf("Scrobble: %v", err)
	}
	if methods[1] != "track.scrobble" {
		t.Fatalf("method = %q", methods[1])
	}
	// Last.fm batch keys for single-track scrobble
	if lastForm.Get("artist[0]") != "Artist" {
		t.Fatalf("artist[0] = %q", lastForm.Get("artist[0]"))
	}
	if lastForm.Get("track[0]") != "Song" {
		t.Fatalf("track[0] = %q", lastForm.Get("track[0]"))
	}
	if lastForm.Get("timestamp[0]") != "1700000000" {
		t.Fatalf("timestamp[0] = %q", lastForm.Get("timestamp[0]"))
	}
	if lastForm.Get("sk") != "sk-1" {
		t.Fatalf("sk = %q", lastForm.Get("sk"))
	}
	if lastForm.Get("api_sig") == "" {
		t.Fatal("missing api_sig on scrobble")
	}
}
