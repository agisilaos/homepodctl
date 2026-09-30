package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/agisilaos/homepodctl/internal/native"
)

func TestCmdPlaySavedDefaultAndExplicitOverrides(t *testing.T) {
	for _, backend := range []string{"airplay", "native"} {
		for _, tc := range []struct {
			name, id, query string
			args            []string
		}{
			{name: "saved default", id: "DEFAULT"},
			{name: "query overrides", args: []string{"Focus", "Mix"}, query: "Focus Mix"},
			{name: "playlist flag overrides", args: []string{"--playlist", "Focus Mix"}, query: "Focus Mix"},
			{name: "ID overrides", args: []string{"--playlist-id", "OTHER"}, id: "OTHER"},
		} {
			for _, dryRun := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/dry=%t", backend, tc.name, dryRun), func(t *testing.T) {
					r := recordPlayBackend(t)
					cfg := &native.Config{
						Defaults: native.DefaultsConfig{Backend: backend, Rooms: []string{"Bedroom"}, PlaylistID: " DEFAULT ", Volume: intPtr(30), Shuffle: true},
						Native:   native.NativeConfig{Playlists: map[string]map[string]string{"Bedroom": {"Focus Mix": "Play Focus"}}},
					}
					out, err := runPlayJSON(t, cfg, tc.args, dryRun)
					if err != nil {
						t.Fatal(err)
					}
					wantID, wantQuery := tc.id, tc.query
					var wantCalls []string
					if !dryRun {
						if tc.id != "" {
							wantCalls = append(wantCalls, "lookup:"+tc.id)
						}
						if backend == "airplay" {
							if tc.query != "" {
								wantCalls = append(wantCalls, "search:"+tc.query)
								wantID = "A"
							}
							wantCalls = append(wantCalls, "outputs:Bedroom", "volume:Bedroom:30", "shuffle:true", "play:"+wantID, "now")
						} else {
							wantQuery = "Focus Mix"
							wantCalls = append(wantCalls, "shortcut:Play Focus")
						}
					}
					if out.PlaylistID != wantID || out.Playlist != wantQuery || !slices.Equal(out.Rooms, []string{"Bedroom"}) {
						t.Fatalf("output=%+v, want ID=%q query=%q rooms=[Bedroom]", out, wantID, wantQuery)
					}
					if !slices.Equal(r.calls, wantCalls) {
						t.Fatalf("calls=%v, want %v", r.calls, wantCalls)
					}
				})
			}
		}
	}
}

func TestCmdPlaySavedDefaultDoesNotHideInvalidTargets(t *testing.T) {
	for _, backend := range []string{"airplay", "native"} {
		for _, tc := range []struct {
			name, want string
			args       []string
		}{
			{"blank positional", "playlist is required", []string{" "}},
			{"blank playlist", "playlist is required", []string{"--playlist", " "}},
			{"blank ID", "playlist is required", []string{"--playlist-id", " "}},
			{"both flags", "exactly one playlist target", []string{"--playlist", "Focus", "--playlist-id", "A"}},
			{"positional and flag", "exactly one playlist target", []string{"Focus", "--playlist-id", "A"}},
			{"blank flag conflicts", "exactly one playlist target", []string{"Focus", "--playlist", ""}},
		} {
			for _, dryRun := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/dry=%t", backend, tc.name, dryRun), func(t *testing.T) {
					r := recordPlayBackend(t)
					cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: backend, Rooms: []string{"Bedroom"}, PlaylistID: "DEFAULT"}}
					_, err := runPlayJSON(t, cfg, tc.args, dryRun)
					if err == nil || !strings.Contains(err.Error(), tc.want) || classifyExitCode(err) != exitUsage {
						t.Fatalf("error=%v, want usage error containing %q", err, tc.want)
					}
					if len(r.calls) != 0 {
						t.Fatalf("invalid target called backend: %v", r.calls)
					}
				})
			}
		}
	}
}

