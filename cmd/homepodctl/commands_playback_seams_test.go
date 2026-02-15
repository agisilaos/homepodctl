package main

import (
	"context"
	"strings"
	"testing"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func TestCmdTransportUsesGetNowPlayingSeam(t *testing.T) {
	origGetNowPlaying := getNowPlaying
	t.Cleanup(func() { getNowPlaying = origGetNowPlaying })

	getNowPlaying = func(context.Context) (music.NowPlaying, error) {
		return music.NowPlaying{
			PlayerState: "paused",
			Track:       music.NowPlayingTrack{Name: "Test Song"},
		}, nil
	}

	out := captureStdout(t, func() {
		cmdTransport(context.Background(), []string{"--json"}, "pause", func(context.Context) error { return nil })
	})
	if !strings.Contains(out, `"action": "pause"`) {
		t.Fatalf("missing action in output: %s", out)
	}
	if !strings.Contains(out, `"nowPlaying"`) {
		t.Fatalf("expected nowPlaying in output: %s", out)
	}
}

func TestCmdOutSetUsesSetCurrentOutputsSeam(t *testing.T) {
	origSetCurrentOutputs := setCurrentOutputs
	origGetNowPlaying := getNowPlaying
	t.Cleanup(func() {
		setCurrentOutputs = origSetCurrentOutputs
		getNowPlaying = origGetNowPlaying
	})

	called := false
	setCurrentOutputs = func(_ context.Context, rooms []string) error {
		called = true
		if len(rooms) != 1 || rooms[0] != "Bedroom" {
			t.Fatalf("unexpected rooms=%v", rooms)
		}
		return nil
	}
	getNowPlaying = func(context.Context) (music.NowPlaying, error) {
		return music.NowPlaying{PlayerState: "playing"}, nil
	}

	cfg := &native.Config{
		Defaults: native.DefaultsConfig{
			Backend: "airplay",
		},
	}
	out := captureStdout(t, func() {
		cmdOut(context.Background(), cfg, []string{"set", "Bedroom", "--json"})
	})
	if !called {
		t.Fatalf("expected setCurrentOutputs seam to be called")
	}
	if !strings.Contains(out, `"action": "out.set"`) {
		t.Fatalf("unexpected output: %s", out)
	}
}
