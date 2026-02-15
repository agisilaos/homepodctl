package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func TestValidateConfigValues_FindsMultipleIssues(t *testing.T) {
	t.Parallel()

	v := 120
	cfg := &native.Config{
		Defaults: native.DefaultsConfig{
			Backend: "bad",
			Rooms:   []string{"", "Bedroom"},
			Volume:  &v,
		},
		Aliases: map[string]native.Alias{
			"night": {
				Backend: "unknown",
				Rooms:   []string{""},
				Volume:  &v,
			},
		},
		Native: native.NativeConfig{
			Playlists: map[string]map[string]string{
				"": {"Focus": "x"},
			},
			VolumeShortcuts: map[string]map[string]string{
				"Bedroom": {"999": ""},
			},
		},
	}

	issues := validateConfigValues(cfg)
	if len(issues) < 5 {
		t.Fatalf("issues=%v", issues)
	}
}

func TestConfigPathGetSet_RoundTrip(t *testing.T) {
	t.Parallel()

	cfg := &native.Config{
		Defaults: native.DefaultsConfig{
			Backend: "airplay",
			Rooms:   []string{"Bedroom"},
		},
		Aliases: map[string]native.Alias{},
		Native: native.NativeConfig{
			Playlists:       map[string]map[string]string{},
			VolumeShortcuts: map[string]map[string]string{},
		},
	}

	if err := setConfigPathValue(cfg, "aliases.work.backend", []string{"native"}); err != nil {
		t.Fatalf("set alias backend: %v", err)
	}
	if err := setConfigPathValue(cfg, "aliases.work.shuffle", []string{"true"}); err != nil {
		t.Fatalf("set alias shuffle: %v", err)
	}
	if err := setConfigPathValue(cfg, "native.playlists.Bedroom.Focus", []string{"BR Focus"}); err != nil {
		t.Fatalf("set native playlist mapping: %v", err)
	}
	if err := setConfigPathValue(cfg, "native.volumeShortcuts.Bedroom.30", []string{"BR Vol 30"}); err != nil {
		t.Fatalf("set native volume mapping: %v", err)
	}

	got, err := getConfigPathValue(cfg, "aliases.work.backend")
	if err != nil || got != "native" {
		t.Fatalf("get alias backend got=%v err=%v", got, err)
	}
	got, err = getConfigPathValue(cfg, "native.playlists.Bedroom.Focus")
	if err != nil || got != "BR Focus" {
		t.Fatalf("get native playlist got=%v err=%v", got, err)
	}
	got, err = getConfigPathValue(cfg, "native.volumeShortcuts.Bedroom.30")
	if err != nil || got != "BR Vol 30" {
		t.Fatalf("get native volume got=%v err=%v", got, err)
	}
}

func TestSetConfigPathValue_RejectsInvalidInput(t *testing.T) {
	t.Parallel()

	cfg := &native.Config{
		Aliases: map[string]native.Alias{},
	}
	if err := setConfigPathValue(cfg, "defaults.backend", []string{"bad"}); err == nil {
		t.Fatalf("expected invalid backend error")
	}
	if err := setConfigPathValue(cfg, "native.volumeShortcuts.Bedroom.xyz", []string{"x"}); err == nil {
		t.Fatalf("expected invalid volume key error")
	}
}

