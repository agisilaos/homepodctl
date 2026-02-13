package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func TestAutomationParseAndValidateYAML(t *testing.T) {
	t.Parallel()
	doc, err := parseAutomationBytes([]byte(`version: "1"
name: morning
steps:
  - type: out.set
    rooms: ["Bedroom"]
  - type: play
    query: "Morning Mix"
  - type: volume.set
    value: 30
  - type: wait
    state: playing
    timeout: 20s
`))
	if err != nil {
		t.Fatalf("parseAutomationBytes: %v", err)
	}
	if err := validateAutomation(doc); err != nil {
		t.Fatalf("validateAutomation: %v", err)
	}
}

func TestAutomationValidateRejectsInvalidPlayStep(t *testing.T) {
	t.Parallel()
	doc := &automationFile{
		Version: "1",
		Name:    "bad",
		Steps: []automationStep{{
			Type:       "play",
			Query:      "x",
			PlaylistID: "ABC",
		}},
	}
	err := validateAutomation(doc)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "exactly one of query or playlistId") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAutomationPreset(t *testing.T) {
	t.Parallel()
	doc, err := automationPreset("focus")
	if err != nil {
		t.Fatalf("automationPreset: %v", err)
	}
	if doc.Name != "focus" {
		t.Fatalf("name=%q, want focus", doc.Name)
	}
	if len(doc.Steps) == 0 {
		t.Fatalf("expected steps")
	}
	if _, err := automationPreset("unknown"); err == nil {
		t.Fatalf("expected error for unknown preset")
	}
}

func TestBuildAutomationResultJSONShape(t *testing.T) {
	t.Parallel()
	doc := &automationFile{
		Version: "1",
		Name:    "morning",
		Steps:   []automationStep{{Type: "out.set", Rooms: []string{"Bedroom"}}},
	}
	steps := resolveAutomationSteps(nil, doc)
	res := buildAutomationResult("dry-run", doc, steps)
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(b), `"mode":"dry-run"`) {
		t.Fatalf("missing mode in json: %s", string(b))
	}
	if !strings.Contains(string(b), `"steps"`) {
		t.Fatalf("missing steps in json: %s", string(b))
	}
}

func TestExecuteAutomationSteps_StopsOnFailure(t *testing.T) {
	origSetCurrentOutputs := setCurrentOutputs
	origSetDeviceVolume := setDeviceVolume
	origSetShuffle := setShuffle
	origSearchPlaylists := searchPlaylists
	origPlayPlaylistByID := playPlaylistByID
	t.Cleanup(func() {
		setCurrentOutputs = origSetCurrentOutputs
		setDeviceVolume = origSetDeviceVolume
		setShuffle = origSetShuffle
		searchPlaylists = origSearchPlaylists
		playPlaylistByID = origPlayPlaylistByID
	})

	setCurrentOutputs = func(context.Context, []string) error { return errors.New("boom") }
	setDeviceVolume = func(context.Context, string, int) error { return nil }
	setShuffle = func(context.Context, bool) error { return nil }
	searchPlaylists = func(context.Context, string) ([]music.UserPlaylist, error) {
		return []music.UserPlaylist{{PersistentID: "P1", Name: "X"}}, nil
	}
	playPlaylistByID = func(context.Context, string) error { return nil }

	doc := &automationFile{
		Version: "1",
		Name:    "test",
		Defaults: automationDefaults{
			Backend: "airplay",
			Rooms:   []string{"Bedroom"},
		},
		Steps: []automationStep{
			{Type: "out.set", Rooms: []string{"Bedroom"}},
			{Type: "play", Query: "Chill"},
		},
	}
	results, ok := executeAutomationSteps(context.Background(), &native.Config{}, doc)
	if ok {
		t.Fatalf("ok=true, want false")
	}
	if len(results) != 2 {
		t.Fatalf("len(results)=%d, want 2", len(results))
	}
	if results[0].OK {
		t.Fatalf("first step should fail")
	}
	if !results[1].Skipped {
		t.Fatalf("second step should be skipped")
	}
}

func TestExecuteAutomationPlayNative(t *testing.T) {
	origRunShortcut := runNativeShortcut
	t.Cleanup(func() { runNativeShortcut = origRunShortcut })

	called := 0
	runNativeShortcut = func(context.Context, string) error {
		called++
		return nil
	}
	cfg := &native.Config{
		Native: native.NativeConfig{
			Playlists: map[string]map[string]string{
				"Bedroom": {"Focus": "BR Focus"},
			},
		},
	}
	err := executeAutomationPlay(context.Background(), cfg, "native", automationDefaults{Backend: "native", Rooms: []string{"Bedroom"}}, automationStep{
		Type:  "play",
		Query: "Focus",
	})
	if err != nil {
		t.Fatalf("executeAutomationPlay: %v", err)
	}
	if called != 1 {
		t.Fatalf("runNativeShortcut calls=%d, want 1", called)
	}
}
