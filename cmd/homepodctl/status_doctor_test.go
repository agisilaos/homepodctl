package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func TestInferSelectedOutputs(t *testing.T) {
	t.Run("dedupes and trims output names", func(t *testing.T) {
		orig := playbackApp.nowPlayingFn
		t.Cleanup(func() { playbackApp.nowPlayingFn = orig })
		playbackApp.nowPlayingFn = func(context.Context) (music.NowPlaying, error) {
			return music.NowPlaying{Outputs: []music.AirPlayDevice{
				{Name: " Bedroom "},
				{Name: ""},
				{Name: "Bedroom"},
				{Name: "Living Room"},
			}}, nil
		}

		got := inferSelectedOutputs(context.Background())
		if len(got) != 2 || got[0] != "Bedroom" || got[1] != "Living Room" {
			t.Fatalf("inferSelectedOutputs=%v, want [Bedroom Living Room]", got)
		}
	})

	t.Run("returns nil on now-playing error", func(t *testing.T) {
		orig := playbackApp.nowPlayingFn
		t.Cleanup(func() { playbackApp.nowPlayingFn = orig })
		playbackApp.nowPlayingFn = func(context.Context) (music.NowPlaying, error) {
			return music.NowPlaying{}, errors.New("boom")
		}

		if got := inferSelectedOutputs(context.Background()); got != nil {
			t.Fatalf("inferSelectedOutputs=%v, want nil", got)
		}
	})
}

func TestSetVolumeForRooms(t *testing.T) {
	orig := playbackApp.setVolumeFn
	t.Cleanup(func() { playbackApp.setVolumeFn = orig })

	var got []string
	playbackApp.setVolumeFn = func(_ context.Context, room string, value int) error {
		got = append(got, room+":"+strconv.Itoa(value))
		if room == "Kitchen" {
			return errors.New("boom")
		}
		return nil
	}

	err := setVolumeForRooms(context.Background(), []string{"Bedroom", "Kitchen"}, 35)
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(got) != 2 {
		t.Fatalf("calls=%v, want 2 calls", got)
	}
}

func TestRunDoctorChecksUsesInjectedSeams(t *testing.T) {
	origLookPath := lookPath
	origConfigPath := configPath
	origLoadConfigOptional := loadConfigOptional
	origGetNowPlaying := playbackApp.nowPlayingFn
	t.Cleanup(func() {
		lookPath = origLookPath
		configPath = origConfigPath
		loadConfigOptional = origLoadConfigOptional
		playbackApp.nowPlayingFn = origGetNowPlaying
	})

	lookPath = func(name string) (string, error) {
		switch name {
		case "osascript":
			return "", errors.New("missing")
		case "shortcuts":
			return "/usr/bin/shortcuts", nil
		default:
			return "", errors.New("unexpected")
		}
	}
	configPath = func() (string, error) { return "/tmp/homepodctl/config.json", nil }
	loadConfigOptional = func() (*native.Config, error) {
		return &native.Config{Aliases: map[string]native.Alias{"bed": {Playlist: "Focus"}}}, nil
	}
	playbackApp.nowPlayingFn = func(context.Context) (music.NowPlaying, error) {
		return music.NowPlaying{}, errors.New("music unavailable")
	}

	report := runDoctorChecks(context.Background())
	if report.OK {
		t.Fatalf("report.OK=true, want false due to missing osascript")
	}

	statusByName := map[string]string{}
	for _, check := range report.Checks {
		statusByName[check.Name] = check.Status
	}
	if statusByName["osascript"] != "fail" {
		t.Fatalf("osascript status=%q", statusByName["osascript"])
	}
	if statusByName["shortcuts"] != "pass" {
		t.Fatalf("shortcuts status=%q", statusByName["shortcuts"])
	}
	if statusByName["config"] != "pass" {
		t.Fatalf("config status=%q", statusByName["config"])
	}
	if statusByName["music-backend"] != "warn" {
		t.Fatalf("music-backend status=%q", statusByName["music-backend"])
	}
}

type fakeStatusTicker struct {
	ch      chan time.Time
	stopped bool
}

func (f *fakeStatusTicker) Chan() <-chan time.Time { return f.ch }

func (f *fakeStatusTicker) Stop() { f.stopped = true }

