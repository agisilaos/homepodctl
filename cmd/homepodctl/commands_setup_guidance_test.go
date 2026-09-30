package main

import (
	"bytes"
	"context"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func TestSetupPlaylistSuggestionsAreStableAndBounded(t *testing.T) {
	playlists := []music.UserPlaylist{
		{Name: "Zulu", PersistentID: "z"},
		{Name: "focus", PersistentID: "f"},
		{Name: "Alpha", PersistentID: "b"},
		{Name: "Alpha", PersistentID: "a"},
		{Name: "alpha", PersistentID: "c"},
		{Name: "No ID"},
		{Name: " \t", PersistentID: "blank"},
		{Name: "Blank ID", PersistentID: " "},
	}
	original := slices.Clone(playlists)
	got := setupPlaylistSuggestions(playlists)
	want := []music.UserPlaylist{{Name: "Alpha", PersistentID: "a"}, {Name: "Alpha", PersistentID: "b"}, {Name: "alpha", PersistentID: "c"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("suggestions=%+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(playlists, original) {
		t.Fatalf("changed discovery results: %+v", playlists)
	}
	got[0].Name = "changed"
	if !reflect.DeepEqual(playlists, original) {
		t.Fatal("suggestions share storage with discovery results")
	}
}

func TestSetupGuidanceUsesDiscoveredRoomAndPlaylist(t *testing.T) {
	cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "airplay"}}
	next, warnings := setupGuidance(cfg, []music.AirPlayDevice{
		{Name: "Zulu", Available: true, Selected: true},
		{Name: "Kitchen", Available: true},
		{Name: "Bedroom", Available: false},
	}, []music.UserPlaylist{{Name: "Evening", PersistentID: "EVENING-ID"}}, true, true)
	if len(warnings) != 0 {
		t.Fatalf("warnings=%v", warnings)
	}
	if !slices.Contains(next, "homepodctl setup --room='Kitchen'") ||
		!slices.Contains(next, "homepodctl play --backend airplay --playlist-id='EVENING-ID' --room='Kitchen'") {
		t.Fatalf("next=%v", next)
	}
	if len(cfg.Defaults.Rooms) != 0 {
		t.Fatalf("guidance saved defaults: %+v", cfg.Defaults)
	}
}

func TestSetupGuidancePrefersHomePodWhenAvailable(t *testing.T) {
	cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "airplay"}}
	next, _ := setupGuidance(cfg, []music.AirPlayDevice{
		{Name: "AAA Mac", Kind: "computer", Available: true},
		{Name: "BBB Headphones", Kind: "Bluetooth device", Available: true},
		{Name: "Kitchen", Kind: "AirPlay device", Available: true},
		{Name: "Zulu HomePod", Kind: "HomePod", Available: true},
	}, []music.UserPlaylist{{Name: "Focus", PersistentID: "ID"}}, true, true)
	if !slices.Contains(next, "homepodctl setup --room='Zulu HomePod'") {
		t.Fatalf("HomePod example preferred another output: %v", next)
	}
	if len(cfg.Defaults.Rooms) != 0 {
		t.Fatal("suggestion persisted a preference")
	}
	next, _ = setupGuidance(cfg, []music.AirPlayDevice{
		{Name: "AAA Mac", Kind: "computer", Available: true},
		{Name: "Kitchen", Kind: "AirPlay device", Available: true},
	}, []music.UserPlaylist{{Name: "Focus", PersistentID: "ID"}}, true, true)
	if !slices.Contains(next, "homepodctl setup --room='Kitchen'") {
		t.Fatalf("other AirPlay destination should precede computer: %v", next)
	}
	next, _ = setupGuidance(cfg, []music.AirPlayDevice{{Name: "AAA Mac", Kind: "computer", Available: true}},
		[]music.UserPlaylist{{Name: "Focus", PersistentID: "ID"}}, true, true)
	if !slices.Contains(next, "homepodctl setup --room='AAA Mac'") {
		t.Fatalf("computer-only setup lost usable guidance: %v", next)
	}
}

