package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParseTrackOutput_Normal(t *testing.T) {
	tr, err := parseTrackOutput("playing|Song Name|The Artist|The Album|240.5|12.25")
	if err != nil {
		t.Fatal(err)
	}
	if tr.State != "playing" || tr.Name != "Song Name" || tr.Artist != "The Artist" {
		t.Fatalf("got %+v", tr)
	}
	if tr.Album != "The Album" {
		t.Fatalf("album = %q", tr.Album)
	}
	if tr.Duration != 240.5 || tr.Position != 12.25 {
		t.Fatalf("dur/pos = %v/%v", tr.Duration, tr.Position)
	}
}

func TestParseTrackOutput_Stopped(t *testing.T) {
	tr, err := parseTrackOutput("stopped||||0|0")
	if err != nil {
		t.Fatal(err)
	}
	if tr.State != "stopped" {
		t.Fatalf("state = %q", tr.State)
	}
	if tr.Name != "" || tr.Artist != "" {
		t.Fatalf("expected empty track: %+v", tr)
	}
}

func TestParseTrackOutput_PausedNormalized(t *testing.T) {
	tr, err := parseTrackOutput("Paused|A|B|C|100|10")
	if err != nil {
		t.Fatal(err)
	}
	if tr.State != "paused" {
		t.Fatalf("state = %q, want paused", tr.State)
	}
}

func TestParseTrackOutput_Incomplete(t *testing.T) {
	tr, err := parseTrackOutput("playing|only-two")
	if err != nil {
		t.Fatal(err)
	}
	if tr.State != "stopped" {
		t.Fatalf("incomplete should be stopped, got %q", tr.State)
	}
}

func TestParseTrackOutput_PipeInName(t *testing.T) {
	// name contains | → extra parts; rejoin logic should recover.
	tr, err := parseTrackOutput("playing|Part|One|Artist|Album|180|5")
	if err != nil {
		t.Fatal(err)
	}
	if tr.State != "playing" {
		t.Fatalf("state = %q", tr.State)
	}
	if tr.Name != "Part|One" {
		t.Fatalf("name = %q, want Part|One", tr.Name)
	}
	if tr.Artist != "Artist" || tr.Album != "Album" {
		t.Fatalf("artist/album = %q/%q", tr.Artist, tr.Album)
	}
	if tr.Duration != 180 || tr.Position != 5 {
		t.Fatalf("dur/pos = %v/%v", tr.Duration, tr.Position)
	}
}

func TestParseTrackOutput_Whitespace(t *testing.T) {
	tr, err := parseTrackOutput("  playing|X|Y|Z|60|1  \n")
	if err != nil {
		t.Fatal(err)
	}
	if tr.State != "playing" || tr.Name != "X" {
		t.Fatalf("got %+v", tr)
	}
}

func TestTrackSameTrack(t *testing.T) {
	a := Track{Name: "A", Artist: "B"}
	b := Track{Name: "A", Artist: "B", Album: "other"}
	c := Track{Name: "A", Artist: "C"}
	if !a.SameTrack(b) {
		t.Error("expected same")
	}
	if a.SameTrack(c) {
		t.Error("expected different")
	}
}

func TestAppleScriptMusic_SoftFailureOnExecError(t *testing.T) {
	m := &AppleScriptMusic{
		Timeout: time.Second,
		Exec: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, errors.New("Music is not running")
		},
	}
	tr, err := m.GetCurrentTrack()
	if err != nil {
		t.Fatalf("expected soft failure, got err: %v", err)
	}
	if tr.State != "stopped" {
		t.Fatalf("state = %q", tr.State)
	}
}

func TestAppleScriptMusic_Success(t *testing.T) {
	m := &AppleScriptMusic{
		Timeout: time.Second,
		Exec: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name != "osascript" {
				t.Errorf("name = %q", name)
			}
			return []byte("playing|Song|Artist|Album|200|10\n"), nil
		},
	}
	tr, err := m.GetCurrentTrack()
	if err != nil {
		t.Fatal(err)
	}
	if tr.Name != "Song" || tr.State != "playing" {
		t.Fatalf("got %+v", tr)
	}
}

func TestTrackString(t *testing.T) {
	if (Track{State: "stopped"}).String() != "stopped" {
		t.Fatal("stopped string")
	}
	s := (Track{State: "playing", Name: "N", Artist: "A", Album: "L", Position: 10, Duration: 100}).String()
	if s == "stopped" || s == "" {
		t.Fatalf("unexpected: %q", s)
	}
}
