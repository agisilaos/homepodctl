package main

import (
	"context"
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