func TestSetupGuidancePreservesSavedDefaults(t *testing.T) {
	cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "airplay", Rooms: []string{"Kitchen", "Office"}}}
	next, warnings := setupGuidance(cfg, []music.AirPlayDevice{
		{Name: "Kitchen", Available: true},
		{Name: "Office", Available: true},
		{Name: "Living Room", Available: true, Selected: true},
	}, []music.UserPlaylist{{Name: "Evening", PersistentID: "EVENING-ID"}}, true, true)
	if len(warnings) != 0 || !slices.Contains(next, "homepodctl play --backend airplay --playlist-id='EVENING-ID'") {
		t.Fatalf("next=%v warnings=%v", next, warnings)
	}
	for _, step := range next {
		if strings.Contains(step, "--room") {
			t.Fatalf("guidance overrides saved rooms: %s", step)
		}
	}
	if !slices.Equal(cfg.Defaults.Rooms, []string{"Kitchen", "Office"}) {
		t.Fatalf("changed preferences: %v", cfg.Defaults.Rooms)
	}
}

func TestSetupGuidanceSavedPlaylistUsesBarePlay(t *testing.T) {
	// The saved playlist is outside the bounded suggestions. Readiness must use
	// the complete discovery result rather than only the first three playlists.
	playlists := []music.UserPlaylist{
		{Name: "Alpha", PersistentID: "A"},
		{Name: "Beta", PersistentID: "B"},
		{Name: "Gamma", PersistentID: "G"},
		{Name: "Zulu", PersistentID: "Z"},
	}
	cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "airplay", Rooms: []string{"Kitchen"}, PlaylistID: "Z"}}
	next, warnings := setupGuidance(cfg, []music.AirPlayDevice{{Name: "Kitchen", Available: true}}, playlists, true, true)
	if len(warnings) != 0 || !slices.Equal(next, []string{"homepodctl play", "homepodctl status"}) {
		t.Fatalf("next=%v warnings=%v", next, warnings)
	}
	if cfg.Defaults.PlaylistID != "Z" || !slices.Equal(cfg.Defaults.Rooms, []string{"Kitchen"}) {
		t.Fatalf("changed saved preferences: %+v", cfg.Defaults)
	}
}

func TestSetupGuidanceSavedPlaylistRequiresVerification(t *testing.T) {
	for _, tc := range []struct {
		name         string
		playlists    []music.UserPlaylist
		discoveryOK  bool
		warning      string
		wrongWarning string
	}{
		{name: "missing", playlists: []music.UserPlaylist{{Name: "Another", PersistentID: "OTHER"}}, discoveryOK: true, warning: "was not found", wrongWarning: "discovery failed"},
		{name: "empty library", discoveryOK: true, warning: "was not found", wrongWarning: "discovery failed"},
		{name: "catalog failure", discoveryOK: false, warning: "Could not verify", wrongWarning: "was not found"},
		{name: "partial catalog failure", playlists: []music.UserPlaylist{{Name: "Saved", PersistentID: "SAVED"}}, discoveryOK: false, warning: "Could not verify", wrongWarning: "was not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "airplay", Rooms: []string{"Kitchen"}, PlaylistID: "SAVED"}}
			next, warnings := setupGuidance(cfg, []music.AirPlayDevice{{Name: "Kitchen", Available: true}}, tc.playlists, true, tc.discoveryOK)
			joined := strings.Join(warnings, "\n")
			if !strings.Contains(joined, tc.warning) || strings.Contains(joined, tc.wrongWarning) {
				t.Fatalf("warnings=%v", warnings)
			}
			if !slices.Contains(next, "homepodctl playlists") || !slices.Contains(next, "homepodctl setup --choose") {
				t.Fatalf("missing recovery commands: %v", next)
			}
			for _, command := range next {
				if strings.HasPrefix(command, "homepodctl play ") || command == "homepodctl play" {
					t.Fatalf("unverified default recommended for playback: %s", command)
				}
			}
			if cfg.Defaults.PlaylistID != "SAVED" {
				t.Fatal("guidance cleared an unavailable playlist preference")
			}
		})
	}
}

func TestSetupGuidanceSavedPlaylistWithoutDefaultRoomsUsesExplicitDestination(t *testing.T) {
	room := "--json Kitchen's $(printf INJECTED >&2)\n; room"
	cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "airplay", PlaylistID: "SAVED"}}
	next, warnings := setupGuidance(cfg, []music.AirPlayDevice{{Name: room, Available: true}}, []music.UserPlaylist{{Name: "Saved", PersistentID: "SAVED"}}, true, true)
	if len(warnings) != 0 || len(next) < 2 || next[1] != "homepodctl status" {
		t.Fatalf("next=%v warnings=%v", next, warnings)
	}
	command := next[0]
	if !strings.HasPrefix(command, "homepodctl play --room=") || strings.Contains(command, "--playlist-id") {
		t.Fatalf("default playback command=%s", command)
	}
	cmd := exec.Command("sh", "-c", "homepodctl() { printf '%s\\0' \"$@\"; }; "+command)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil || stderr.Len() != 0 {
		t.Fatalf("command=%s err=%v stderr=%s", command, err, stderr.String())
	}
	arguments := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if !slices.Equal(arguments, []string{"play", "--room=" + room}) {
		t.Fatalf("shell arguments=%q", arguments)
	}
	if len(cfg.Defaults.Rooms) != 0 || cfg.Defaults.PlaylistID != "SAVED" {
		t.Fatalf("guidance mutated preferences: %+v", cfg.Defaults)
	}
}

