package main

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func TestChooseSetupRoomsAndPlaylistWithoutMutatingInputs(t *testing.T) {
	cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "airplay", Rooms: []string{"Offline room"}}}
	devices := []music.AirPlayDevice{
		{Name: "Z Lounge", Available: true},
		{Name: "Bedroom", Available: true, PersistentID: "A"},
		{Name: "Kitchen", Available: true, Selected: true},
		{Name: "Bedroom", Available: false, PersistentID: "B"},
		{Name: "Dormant", Available: false},
		{Name: "", Available: true},
	}
	playlists := []music.UserPlaylist{
		{Name: "Z Mix", PersistentID: "Z"},
		{Name: "Alpha Mix", PersistentID: "A"},
	}
	beforeDevices, beforePlaylists := slices.Clone(devices), slices.Clone(playlists)
	var out bytes.Buffer
	got, err := chooseSetup(strings.NewReader("2, 1 2\nmix\n2\n"), &out, cfg, devices, playlists, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Rooms, []string{"Z Lounge", "Kitchen"}) || !got.RoomsChanged {
		t.Fatalf("selection=%#v", got)
	}
	if got.Playlist == nil || *got.Playlist != playlists[0] {
		t.Errorf("playlist=%#v, want %#v", got.Playlist, playlists[0])
	}
	if !slices.Equal(cfg.Defaults.Rooms, []string{"Offline room"}) || !reflect.DeepEqual(devices, beforeDevices) || !reflect.DeepEqual(playlists, beforePlaylists) {
		t.Fatal("chooser mutated configuration or discovery inputs")
	}
	for _, want := range []string{
		`1) "Kitchen" (available)`,
		`2) "Z Lounge" (available)`,
		`"Bedroom" (available, duplicate name: cannot select by name)`,
		`"Bedroom" (unavailable, duplicate name: cannot select by name)`,
		`"Dormant" (unavailable)`,
		`"" (available, missing name: cannot select)`,
		`"Offline room" (saved default, not discovered)`,
		`1) "Alpha Mix" (ID "A")`,
		`2) "Z Mix" (ID "Z")`,
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestChooseSetupKeepsDefaultsAndSkipsPlaylist(t *testing.T) {
	cfg := &native.Config{Defaults: native.DefaultsConfig{Rooms: []string{"Bedroom", "Offline"}}}
	devices := []music.AirPlayDevice{{Name: "Bedroom", Available: true}}
	var out bytes.Buffer
	got, err := chooseSetup(strings.NewReader("\n\n"), &out, cfg, devices, []music.UserPlaylist{{Name: "Focus", PersistentID: "A"}}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Rooms, cfg.Defaults.Rooms) || got.RoomsChanged || got.Playlist != nil {
		t.Fatalf("selection=%#v", got)
	}
	got.Rooms[0] = "Kitchen"
	if cfg.Defaults.Rooms[0] != "Bedroom" {
		t.Fatal("returned rooms share configuration storage")
	}
	if !strings.Contains(out.String(), `"Bedroom" (available, saved default)`) {
		t.Fatalf("output did not identify saved defaults:\n%s", out.String())
	}
}

func TestChooseSetupExplicitRoomsSkipRoomPromptAndPlaylistRecovers(t *testing.T) {
	cfg := &native.Config{Defaults: native.DefaultsConfig{Rooms: []string{"Explicit"}}}
	playlists := []music.UserPlaylist{{Name: "Focus B", PersistentID: "B"}, {Name: "Focus A", PersistentID: "A"}}
	var out bytes.Buffer
	got, err := chooseSetup(strings.NewReader("missing\nfocus\n\nfocus\n+1\n1x\n1 2\n0\n3\n2\n"), &out, cfg, nil, playlists, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Rooms, cfg.Defaults.Rooms) || got.RoomsChanged || got.Playlist == nil || got.Playlist.PersistentID != "B" {
		t.Fatalf("selection=%#v", got)
	}
	if strings.Contains(out.String(), "Default rooms:") || strings.Contains(out.String(), "Discovered destinations") {
		t.Fatalf("explicit rooms caused room selection:\n%s", out.String())
	}
	if !strings.Contains(out.String(), `No library playlists match "missing"`) || strings.Count(out.String(), "Invalid playlist selection.") != 5 {
		t.Fatalf("search/selection recovery missing:\n%s", out.String())
	}
}

func TestChooseSetupSkipsPlaylistAfterSearching(t *testing.T) {
	got, err := chooseSetup(strings.NewReader("focus\ns\n"), &bytes.Buffer{}, &native.Config{}, nil,
		[]music.UserPlaylist{{Name: "Focus", PersistentID: "A"}}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Playlist != nil {
		t.Fatalf("skipped playlist=%#v", got.Playlist)
	}
}

func TestChooseSetupRoomSelectionRejectsMalformedNumbers(t *testing.T) {
	cfg := &native.Config{Defaults: native.DefaultsConfig{Rooms: []string{"Bedroom"}}}
	devices := []music.AirPlayDevice{{Name: "Bedroom", Available: true}}
	var out bytes.Buffer
	got, err := chooseSetup(strings.NewReader("0\n2\n-1\n1.0\n1x\n+1\n,\n9999999999999999999999999999\n1, 1\n"), &out, cfg, devices, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Rooms, cfg.Defaults.Rooms) || got.RoomsChanged {
		t.Fatalf("selection=%#v", got)
	}
	if strings.Count(out.String(), "Invalid room selection.") != 8 {
		t.Fatalf("invalid room input was not retried:\n%s", out.String())
	}
}

func TestChooseSetupCancellationDiscardsPartialSelection(t *testing.T) {
	devices := []music.AirPlayDevice{{Name: "Bedroom", Available: true}}
	playlists := []music.UserPlaylist{{Name: "Focus", PersistentID: "A"}}
	for _, input := range []string{"q\n", "", "1\nq\n", "1\n", "1\nfocus\nq\n", "1\nfocus\n", "1\nfocus\n1"} {
		t.Run(fmt.Sprintf("input_%q", input), func(t *testing.T) {
			cfg := &native.Config{Defaults: native.DefaultsConfig{Rooms: []string{"Existing"}}}
			var out bytes.Buffer
			got, err := chooseSetup(strings.NewReader(input), &out, cfg, devices, playlists, false, false)
			if err == nil || !strings.Contains(err.Error(), "setup cancelled") {
				t.Fatalf("got=%#v err=%v", got, err)
			}
			if got.Rooms != nil || got.RoomsChanged || got.Playlist != nil {
				t.Errorf("cancelled chooser returned partial selections: %#v", got)
			}
			if !slices.Equal(cfg.Defaults.Rooms, []string{"Existing"}) {
				t.Fatal("cancelled chooser changed configuration")
			}
		})
	}
}

func TestChooseSetupEmptyDiscoveryPreservesDefaultsWithoutPrompts(t *testing.T) {
	cfg := &native.Config{Defaults: native.DefaultsConfig{Rooms: []string{"Saved"}}}
	var out bytes.Buffer
	got, err := chooseSetup(strings.NewReader(""), &out, cfg,
		[]music.AirPlayDevice{{Name: "Offline", Available: false}},
		[]music.UserPlaylist{{Name: "Missing ID"}}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Rooms, cfg.Defaults.Rooms) || got.RoomsChanged || got.Playlist != nil {
		t.Fatalf("selection=%#v", got)
	}
	if strings.Contains(out.String(), "Default rooms:") || strings.Contains(out.String(), "Playlist search:") {
		t.Fatalf("empty results caused a prompt:\n%s", out.String())
	}
}

func TestChooseSetupLimitsPlaylistMatchesAndQuotesDisplay(t *testing.T) {
	var playlists []music.UserPlaylist
	for i := 22; i > 0; i-- {
		playlists = append(playlists, music.UserPlaylist{Name: fmt.Sprintf("Mix %02d", i), PersistentID: fmt.Sprintf("ID%02d", i)})
	}
	var out bytes.Buffer
	got, err := chooseSetup(strings.NewReader("mix\n21\n20\n"), &out, &native.Config{}, nil, playlists, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Playlist == nil || got.Playlist.PersistentID != "ID20" {
		t.Fatalf("selection=%#v", got)
	}
	if !strings.Contains(out.String(), "Showing the first 20 of 22 matches") || strings.Contains(out.String(), `"Mix 21"`) {
		t.Fatalf("match limit not displayed/applied:\n%s", out.String())
	}

	out.Reset()
	_, err = chooseSetup(strings.NewReader("\n\n"), &out, &native.Config{},
		[]music.AirPlayDevice{{Name: "Room\n\x1b[31m", Available: true}},
		[]music.UserPlaylist{{Name: "Focus\n\x1b[31m", PersistentID: "ID\n"}}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"Room\n\x1b[31m"`) || strings.ContainsRune(out.String(), '\x1b') {
		t.Fatalf("unsafe room display:\n%q", out.String())
	}
}

type setupErrorReader struct{ err error }

func (r setupErrorReader) Read([]byte) (int, error) { return 0, r.err }

func TestChooseSetupReturnsInputError(t *testing.T) {
	want := errors.New("terminal disconnected")
	_, err := chooseSetup(setupErrorReader{want}, &bytes.Buffer{}, &native.Config{},
		[]music.AirPlayDevice{{Name: "Bedroom", Available: true}}, nil, false, false)
	if !errors.Is(err, want) {
		t.Fatalf("err=%v, want wrapped %v", err, want)
	}
}