func TestRunStatusLoop_NoWatchPrintsOnce(t *testing.T) {
	calls := 0
	err := runStatusLoop(context.Background(), 0, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("runStatusLoop: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
}

func TestRunStatusLoop_WatchStopsOnContextCancel(t *testing.T) {
	origTicker := newStatusTicker
	fake := &fakeStatusTicker{ch: make(chan time.Time)}
	newStatusTicker = func(_ time.Duration) statusTicker { return fake }
	t.Cleanup(func() { newStatusTicker = origTicker })

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	done := make(chan error, 1)
	go func() {
		done <- runStatusLoop(ctx, time.Second, func() error {
			calls++
			if calls == 2 {
				cancel()
			}
			return nil
		})
	}()

	fake.ch <- time.Now()
	err := <-done
	if err != nil {
		t.Fatalf("runStatusLoop: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
	if !fake.stopped {
		t.Fatalf("expected ticker.Stop to be called")
	}
}

func TestRunStatusLoop_PropagatesPrintError(t *testing.T) {
	errBoom := errors.New("boom")
	err := runStatusLoop(context.Background(), 0, func() error { return errBoom })
	if !errors.Is(err, errBoom) {
		t.Fatalf("err=%v, want %v", err, errBoom)
	}
}

func TestCollectStatus_Connected(t *testing.T) {
	origLookPath := lookPath
	origGetNowPlaying := playbackApp.nowPlayingFn
	t.Cleanup(func() {
		lookPath = origLookPath
		playbackApp.nowPlayingFn = origGetNowPlaying
	})

	lookPath = func(string) (string, error) { return "/usr/bin/osascript", nil }
	playbackApp.nowPlayingFn = func(context.Context) (music.NowPlaying, error) {
		return music.NowPlaying{
			PlayerState: "playing",
			Track: music.NowPlayingTrack{
				Name:   "Song",
				Artist: "Artist",
				Album:  "Album",
			},
			Outputs: []music.AirPlayDevice{
				{Name: "Bedroom", Volume: 30, Kind: "speaker"},
				{Name: "Living Room", Volume: 50, Kind: "speaker"},
			},
		}, nil
	}

	res, err := collectStatus(context.Background())
	if err != nil {
		t.Fatalf("collectStatus: %v", err)
	}
	if !res.OK {
		t.Fatalf("status ok=false")
	}
	if res.Player != "playing" {
		t.Fatalf("player=%q", res.Player)
	}
	if res.Connection.Music != "connected" || res.Connection.Automation != "granted" {
		t.Fatalf("connection=%+v", res.Connection)
	}
	if res.Volume == nil || *res.Volume != 40 {
		t.Fatalf("volume=%v", res.Volume)
	}
	if len(res.Route) != 2 || res.Route[0] != "Bedroom" || res.Route[1] != "Living Room" {
		t.Fatalf("route=%v", res.Route)
	}
}

func TestCollectStatus_MissingOsaScript(t *testing.T) {
	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })
	lookPath = func(string) (string, error) { return "", errors.New("missing") }

	res, err := collectStatus(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	if res.OK {
		t.Fatalf("status ok=true")
	}
	if res.Connection.Music != "missing" {
		t.Fatalf("music=%q", res.Connection.Music)
	}
}

func TestInferStatusConnection(t *testing.T) {
	scriptErr := func(output string) error {
		return &music.ScriptError{Err: errors.New("boom"), Output: output}
	}
	tests := []struct {
		name           string
		err            error
		wantMusic      string
		wantAutomation string
	}{
		{name: "auth denied", err: scriptErr("Not authorised to send Apple events"), wantMusic: "connected", wantAutomation: "denied"},
		{name: "connection invalid", err: scriptErr("Connection Invalid"), wantMusic: "unreachable", wantAutomation: "unknown"},
		{name: "generic script error", err: scriptErr("random"), wantMusic: "error", wantAutomation: "unknown"},
		{name: "deadline", err: context.DeadlineExceeded, wantMusic: "unreachable", wantAutomation: "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := inferStatusConnection(tc.err)
			if got.Music != tc.wantMusic || got.Automation != tc.wantAutomation {
				t.Fatalf("got=%+v wantMusic=%s wantAutomation=%s", got, tc.wantMusic, tc.wantAutomation)
			}
		})
	}
}

func TestCmdStatus_JSONIncludesConnectionState(t *testing.T) {
	origLookPath := lookPath
	origGetNowPlaying := playbackApp.nowPlayingFn
	t.Cleanup(func() {
		lookPath = origLookPath
		playbackApp.nowPlayingFn = origGetNowPlaying
	})

	lookPath = func(string) (string, error) { return "/usr/bin/osascript", nil }
	playbackApp.nowPlayingFn = func(context.Context) (music.NowPlaying, error) {
		return music.NowPlaying{
			PlayerState: "paused",
			Track:       music.NowPlayingTrack{Name: "Song"},
			Outputs:     []music.AirPlayDevice{{Name: "Bedroom", Volume: 25}},
		}, nil
	}

	out := captureStdout(t, func() {
		cmdStatus(context.Background(), []string{"--json"})
	})
	var payload statusResult
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("status json: %v: %s", err, out)
	}
	if !payload.OK || payload.Player != "paused" {
		t.Fatalf("payload=%+v", payload)
	}
	if payload.Connection.Music != "connected" {
		t.Fatalf("connection=%+v", payload.Connection)
	}
}

func TestFormatStatusSnapshotHeader(t *testing.T) {
	at := time.Date(2026, 2, 23, 8, 0, 0, 0, time.UTC)
	got := formatStatusSnapshotHeader(at, 2)
	want := "--- status snapshot 2 @ 2026-02-23T08:00:00Z ---"
	if got != want {
		t.Fatalf("header=%q want=%q", got, want)
	}
}