func TestSetupGuidanceSavedPlaylistRequiresUsableDestination(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rooms []string
	}{
		{name: "saved unavailable room", rooms: []string{"Kitchen"}},
		{name: "no available destinations"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "airplay", Rooms: tc.rooms, PlaylistID: "SAVED"}}
			next, _ := setupGuidance(cfg, nil, []music.UserPlaylist{{Name: "Saved", PersistentID: "SAVED"}}, true, true)
			for _, command := range next {
				if strings.HasPrefix(command, "homepodctl play ") || command == "homepodctl play" {
					t.Fatalf("unavailable destination recommended for playback: %s", command)
				}
			}
		})
	}
}

func TestSetupGuidanceFailedPlaylistDiscoveryDoesNotUsePartialSuggestions(t *testing.T) {
	next, _ := setupGuidance(&native.Config{}, []music.AirPlayDevice{{Name: "Kitchen", Available: true}}, []music.UserPlaylist{{Name: "Partial", PersistentID: "PARTIAL"}}, true, false)
	if !slices.Contains(next, "homepodctl playlists") {
		t.Fatalf("missing discovery recovery: %v", next)
	}
	for _, command := range next {
		if strings.HasPrefix(command, "homepodctl play ") {
			t.Fatalf("partial discovery recommended for playback: %s", command)
		}
	}
}

func TestSetupGuidanceRequiresAvailableUnambiguousRooms(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rooms   []string
		devices []music.AirPlayDevice
		ok      bool
		warning string
	}{
		{name: "missing", rooms: []string{"Kitchen"}, ok: true, warning: "was not discovered"},
		{name: "offline", rooms: []string{"Kitchen"}, devices: []music.AirPlayDevice{{Name: "Kitchen"}}, ok: true, warning: "is unavailable"},
		{name: "duplicate", rooms: []string{"Kitchen"}, devices: []music.AirPlayDevice{{Name: "Kitchen", Available: true}, {Name: "Kitchen"}}, ok: true, warning: "matches multiple devices"},
		{name: "one of multiple invalid", rooms: []string{"Kitchen", "Office"}, devices: []music.AirPlayDevice{{Name: "Kitchen", Available: true}}, ok: true, warning: "Office"},
		{name: "empty discovery", ok: true, warning: "No available destination"},
		{name: "discovery failed", rooms: []string{"Kitchen"}, devices: []music.AirPlayDevice{{Name: "Kitchen", Available: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "airplay", Rooms: slices.Clone(tc.rooms)}}
			next, warnings := setupGuidance(cfg, tc.devices, []music.UserPlaylist{{Name: "Evening", PersistentID: "ID"}}, tc.ok, true)
			for _, step := range next {
				if strings.HasPrefix(step, "homepodctl play ") {
					t.Fatalf("unsafe playback guidance: %s", step)
				}
			}
			if tc.warning != "" && !strings.Contains(strings.Join(warnings, "\n"), tc.warning) {
				t.Fatalf("warnings=%v, want %q", warnings, tc.warning)
			}
			if !slices.Contains(next, "homepodctl devices") {
				t.Fatalf("missing recovery: %v", next)
			}
			if !slices.Equal(cfg.Defaults.Rooms, tc.rooms) {
				t.Fatalf("changed preferences: %v", cfg.Defaults.Rooms)
			}
		})
	}
}

func TestSetupGuidanceSkipsDuplicateAndUnavailableCandidates(t *testing.T) {
	next, _ := setupGuidance(&native.Config{}, []music.AirPlayDevice{
		{Name: "Alpha", Available: true}, {Name: "Alpha", Available: true},
		{Name: "Beta", Available: false}, {Name: "Delta", Available: true},
	}, []music.UserPlaylist{{Name: "Evening", PersistentID: "ID"}}, true, true)
	if !slices.Contains(next, "homepodctl setup --room='Delta'") {
		t.Fatalf("next=%v", next)
	}
}