func TestSetConfigPathValue_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		key     string
		values  []string
		wantErr bool
	}{
		{name: "defaults backend", key: "defaults.backend", values: []string{"native"}},
		{name: "defaults volume null", key: "defaults.volume", values: []string{"null"}},
		{name: "defaults rooms", key: "defaults.rooms", values: []string{"Bedroom", "Kitchen"}},
		{name: "alias playlist id", key: "aliases.evening.playlistId", values: []string{"ABC123"}},
		{name: "alias shuffle null", key: "aliases.evening.shuffle", values: []string{"null"}},
		{name: "native playlist mapping", key: "native.playlists.Bedroom.Focus", values: []string{"BR Focus"}},
		{name: "native volume mapping", key: "native.volumeShortcuts.Bedroom.25", values: []string{"BR Vol 25"}},
		{name: "bad alias path", key: "aliases..backend", values: []string{"airplay"}, wantErr: true},
		{name: "bad native volume key", key: "native.volumeShortcuts.Bedroom.xx", values: []string{"x"}, wantErr: true},
		{name: "unknown path", key: "defaults.nope", values: []string{"x"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &native.Config{
				Aliases: map[string]native.Alias{},
				Native: native.NativeConfig{
					Playlists:       map[string]map[string]string{},
					VolumeShortcuts: map[string]map[string]string{},
				},
			}
			err := setConfigPathValue(cfg, tc.key, tc.values)
			if (err != nil) != tc.wantErr {
				t.Fatalf("setConfigPathValue err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestGetConfigPathValue_Table(t *testing.T) {
	t.Parallel()

	v := 35
	b := true
	cfg := &native.Config{
		Defaults: native.DefaultsConfig{
			Backend: "airplay",
			Rooms:   []string{"Bedroom", "Kitchen"},
			Volume:  &v,
			Shuffle: true,
		},
		Aliases: map[string]native.Alias{
			"focus": {
				Backend:    "native",
				Rooms:      []string{"Bedroom"},
				Playlist:   "Deep Focus",
				PlaylistID: "P123",
				Shuffle:    &b,
				Volume:     &v,
				Shortcut:   "Focus Shortcut",
			},
		},
		Native: native.NativeConfig{
			Playlists: map[string]map[string]string{
				"Bedroom": {"Deep Focus": "BR Focus"},
			},
			VolumeShortcuts: map[string]map[string]string{
				"Bedroom": {"35": "BR Vol 35"},
			},
		},
	}

	tests := []struct {
		key     string
		want    any
		wantErr bool
	}{
		{key: "defaults.backend", want: "airplay"},
		{key: "defaults.rooms", want: []string{"Bedroom", "Kitchen"}},
		{key: "aliases.focus.playlistId", want: "P123"},
		{key: "native.playlists.Bedroom.Deep Focus", want: "BR Focus"},
		{key: "native.volumeShortcuts.Bedroom.35", want: "BR Vol 35"},
		{key: "aliases.missing.backend", wantErr: true},
		{key: "no.such.path", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			got, err := getConfigPathValue(cfg, tc.key)
			if (err != nil) != tc.wantErr {
				t.Fatalf("getConfigPathValue err=%v wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got=%#v want=%#v", got, tc.want)
			}
		})
	}
}

func TestGetConfigPathValue_NilOptionalsAndInvalidShapes(t *testing.T) {
	t.Parallel()

	cfg := &native.Config{
		Defaults: native.DefaultsConfig{
			Backend: "airplay",
		},
		Aliases: map[string]native.Alias{
			"focus": {
				Playlist: "Focus",
			},
		},
		Native: native.NativeConfig{
			Playlists:       map[string]map[string]string{},
			VolumeShortcuts: map[string]map[string]string{},
		},
	}

	got, err := getConfigPathValue(cfg, "defaults.volume")
	if err != nil || got != nil {
		t.Fatalf("defaults.volume got=%v err=%v, want nil,nil", got, err)
	}
	got, err = getConfigPathValue(cfg, "aliases.focus.shuffle")
	if err != nil || got != nil {
		t.Fatalf("aliases.focus.shuffle got=%v err=%v, want nil,nil", got, err)
	}
	got, err = getConfigPathValue(cfg, "aliases.focus.volume")
	if err != nil || got != nil {
		t.Fatalf("aliases.focus.volume got=%v err=%v, want nil,nil", got, err)
	}

	invalid := []string{
		"aliases.focus.backend.extra",
		"native.playlists.Bedroom",
		"native.playlists..Focus",
		"native.volumeShortcuts.Bedroom.",
		"native.volumeShortcuts.Bedroom.30.extra",
	}
	for _, key := range invalid {
		t.Run(key, func(t *testing.T) {
			if _, err := getConfigPathValue(cfg, key); err == nil {
				t.Fatalf("expected error for key=%q", key)
			}
		})
	}
}

func TestSetConfigPathValue_NullsAndInvalidShapes(t *testing.T) {
	t.Parallel()

	cfg := &native.Config{}

	if err := setConfigPathValue(cfg, "defaults.volume", []string{"null"}); err != nil {
		t.Fatalf("defaults.volume null: %v", err)
	}
	if cfg.Defaults.Volume != nil {
		t.Fatalf("defaults.volume expected nil, got %v", *cfg.Defaults.Volume)
	}

	if err := setConfigPathValue(cfg, "aliases.focus.shuffle", []string{"null"}); err != nil {
		t.Fatalf("aliases.focus.shuffle null: %v", err)
	}
	if got := cfg.Aliases["focus"].Shuffle; got != nil {
		t.Fatalf("aliases.focus.shuffle expected nil, got %v", *got)
	}

	if err := setConfigPathValue(cfg, "aliases.focus.volume", []string{"null"}); err != nil {
		t.Fatalf("aliases.focus.volume null: %v", err)
	}
	if got := cfg.Aliases["focus"].Volume; got != nil {
		t.Fatalf("aliases.focus.volume expected nil, got %v", *got)
	}

	bad := []struct {
		key    string
		values []string
	}{
		{key: "defaults.backend", values: []string{}},
		{key: "defaults.shuffle", values: []string{"true", "false"}},
		{key: "aliases.focus.backend.extra", values: []string{"airplay"}},
		{key: "native.playlists.Bedroom", values: []string{"Shortcut"}},
		{key: "native.playlists..Focus", values: []string{"Shortcut"}},
		{key: "native.volumeShortcuts.Bedroom.200", values: []string{"Shortcut"}},
		{key: "native.volumeShortcuts.Bedroom.10", values: []string{}},
	}
	for _, tc := range bad {
		t.Run(tc.key, func(t *testing.T) {
			if err := setConfigPathValue(cfg, tc.key, tc.values); err == nil {
				t.Fatalf("expected error for key=%q values=%v", tc.key, tc.values)
			}
		})
	}
}

func TestParseAutomationBytes_JSON(t *testing.T) {
	t.Parallel()

	doc, err := parseAutomationBytes([]byte(`{"version":"1","name":"json","steps":[{"type":"transport","action":"stop"}]}`))
	if err != nil {
		t.Fatalf("parse json automation: %v", err)
	}
	if doc.Name != "json" || len(doc.Steps) != 1 || doc.Steps[0].Type != "transport" {
		t.Fatalf("unexpected doc: %+v", doc)
	}
}

func TestExecuteAutomationVolume_AirplayUsesGivenRooms(t *testing.T) {
	origSetDeviceVolume := setDeviceVolume
	t.Cleanup(func() { setDeviceVolume = origSetDeviceVolume })

	calls := 0
	setDeviceVolume = func(_ context.Context, room string, value int) error {
		calls++
		if room != "Bedroom" || value != 35 {
			t.Fatalf("unexpected setDeviceVolume args room=%q value=%d", room, value)
		}
		return nil
	}

	err := executeAutomationVolume(context.Background(), nil, "airplay", automationDefaults{}, 35, []string{"Bedroom"})
	if err != nil {
		t.Fatalf("executeAutomationVolume: %v", err)
	}
	if calls != 1 {
		t.Fatalf("setDeviceVolume calls=%d, want 1", calls)
	}
}

func TestExecuteAutomationWait_SuccessAndTimeout(t *testing.T) {
	origGetNowPlaying := getNowPlaying
	origSleepFn := sleepFn
	t.Cleanup(func() {
		getNowPlaying = origGetNowPlaying
		sleepFn = origSleepFn
	})

	getNowPlaying = func(context.Context) (music.NowPlaying, error) {
		return music.NowPlaying{PlayerState: "playing"}, nil
	}
	sleepFn = func(time.Duration) {}
	if err := executeAutomationWait(context.Background(), "playing", "50ms"); err != nil {
		t.Fatalf("executeAutomationWait success: %v", err)
	}

	getNowPlaying = func(context.Context) (music.NowPlaying, error) {
		return music.NowPlaying{PlayerState: "paused"}, nil
	}
	err := executeAutomationWait(context.Background(), "playing", "20ms")
	if err == nil || !strings.Contains(err.Error(), "wait timeout") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}
