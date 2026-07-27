package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTruncateRunes(t *testing.T) {
	if truncateRunes("hello", 10) != "hello" {
		t.Fatal("short string")
	}
	got := truncateRunes("abcdefghij", 5)
	if got != "abcd…" {
		t.Fatalf("got %q", got)
	}
}

func TestPresenceKey(t *testing.T) {
	tr := Track{Name: "A", Artist: "B", Album: "C"}
	if presenceKey(tr, true) == presenceKey(tr, false) {
		t.Fatal("playing vs paused should differ")
	}
	if presenceKey(tr, true) != presenceKey(Track{Name: "A", Artist: "B", Album: "C"}, true) {
		t.Fatal("same track should match")
	}
}

func TestNopPresence(t *testing.T) {
	var p Presence = NopPresence{}
	if err := p.SetPlaying(Track{Name: "x"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := p.Clear(); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDiscordPresence_HandshakeAndSetActivity(t *testing.T) {
	// Short path: macOS rejects very long unix socket paths.
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("lfm-d-%d", time.Now().UnixNano()%1e9))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	sockPath := filepath.Join(dir, "discord-ipc-0")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	type result struct {
		opcodes []uint32
		bodies  []map[string]any
		err     error
	}
	done := make(chan result, 1)

	go func() {
		var r result
		defer func() { done <- r }()
		conn, err := ln.Accept()
		if err != nil {
			r.err = err
			return
		}
		defer conn.Close()
		// handshake
		op, body, err := readFrame(conn)
		if err != nil {
			r.err = err
			return
		}
		r.opcodes = append(r.opcodes, op)
		var hs map[string]any
		_ = json.Unmarshal(body, &hs)
		r.bodies = append(r.bodies, hs)
		// reply READY-ish
		reply, _ := json.Marshal(map[string]any{"evt": "READY", "cmd": "DISPATCH"})
		var hdr [8]byte
		binary.LittleEndian.PutUint32(hdr[0:4], 1)
		binary.LittleEndian.PutUint32(hdr[4:8], uint32(len(reply)))
		_, _ = conn.Write(hdr[:])
		_, _ = conn.Write(reply)

		// SET_ACTIVITY
		op, body, err = readFrame(conn)
		if err != nil {
			r.err = err
			return
		}
		r.opcodes = append(r.opcodes, op)
		var frame map[string]any
		_ = json.Unmarshal(body, &frame)
		r.bodies = append(r.bodies, frame)
	}()

	t.Setenv("TMPDIR", dir)

	art := &stubArt{url: "https://cdn.example/cover.jpg"}
	d := NewDiscordPresence("1234567890", "lfm", art)
	err = d.SetPlaying(Track{
		Name:     "Twitterloser",
		Artist:   "AViT",
		Album:    "Twitterloser - Single",
		State:    "playing",
		Duration: 180,
	}, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("SetPlaying: %v", err)
	}

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("server: %v", r.err)
		}
		if len(r.opcodes) < 2 {
			t.Fatalf("opcodes = %v", r.opcodes)
		}
		if r.opcodes[0] != 0 {
			t.Fatalf("handshake opcode = %d", r.opcodes[0])
		}
		if r.bodies[0]["client_id"] != "1234567890" {
			t.Fatalf("client_id = %v", r.bodies[0]["client_id"])
		}
		if r.opcodes[1] != 1 {
			t.Fatalf("frame opcode = %d", r.opcodes[1])
		}
		if r.bodies[1]["cmd"] != "SET_ACTIVITY" {
			t.Fatalf("cmd = %v", r.bodies[1]["cmd"])
		}
		args, _ := r.bodies[1]["args"].(map[string]any)
		act, _ := args["activity"].(map[string]any)
		if act["details"] != "Twitterloser" {
			t.Fatalf("details = %v", act["details"])
		}
		if act["state"] != "AViT" {
			t.Fatalf("state = %v", act["state"])
		}
		if act["type"].(float64) != 2 {
			t.Fatalf("type = %v want Listening(2)", act["type"])
		}
		assets, _ := act["assets"].(map[string]any)
		if assets["large_image"] != "https://cdn.example/cover.jpg" {
			t.Fatalf("large_image = %v", assets["large_image"])
		}
		buttons, _ := act["buttons"].([]any)
		if len(buttons) != 1 {
			t.Fatalf("buttons = %v", buttons)
		}
		btn, _ := buttons[0].(map[string]any)
		if btn["label"] != "Apple Music powered by lfm" {
			t.Fatalf("button label = %v", btn["label"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server timeout")
	}
	_ = d.Close()
}

type stubArt struct{ url string }

func (s *stubArt) CoverURL(artist, track, album string) string { return s.url }
