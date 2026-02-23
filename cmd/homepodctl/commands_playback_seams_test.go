package main

import (
	"context"
	"os"
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
		cmdOut(context.Background(), cfg, []string{"set", "--room", "Bedroom", "--json"})
	})
	if !called {
		t.Fatalf("expected setCurrentOutputs seam to be called")
	}
	if !strings.Contains(out, `"action": "out.set"`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestCmdOutSetFallsBackToPositionalRooms(t *testing.T) {
	origSetCurrentOutputs := setCurrentOutputs
	t.Cleanup(func() { setCurrentOutputs = origSetCurrentOutputs })

	var got []string
	setCurrentOutputs = func(_ context.Context, rooms []string) error {
		got = append([]string(nil), rooms...)
		return nil
	}

	cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "airplay"}}
	cmdOut(context.Background(), cfg, []string{"set", "Bedroom", "--dry-run"})
	if len(got) != 0 {
		t.Fatalf("dry-run should not call backend, got=%v", got)
	}

	cmdOut(context.Background(), cfg, []string{"set", "Bedroom"})
	if len(got) != 1 || got[0] != "Bedroom" {
		t.Fatalf("expected positional room fallback, got=%v", got)
	}
}

func TestChoosePlaylist_NoInput(t *testing.T) {
	t.Parallel()

	_, err := choosePlaylist([]music.UserPlaylist{
		{Name: "Focus", PersistentID: "A"},
		{Name: "Focus Mix", PersistentID: "B"},
	}, false)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "non-interactive") {
		t.Fatalf("expected non-interactive error, got: %v", err)
	}
}

func TestChoosePlaylist_RequiresInteractiveStdin(t *testing.T) {
	t.Parallel()

	orig := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	_ = w.Close()
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = orig
		_ = r.Close()
	})

	_, err = choosePlaylist([]music.UserPlaylist{
		{Name: "Focus", PersistentID: "A"},
		{Name: "Focus Mix", PersistentID: "B"},
	}, true)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "interactive stdin") {
		t.Fatalf("expected interactive stdin error, got: %v", err)
	}
}