func TestCmdPlayMissingSavedDefaultFailsBeforeSettings(t *testing.T) {
	for _, backend := range []string{"airplay", "native"} {
		t.Run(backend, func(t *testing.T) {
			r := recordPlayBackend(t)
			r.lookupErr = errors.New("playlist missing")
			cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: backend, Rooms: []string{"Bedroom"}, PlaylistID: "DELETED", Volume: intPtr(30), Shuffle: true}}
			_, err := runPlayJSON(t, cfg, nil, false)
			if !errors.Is(err, r.lookupErr) {
				t.Fatalf("error=%v, want original lookup failure", err)
			}
			if !slices.Equal(r.calls, []string{"lookup:DELETED"}) {
				t.Fatalf("calls=%v, want only default lookup", r.calls)
			}
		})
	}
}

func TestCmdPlayWithoutDefaultExplainsSetup(t *testing.T) {
	r := recordPlayBackend(t)
	cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "airplay", PlaylistID: " \t "}}
	_, err := runPlayJSON(t, cfg, nil, false)
	if err == nil || !strings.Contains(err.Error(), "homepodctl setup --choose") || classifyExitCode(err) != exitUsage {
		t.Fatalf("error=%v, want actionable usage error", err)
	}
	if len(r.calls) != 0 {
		t.Fatalf("missing target called backend: %v", r.calls)
	}
}

func TestSavedDefaultDoesNotSupplyAliasOrAutomationTargets(t *testing.T) {
	r := recordPlayBackend(t)
	cfg := &native.Config{
		Defaults: native.DefaultsConfig{Backend: "airplay", Rooms: []string{"Bedroom"}, PlaylistID: "DEFAULT"},
		Aliases:  map[string]native.Alias{"rooms-only": {Rooms: []string{"Kitchen"}}},
	}
	captureStdout(t, func() { cmdRun(context.Background(), cfg, []string{"rooms-only", "--json"}) })
	if !slices.Equal(r.calls, []string{"outputs:Kitchen", "now"}) {
		t.Fatalf("settings-only alias calls=%v, want output selection only", r.calls)
	}
	_, err := compileAutomationPlan(cfg, &automationFile{Version: "1", Name: "missing target", Steps: []automationStep{{Type: "play"}}})
	if err == nil || !strings.Contains(err.Error(), "play requires exactly one of query or playlistId") {
		t.Fatalf("automation without target error=%v, want required target", err)
	}
}

func TestConfigDefaultPlaylistIDSetGetAndClear(t *testing.T) {
	t.Parallel()
	cfg := &native.Config{}
	for _, value := range []string{" DEFAULT ", ""} {
		if err := setConfigPathValue(cfg, "defaults.playlistId", []string{value}); err != nil {
			t.Fatal(err)
		}
		got, err := getConfigPathValue(cfg, "defaults.playlistId")
		if err != nil || got != strings.TrimSpace(value) {
			t.Fatalf("get default playlist ID=%v, err=%v", got, err)
		}
	}
	for _, values := range [][]string{nil, {"A", "B"}} {
		if err := setConfigPathValue(cfg, "defaults.playlistId", values); err == nil {
			t.Fatalf("values=%v accepted, want exactly one value", values)
		}
	}
}

func TestCLIPlaySavedDefaultPreviewAndClear(t *testing.T) {
	cli := newCLIHarness(t)
	for _, args := range [][]string{
		{"config", "set", "defaults.rooms", "Bedroom"},
		{"config", "set", "defaults.playlistId", "DEFAULT"},
	} {
		if result := cli.run(t, args...); result.ExitCode != 0 {
			t.Fatalf("configure defaults: %+v", result)
		}
	}
	for _, args := range [][]string{{"play", "--dry-run", "--json", "--no-input"}, {"plan", "play", "--json", "--no-input"}} {
		result := cli.run(t, args...)
		if result.ExitCode != 0 || result.Stderr != "" || !strings.Contains(result.Stdout, `"playlistId": "DEFAULT"`) {
			t.Fatalf("default preview: %+v", result)
		}
	}
	if result := cli.run(t, "config", "set", "defaults.playlistId", ""); result.ExitCode != 0 {
		t.Fatalf("clear default: %+v", result)
	}
	if result := cli.run(t, "play", "--dry-run", "--no-input"); result.ExitCode != exitUsage || !strings.Contains(result.Stderr, "homepodctl setup --choose") {
		t.Fatalf("play after clear: %+v", result)
	}
}