func TestSetupGuidanceEmptyLibraryOffersDiscovery(t *testing.T) {
	next, _ := setupGuidance(&native.Config{}, []music.AirPlayDevice{{Name: "Kitchen", Available: true}}, nil, true, true)
	if !slices.Contains(next, "homepodctl playlists") {
		t.Fatalf("next=%v", next)
	}
	for _, step := range next {
		if strings.HasPrefix(step, "homepodctl play ") {
			t.Fatalf("invented playlist: %s", step)
		}
	}
}

func TestSetupGuidanceNativeExplainsMapping(t *testing.T) {
	cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "native", Rooms: []string{"Kitchen"}}}
	next, warnings := setupGuidance(cfg, []music.AirPlayDevice{{Name: "Kitchen", Available: true}}, []music.UserPlaylist{{Name: "Evening", PersistentID: "ID"}}, true, true)
	if !slices.Contains(next, "homepodctl help config") || !slices.Contains(next, "homepodctl config validate") ||
		!strings.Contains(strings.Join(warnings, "\n"), "Shortcut mapping") {
		t.Fatalf("next=%v warnings=%v", next, warnings)
	}
	for _, step := range next {
		if strings.HasPrefix(step, "homepodctl play ") || strings.Contains(step, "--choose") {
			t.Fatalf("native setup claims readiness without mappings: %s", step)
		}
	}
}

func TestSetupGuidanceNativeWarnsAboutUndiscoveredDefaults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		devices []music.AirPlayDevice
		warning string
	}{
		{name: "missing", warning: `Default room "Kitchen" was not discovered`},
		{name: "offline", devices: []music.AirPlayDevice{{Name: "Kitchen"}}, warning: `Default room "Kitchen" is unavailable`},
		{name: "duplicate", devices: []music.AirPlayDevice{{Name: "Kitchen", Available: true}, {Name: "Kitchen", Available: true}}, warning: `Default room "Kitchen" matches multiple devices`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "native", Rooms: []string{"Kitchen"}}}
			next, warnings := setupGuidance(cfg, tc.devices, nil, true, true)
			if !strings.Contains(strings.Join(warnings, "\n"), tc.warning) || !slices.Contains(next, "homepodctl devices") {
				t.Fatalf("next=%v warnings=%v, want warning %q and device recovery", next, warnings, tc.warning)
			}
			if strings.Contains(strings.Join(append(next, warnings...), "\n"), "--choose") {
				t.Fatalf("native recovery suggests unsupported chooser: next=%v warnings=%v", next, warnings)
			}
			if !slices.Equal(cfg.Defaults.Rooms, []string{"Kitchen"}) {
				t.Fatal("guidance changed native preferences")
			}
		})
	}
}

func TestSetupGuidanceCommandsRoundTripThroughShell(t *testing.T) {
	room := "--json Kitchen's $(printf INJECTED >&2) `printf INJECTED >&2`\n; space"
	id := "--room=Playlist's $(printf INJECTED >&2) `printf INJECTED >&2`\n; ID"
	cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "airplay"}}
	next, warnings := setupGuidance(cfg, []music.AirPlayDevice{{Name: room, Available: true}}, []music.UserPlaylist{{Name: "Evening", PersistentID: id}}, true, true)
	if len(warnings) != 0 {
		t.Fatalf("warnings=%v", warnings)
	}
	for _, command := range next {
		if !strings.HasPrefix(command, "homepodctl setup ") && !strings.HasPrefix(command, "homepodctl play ") {
			continue
		}
		cmd := exec.Command("sh", "-c", "homepodctl() { printf '%s\\0' \"$@\"; }; "+command)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil || stderr.Len() != 0 {
			t.Fatalf("command=%s err=%v stderr=%s", command, err, stderr.String())
		}
		arguments := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
		if arguments[0] == "setup" {
			opts, err := parseSetupOptions(arguments[1:])
			if err != nil || !slices.Equal(opts.rooms, []string{room}) {
				t.Fatalf("setup arguments=%q opts=%+v err=%v", arguments, opts, err)
			}
		} else {
			req, err := resolvePlayRequest(context.Background(), cfg, append(arguments[1:], "--dry-run"))
			if err != nil || req.target.value != id || !slices.Equal(req.rooms, []string{room}) || req.backend != playAirplay {
				t.Fatalf("play arguments=%q request=%+v err=%v", arguments, req, err)
			}
		}
	}
}
