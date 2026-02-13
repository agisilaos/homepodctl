package main

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func TestInferSelectedOutputs(t *testing.T) {
	t.Run("dedupes and trims output names", func(t *testing.T) {
		orig := getNowPlaying
		t.Cleanup(func() { getNowPlaying = orig })
		getNowPlaying = func(context.Context) (music.NowPlaying, error) {
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
		orig := getNowPlaying
		t.Cleanup(func() { getNowPlaying = orig })
		getNowPlaying = func(context.Context) (music.NowPlaying, error) {
			return music.NowPlaying{}, errors.New("boom")
		}

		if got := inferSelectedOutputs(context.Background()); got != nil {
			t.Fatalf("inferSelectedOutputs=%v, want nil", got)
		}
	})
}

func TestValidateAirplayVolumeSelection(t *testing.T) {
	tests := []struct {
		name           string
		volumeExplicit bool
		volume         int
		rooms          []string
		wantErr        bool
	}{
		{name: "explicit volume with no rooms errors", volumeExplicit: true, volume: 30, rooms: nil, wantErr: true},
		{name: "explicit volume with rooms passes", volumeExplicit: true, volume: 30, rooms: []string{"Bedroom"}, wantErr: false},
		{name: "implicit default volume with no rooms passes", volumeExplicit: false, volume: 30, rooms: nil, wantErr: false},
		{name: "negative volume bypasses check", volumeExplicit: true, volume: -1, rooms: nil, wantErr: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAirplayVolumeSelection(tc.volumeExplicit, tc.volume, tc.rooms)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateAirplayVolumeSelection() err=%v, wantErr=%t", err, tc.wantErr)
			}
		})
	}
}

func TestSetVolumeForRooms(t *testing.T) {
	orig := setDeviceVolume
	t.Cleanup(func() { setDeviceVolume = orig })

	var got []string
	setDeviceVolume = func(_ context.Context, room string, value int) error {
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

func TestResolveNativeShortcuts(t *testing.T) {
	cfg := &native.Config{
		Native: native.NativeConfig{
			Playlists:       map[string]map[string]string{"Bedroom": {"Focus": "Focus Shortcut"}},
			VolumeShortcuts: map[string]map[string]string{"Bedroom": {"30": "Volume 30 Shortcut"}},
		},
	}

	playlistShortcut, err := resolveNativePlaylistShortcut(cfg, "Bedroom", "Focus")
	if err != nil {
		t.Fatalf("resolveNativePlaylistShortcut: %v", err)
	}
	if playlistShortcut != "Focus Shortcut" {
		t.Fatalf("playlist shortcut=%q", playlistShortcut)
	}

	volumeShortcut, err := resolveNativeVolumeShortcut(cfg, "Bedroom", 30)
	if err != nil {
		t.Fatalf("resolveNativeVolumeShortcut: %v", err)
	}
	if volumeShortcut != "Volume 30 Shortcut" {
		t.Fatalf("volume shortcut=%q", volumeShortcut)
	}

	if _, err := resolveNativePlaylistShortcut(cfg, "Bedroom", "Missing"); err == nil {
		t.Fatalf("expected missing playlist mapping error")
	}
	if _, err := resolveNativeVolumeShortcut(cfg, "Bedroom", 99); err == nil {
		t.Fatalf("expected missing volume mapping error")
	}
}

func TestRunNativeShortcutsUsesResolvedMappings(t *testing.T) {
	orig := runNativeShortcut
	t.Cleanup(func() { runNativeShortcut = orig })

	cfg := &native.Config{
		Native: native.NativeConfig{
			Playlists:       map[string]map[string]string{"Bedroom": {"Focus": "Focus Shortcut"}},
			VolumeShortcuts: map[string]map[string]string{"Bedroom": {"30": "Volume 30 Shortcut"}},
		},
	}

	var calls []string
	runNativeShortcut = func(_ context.Context, name string) error {
		calls = append(calls, name)
		return nil
	}

	if err := runNativePlaylistShortcuts(context.Background(), cfg, []string{"Bedroom"}, "Focus"); err != nil {
		t.Fatalf("runNativePlaylistShortcuts: %v", err)
	}
	if err := runNativeVolumeShortcuts(context.Background(), cfg, []string{"Bedroom"}, 30); err != nil {
		t.Fatalf("runNativeVolumeShortcuts: %v", err)
	}
	if len(calls) != 2 || calls[0] != "Focus Shortcut" || calls[1] != "Volume 30 Shortcut" {
		t.Fatalf("shortcut calls=%v", calls)
	}
}

func TestRunDoctorChecksUsesInjectedSeams(t *testing.T) {
	origLookPath := lookPath
	origConfigPath := configPath
	origLoadConfigOptional := loadConfigOptional
	origGetNowPlaying := getNowPlaying
	t.Cleanup(func() {
		lookPath = origLookPath
		configPath = origConfigPath
		loadConfigOptional = origLoadConfigOptional
		getNowPlaying = origGetNowPlaying
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
	getNowPlaying = func(context.Context) (music.NowPlaying, error) {
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
