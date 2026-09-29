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

func TestAirplayValidatesPlaylistBeforeSettings(t *testing.T) {
	for _, command := range []string{"play", "alias", "automation"} {
		for _, tc := range []struct {
			name, query, id, wantErr        string
			searchErr, lookupErr, volumeErr error
			noMatches                       bool
			wantCalls                       []string
		}{
			{name: "query", query: "Focus Mix", wantCalls: []string{"search:Focus Mix"}},
			{name: "ID", id: " A ", wantCalls: []string{"lookup:A"}},
			{name: "search failure", query: "Focus Mix", searchErr: errors.New("search failed"), wantErr: "search failed", wantCalls: []string{"search:Focus Mix"}},
			{name: "missing query", query: "Missing", noMatches: true, wantErr: "Missing", wantCalls: []string{"search:Missing"}},
			{name: "volume failure retains selected outputs", id: "A", volumeErr: errors.New("volume failed"), wantErr: "volume failed", wantCalls: []string{"lookup:A", "outputs:Bedroom", "volume:Bedroom:30"}},
			{name: "stale ID", id: " A ", lookupErr: errors.New("playlist missing"), wantErr: "playlist missing", wantCalls: []string{"lookup:A"}},
		} {
			t.Run(command+"/"+tc.name, func(t *testing.T) {
				r := recordPlayBackend(t)
				r.searchErr, r.lookupErr, r.volumeErr = tc.searchErr, tc.lookupErr, tc.volumeErr
				if tc.noMatches {
					r.matches = nil
				}
				cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "airplay", Rooms: []string{"Bedroom"}, Volume: intPtr(30), Shuffle: true}}
				var err error
				out, recovered := captureStdoutAndRecover(t, func() {
					switch command {
					case "play":
						args := []string{tc.query, "--json"}
						if tc.id != "" {
							args = []string{"--playlist-id", tc.id, "--json"}
						}
						cmdPlay(context.Background(), cfg, args)
					case "alias":
						cfg.Aliases = map[string]native.Alias{"test": {Playlist: tc.query, PlaylistID: tc.id, Shuffle: boolPtr(true)}}
						cmdRun(context.Background(), cfg, []string{"test", "--json"})
					case "automation":
						err = (automationPlay{Backend: "airplay", Rooms: []string{"Bedroom"}, Query: tc.query, PlaylistID: tc.id, Volume: intPtr(30), Shuffle: boolPtr(true)}).execute(context.Background(), cfg)
					}
				})
				if recovered != nil {
					fatal, ok := recovered.(cliFatal)
					if !ok {
						t.Fatalf("unexpected panic: %v", recovered)
					}
					err = fatal.err
				}
				want := slices.Clone(tc.wantCalls)
				if tc.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
						t.Fatalf("error=%v, want %q", err, tc.wantErr)
					}
					if out != "" {
						t.Fatalf("failure emitted success output: %s", out)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					want = append(want, "outputs:Bedroom", "volume:Bedroom:30", "shuffle:true", "play:A")
					if command != "automation" {
						want = append(want, "now")
					}
				}
				if !slices.Equal(r.calls, want) {
					t.Fatalf("calls=%v, want=%v", r.calls, want)
				}
			})
		}
	}
}

func TestAirplayAliasTargetModes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alias native.Alias
		dry   bool
		want  []string
	}{
		{name: "settings only", alias: native.Alias{Volume: intPtr(0), Shuffle: boolPtr(false)}, want: []string{"outputs:Bedroom", "volume:Bedroom:0", "shuffle:false", "now"}},
		{name: "ID takes precedence", alias: native.Alias{Playlist: "Missing", PlaylistID: "A"}, want: []string{"lookup:A", "outputs:Bedroom", "play:A", "now"}},
		{name: "query preview", alias: native.Alias{Playlist: "Missing"}, dry: true},
		{name: "ID preview", alias: native.Alias{PlaylistID: "Missing"}, dry: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := recordPlayBackend(t)
			cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "airplay", Rooms: []string{"Bedroom"}}, Aliases: map[string]native.Alias{"test": tc.alias}}
			captureStdout(t, func() {
				cmdRun(context.Background(), cfg, []string{"test", "--json", fmt.Sprintf("--dry-run=%t", tc.dry)})
			})
			if !slices.Equal(r.calls, tc.want) {
				t.Fatalf("calls=%v, want=%v", r.calls, tc.want)
			}
		})
	}
}

func TestAutomationPlaylistFailureRetainsEarlierStepsAndSkipsLaterSteps(t *testing.T) {
	r := recordPlayBackend(t)
	r.lookupErr = errors.New("playlist missing")
	plan := mustCompileAutomation(t, &native.Config{}, &automationFile{
		Version: "1", Name: "failure",
		Defaults: automationDefaults{Volume: intPtr(30), Shuffle: boolPtr(true)},
		Steps: []automationStep{
			{Type: "out.set", Rooms: []string{"Kitchen"}},
			{Type: "play", PlaylistID: "A", Rooms: []string{"Bedroom"}},
			{Type: "volume.set", Rooms: []string{"Kitchen"}, Value: intPtr(50)},
		},
	})
	result := executeAutomationPlan(context.Background(), plan)
	if result.OK || !result.Steps[0].OK || result.Steps[1].OK || result.Steps[1].Error != "playlist missing" || !result.Steps[2].Skipped {
		t.Fatalf("unexpected result: %+v", result)
	}
	want := []string{"outputs:Kitchen", "lookup:A"}
	if !slices.Equal(r.calls, want) {
		t.Fatalf("calls=%v, want=%v", r.calls, want)
	}
}
