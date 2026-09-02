package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

type playBackendRecorder struct {
	calls        []string
	matches      []music.UserPlaylist
	nowPlaying   music.NowPlaying
	nowErr       error
	searchErr    error
	playlistName string
}

func recordPlayBackend(t *testing.T) *playBackendRecorder {
	t.Helper()
	oldSearch, oldLookup := searchPlaylists, findPlaylistNameByID
	oldOutputs, oldVolume := playbackApp.setRouteFn, playbackApp.setVolumeFn
	oldShuffle, oldPlay := setShuffle, playPlaylistByID
	oldNow, oldShortcut := playbackApp.nowPlayingFn, runNativeShortcut
	t.Cleanup(func() {
		searchPlaylists, findPlaylistNameByID = oldSearch, oldLookup
		playbackApp.setRouteFn, playbackApp.setVolumeFn = oldOutputs, oldVolume
		setShuffle, playPlaylistByID = oldShuffle, oldPlay
		playbackApp.nowPlayingFn, runNativeShortcut = oldNow, oldShortcut
	})
	r := &playBackendRecorder{
		matches:      []music.UserPlaylist{{Name: "Focus Mix", PersistentID: "A"}},
		playlistName: "Focus Mix",
	}
	searchPlaylists = func(_ context.Context, query string) ([]music.UserPlaylist, error) {
		r.calls = append(r.calls, "search:"+query)
		return r.matches, r.searchErr
	}
	findPlaylistNameByID = func(_ context.Context, id string) (string, error) {
		r.calls = append(r.calls, "lookup:"+id)
		return r.playlistName, nil
	}
	playbackApp.setRouteFn = func(_ context.Context, rooms []string) error {
		r.calls = append(r.calls, "outputs:"+strings.Join(rooms, ","))
		return nil
	}
	playbackApp.setVolumeFn = func(_ context.Context, room string, volume int) error {
		r.calls = append(r.calls, fmt.Sprintf("volume:%s:%d", room, volume))
		return nil
	}
	setShuffle = func(_ context.Context, shuffle bool) error {
		r.calls = append(r.calls, fmt.Sprintf("shuffle:%t", shuffle))
		return nil
	}
	playPlaylistByID = func(_ context.Context, id string) error {
		r.calls = append(r.calls, "play:"+id)
		return nil
	}
	playbackApp.nowPlayingFn = func(context.Context) (music.NowPlaying, error) {
		r.calls = append(r.calls, "now")
		return r.nowPlaying, r.nowErr
	}
	runNativeShortcut = func(_ context.Context, name string) error {
		r.calls = append(r.calls, "shortcut:"+name)
		return nil
	}
	return r
}

func runPlayJSON(t *testing.T, cfg *native.Config, args []string, dryRun bool) (actionResult, error) {
	t.Helper()
	args = append(slices.Clone(args), "--json", fmt.Sprintf("--dry-run=%t", dryRun))
	out, recovered := captureStdoutAndRecover(t, func() {
		cmdPlay(context.Background(), cfg, args)
	})
	if recovered != nil {
		fatal, ok := recovered.(cliFatal)
		if !ok {
			t.Fatalf("unexpected panic: %v", recovered)
		}
		if out != "" {
			t.Fatalf("failed command emitted success output: %s", out)
		}
		return actionResult{}, fatal.err
	}
	var result actionResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode play output %q: %v", out, err)
	}
	if !result.OK || result.Action != "play" || result.DryRun != dryRun {
		t.Fatalf("unexpected play result: %+v", result)
	}
	return result, nil
}

func TestCmdPlayTargets(t *testing.T) {
	targets := []struct {
		name string
		args []string
		id   bool
	}{
		{name: "joined positionals", args: []string{"Focus", "Mix"}},
		{name: "quoted query", args: []string{"Focus Mix"}},
		{name: "playlist flag", args: []string{"--playlist", " Focus Mix "}},
		{name: "persistent ID", args: []string{"--playlist-id", " A "}, id: true},
		{name: "last playlist flag wins", args: []string{"--playlist", "Other", "--playlist", "Focus Mix"}},
		{name: "last ID flag wins", args: []string{"--playlist-id", "B", "--playlist-id", "A"}, id: true},
	}
	for _, backend := range []string{"airplay", "native"} {
		for _, target := range targets {
			for _, dryRun := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/dry=%t", backend, target.name, dryRun), func(t *testing.T) {
					r := recordPlayBackend(t)
					cfg := &native.Config{
						Defaults: native.DefaultsConfig{Backend: backend, Rooms: []string{"Bedroom"}},
						Native: native.NativeConfig{Playlists: map[string]map[string]string{
							"Bedroom": {"Focus Mix": "Play Focus"},
						}},
					}
					out, err := runPlayJSON(t, cfg, target.args, dryRun)
					if err != nil {
						t.Fatal(err)
					}
					wantName, wantID := "Focus Mix", ""
					if target.id {
						wantID = "A"
						if backend == "airplay" || dryRun {
							wantName = ""
						}
					}
					var wantCalls []string
					if !dryRun {
						if backend == "airplay" {
							wantID = "A"
							if !target.id {
								wantCalls = append(wantCalls, "search:Focus Mix")
							}
							wantCalls = append(wantCalls, "outputs:Bedroom", "shuffle:false", "play:A", "now")
						} else {
							if target.id {
								wantCalls = append(wantCalls, "lookup:A")
							}
							wantCalls = append(wantCalls, "shortcut:Play Focus")
						}
					}
					if out.Backend != backend || !slices.Equal(out.Rooms, []string{"Bedroom"}) || out.Playlist != wantName || out.PlaylistID != wantID {
						t.Fatalf("output=%+v, want backend=%s rooms=[Bedroom] playlist=%q id=%q", out, backend, wantName, wantID)
					}
					if !slices.Equal(r.calls, wantCalls) {
						t.Fatalf("calls=%v, want=%v", r.calls, wantCalls)
					}
				})
			}
		}
	}
}

func TestCmdPlayValidationBeforeBackendCalls(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"both flags", []string{"--playlist", "Focus", "--playlist-id", "A"}, "exactly one playlist target"},
		{"positional and ID", []string{"Focus", "--playlist-id", "A"}, "exactly one playlist target"},
		{"positional and playlist", []string{"Focus", "--playlist", "Focus"}, "exactly one playlist target"},
		{"all three", []string{"Focus", "--playlist", "Focus", "--playlist-id", "A"}, "exactly one playlist target"},
		{"empty flag still conflicts", []string{"Focus", "--playlist", " "}, "exactly one playlist target"},
		{"missing target", nil, "playlist is required"},
		{"empty query", []string{" "}, "playlist is required"},
		{"empty playlist", []string{"--playlist", " "}, "playlist is required"},
		{"empty ID", []string{"--playlist-id", " "}, "playlist is required"},
		{"negative volume", []string{"Focus", "--volume", "-1"}, "volume must be 0-100"},
		{"high volume", []string{"Focus", "--volume", "101"}, "volume must be 0-100"},
		{"invalid volume", []string{"Focus", "--volume", "loud"}, "invalid --volume"},
		{"invalid shuffle", []string{"Focus", "--shuffle=maybe"}, "invalid --shuffle"},
		{"invalid choose", []string{"Focus", "--choose=maybe"}, "invalid --choose"},
		{"invalid no-input", []string{"Focus", "--no-input=maybe"}, "invalid --no-input"},
		{"unknown backend", []string{"Focus", "--backend", "other"}, "unknown backend"},
	}
	for _, backend := range []string{"airplay", "native"} {
		for _, tc := range tests {
			for _, dryRun := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/dry=%t", backend, tc.name, dryRun), func(t *testing.T) {
					r := recordPlayBackend(t)
					volume := 30
					cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: backend, Volume: &volume}}
					_, err := runPlayJSON(t, cfg, tc.args, dryRun)
					if err == nil || !strings.Contains(err.Error(), tc.want) || classifyExitCode(err) != exitUsage {
						t.Fatalf("error=%v, want usage error containing %q", err, tc.want)
					}
					if len(r.calls) != 0 {
						t.Fatalf("invalid request called backend: %v", r.calls)
					}
				})
			}
		}
	}
}

func TestResolvePlayRequestOptions(t *testing.T) {
	volume := 30
	cfg := &native.Config{Defaults: native.DefaultsConfig{
		Backend: "native", Rooms: []string{"Bedroom"}, Volume: &volume, Shuffle: true,
	}}
	for _, tc := range []struct {
		name string
		args []string
		want playRequest
	}{
		{
			name: "defaults",
			args: []string{"Focus", "Mix"},
			want: playRequest{
				backend: playNative, rooms: []string{"Bedroom"},
				target: playTarget{kind: playQueryTarget, value: "Focus Mix"},
				volume: playVolume{source: playVolumeDefault, value: 30}, shuffle: true,
			},
		},
		{
			name: "explicit overrides including zero and false",
			args: []string{"--backend", " airplay ", "--room", "Kitchen", "--room", "Office", "--playlist-id", "A", "--volume", "0", "--shuffle=false", "--choose", "--no-input", "--json", "--plain", "--dry-run"},
			want: playRequest{
				backend: playAirplay, rooms: []string{"Kitchen", "Office"},
				target: playTarget{kind: playIDTarget, value: "A"},
				volume: playVolume{source: playVolumeExplicit, value: 0},
				choose: true, noInput: true, output: outputOptions{JSON: true, Plain: true, DryRun: true},
			},
		},
		{
			name: "literal positional after separator",
			args: []string{"--", "--playlist-id"},
			want: playRequest{
				backend: playNative, rooms: []string{"Bedroom"},
				target: playTarget{kind: playQueryTarget, value: "--playlist-id"},
				volume: playVolume{source: playVolumeDefault, value: 30}, shuffle: true,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := recordPlayBackend(t)
			req, err := resolvePlayRequest(context.Background(), cfg, tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(req, tc.want) {
				t.Fatalf("request=%+v, want=%+v", req, tc.want)
			}
			if len(r.calls) != 0 {
				t.Fatalf("unexpected backend calls: %v", r.calls)
			}
			req.rooms[0] = "Changed"
			if cfg.Defaults.Rooms[0] != "Bedroom" {
				t.Fatal("request rooms alias config rooms")
			}
		})
	}
}

func TestCmdPlayVolume(t *testing.T) {
	defaultVolume, zeroVolume, invalidVolume := 30, 0, 101
	for _, tc := range []struct {
		name          string
		defaultVolume *int
		args          []string
		wantVolume    playVolume
		wantCall      string
		wantErr       bool
	}{
		{name: "absent", wantVolume: playVolume{source: playVolumeAbsent}},
		{name: "default", defaultVolume: &defaultVolume, wantVolume: playVolume{source: playVolumeDefault, value: 30}, wantCall: "volume:Bedroom:30"},
		{name: "default zero", defaultVolume: &zeroVolume, wantVolume: playVolume{source: playVolumeDefault, value: 0}, wantCall: "volume:Bedroom:0"},
		{name: "explicit zero overrides default", defaultVolume: &defaultVolume, args: []string{"--volume", "0"}, wantVolume: playVolume{source: playVolumeExplicit, value: 0}, wantCall: "volume:Bedroom:0"},
		{name: "explicit max", args: []string{"--volume", "100"}, wantVolume: playVolume{source: playVolumeExplicit, value: 100}, wantCall: "volume:Bedroom:100"},
		{name: "invalid configured volume", defaultVolume: &invalidVolume, wantErr: true},
		{name: "explicit overrides invalid default", defaultVolume: &invalidVolume, args: []string{"--volume", "30"}, wantVolume: playVolume{source: playVolumeExplicit, value: 30}, wantCall: "volume:Bedroom:30"},
	} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%t", tc.name, dryRun), func(t *testing.T) {
				r := recordPlayBackend(t)
				cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "airplay", Rooms: []string{"Bedroom"}, Volume: tc.defaultVolume}}
				args := append([]string{"--playlist-id", "A"}, tc.args...)
				req, err := resolvePlayRequest(context.Background(), cfg, args)
				if !tc.wantErr && (err != nil || req.volume != tc.wantVolume) {
					t.Fatalf("volume=%+v error=%v, want=%+v", req.volume, err, tc.wantVolume)
				}
				_, err = runPlayJSON(t, cfg, args, dryRun)
				if tc.wantErr {
					if err == nil || classifyExitCode(err) != exitUsage || len(r.calls) != 0 {
						t.Fatalf("error=%v calls=%v, want usage error without backend calls", err, r.calls)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				var wantCalls []string
				if !dryRun {
					wantCalls = append(wantCalls, "outputs:Bedroom")
					if tc.wantCall != "" {
						wantCalls = append(wantCalls, tc.wantCall)
					}
					wantCalls = append(wantCalls, "shuffle:false", "play:A", "now")
				}
				if !slices.Equal(r.calls, wantCalls) {
					t.Fatalf("calls=%v, want=%v", r.calls, wantCalls)
				}
			})
		}
	}
}

func TestCmdPlayRoomResolution(t *testing.T) {
	defaultVolume := 30
	for _, tc := range []struct {
		name          string
		backend       string
		defaultRooms  []string
		defaultVolume *int
		args          []string
		outputs       []music.AirPlayDevice
		inferenceErr  error
		wantRooms     []string
		wantInference bool
		wantErr       string
	}{
		{name: "explicit rooms override defaults", backend: "airplay", defaultRooms: []string{"Bedroom"}, args: []string{"--room", "Kitchen", "--room", "Office"}, wantRooms: []string{"Kitchen", "Office"}},
		{name: "inferred rooms with default volume", backend: "airplay", defaultVolume: &defaultVolume, outputs: []music.AirPlayDevice{{Name: " Kitchen "}, {Name: "Kitchen"}, {Name: ""}, {Name: "Office"}}, wantRooms: []string{"Kitchen", "Office"}, wantInference: true},
		{name: "no rooms or volume preserves outputs", backend: "airplay", wantInference: true},
		{name: "default volume without rooms is skipped", backend: "airplay", defaultVolume: &defaultVolume, wantInference: true},
		{name: "explicit zero without rooms fails", backend: "airplay", defaultVolume: &defaultVolume, args: []string{"--volume", "0"}, wantInference: true, wantErr: "cannot set volume without rooms"},
		{name: "unavailable status preserves outputs", backend: "airplay", inferenceErr: errors.New("unavailable"), wantInference: true},
		{name: "native requires rooms", backend: "native", wantErr: "no rooms provided"},
		{name: "native room override", backend: "native", defaultRooms: []string{"Bedroom"}, args: []string{"--room", "Kitchen", "--room", "Office"}, wantRooms: []string{"Kitchen", "Office"}},
	} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%t", tc.name, dryRun), func(t *testing.T) {
				r := recordPlayBackend(t)
				r.nowPlaying.Outputs, r.nowErr = tc.outputs, tc.inferenceErr
				cfg := &native.Config{
					Defaults: native.DefaultsConfig{Backend: tc.backend, Rooms: tc.defaultRooms, Volume: tc.defaultVolume},
					Native: native.NativeConfig{Playlists: map[string]map[string]string{
						"Kitchen": {"Focus Mix": "Kitchen Focus"},
						"Office":  {"Focus Mix": "Office Focus"},
					}},
				}
				args := append([]string{"--playlist-id", "A"}, tc.args...)
				out, err := runPlayJSON(t, cfg, args, dryRun)
				var wantCalls []string
				if tc.wantInference {
					wantCalls = append(wantCalls, "now")
				}
				if tc.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), tc.wantErr) || classifyExitCode(err) != exitUsage {
						t.Fatalf("error=%v, want usage error containing %q", err, tc.wantErr)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					if !slices.Equal(out.Rooms, tc.wantRooms) {
						t.Fatalf("rooms=%v, want=%v", out.Rooms, tc.wantRooms)
					}
					if !dryRun {
						if tc.backend == "native" {
							wantCalls = append(wantCalls, "lookup:A", "shortcut:Kitchen Focus", "shortcut:Office Focus")
						} else {
							if len(tc.wantRooms) > 0 {
								wantCalls = append(wantCalls, "outputs:"+strings.Join(tc.wantRooms, ","))
								if tc.defaultVolume != nil {
									for _, room := range tc.wantRooms {
										wantCalls = append(wantCalls, fmt.Sprintf("volume:%s:%d", room, *tc.defaultVolume))
									}
								}
							}
							wantCalls = append(wantCalls, "shuffle:false", "play:A", "now")
						}
					}
				}
				if !slices.Equal(r.calls, wantCalls) {
					t.Fatalf("calls=%v, want=%v", r.calls, wantCalls)
				}
			})
		}
	}
}

func TestCmdPlaySelectionOptions(t *testing.T) {
	// A regular file represents redirected stdin without risking an actual prompt.
	stdin, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = stdin
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = stdin.Close()
	})
	for _, tc := range []struct {
		name      string
		backend   string
		args      []string
		multiple  bool
		searchErr error
		noMatches bool
		wantErr   string
		wantCalls []string
	}{
		{name: "auto pick best", backend: "airplay", multiple: true, wantCalls: []string{"search:Focus Mix", "outputs:Bedroom", "shuffle:true", "play:A", "now"}},
		{name: "explicit false overrides defaults", backend: "airplay", multiple: true, args: []string{"--choose=false", "--shuffle=false"}, wantCalls: []string{"search:Focus Mix", "outputs:Bedroom", "shuffle:false", "play:A", "now"}},
		{name: "choose single match without input", backend: "airplay", args: []string{"--choose", "--no-input"}, wantCalls: []string{"search:Focus Mix", "outputs:Bedroom", "shuffle:true", "play:A", "now"}},
		{name: "choose multiple without input", backend: "airplay", multiple: true, args: []string{"--choose", "--no-input"}, wantErr: "non-interactive mode cannot prompt", wantCalls: []string{"search:Focus Mix"}},
		{name: "choose multiple redirected stdin", backend: "airplay", multiple: true, args: []string{"--choose", "--no-input=false"}, wantErr: "requires interactive stdin", wantCalls: []string{"search:Focus Mix"}},
		{name: "search failure", backend: "airplay", searchErr: errors.New("search failed"), wantErr: "search failed", wantCalls: []string{"search:Focus Mix"}},
		{name: "no matches", backend: "airplay", noMatches: true, wantErr: "no playlists match", wantCalls: []string{"search:Focus Mix"}},
		{name: "native ignores AirPlay options", backend: "native", multiple: true, args: []string{"--choose", "--no-input", "--shuffle", "--volume", "70"}, wantCalls: []string{"shortcut:Play Focus"}},
		{name: "native ignores AirPlay defaults", backend: "native", wantCalls: []string{"shortcut:Play Focus"}},
	} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%t", tc.name, dryRun), func(t *testing.T) {
				r := recordPlayBackend(t)
				if tc.multiple {
					r.matches = append([]music.UserPlaylist{{Name: "Focus Mix Extended", PersistentID: "B"}}, r.matches...)
				}
				if tc.noMatches {
					r.matches = nil
				}
				r.searchErr = tc.searchErr
				cfg := &native.Config{
					Defaults: native.DefaultsConfig{Backend: tc.backend, Rooms: []string{"Bedroom"}, Shuffle: true},
					Native:   native.NativeConfig{Playlists: map[string]map[string]string{"Bedroom": {"Focus Mix": "Play Focus"}}},
				}
				if tc.backend == "native" {
					volume := 30
					cfg.Defaults.Volume = &volume
				}
				args := append([]string{"Focus Mix"}, tc.args...)
				_, err := runPlayJSON(t, cfg, args, dryRun)
				wantCalls, wantErr := tc.wantCalls, tc.wantErr
				if dryRun {
					wantCalls, wantErr = nil, ""
				}
				if wantErr == "" && err != nil {
					t.Fatal(err)
				}
				if wantErr != "" && (err == nil || !strings.Contains(err.Error(), wantErr)) {
					t.Fatalf("error=%v, want %q", err, wantErr)
				}
				if !slices.Equal(r.calls, wantCalls) {
					t.Fatalf("calls=%v, want=%v", r.calls, wantCalls)
				}
			})
		}
	}
}

func TestCmdPlayIDBypassesChoose(t *testing.T) {
	for _, backend := range []string{"airplay", "native"} {
		t.Run(backend, func(t *testing.T) {
			r := recordPlayBackend(t)
			r.searchErr = errors.New("must not search")
			cfg := &native.Config{
				Defaults: native.DefaultsConfig{Backend: backend, Rooms: []string{"Bedroom"}},
				Native:   native.NativeConfig{Playlists: map[string]map[string]string{"Bedroom": {"Focus Mix": "Play Focus"}}},
			}
			_, err := runPlayJSON(t, cfg, []string{"--playlist-id", "A", "--choose", "--no-input"}, false)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"outputs:Bedroom", "shuffle:false", "play:A", "now"}
			if backend == "native" {
				want = []string{"lookup:A", "shortcut:Play Focus"}
			}
			if !slices.Equal(r.calls, want) {
				t.Fatalf("calls=%v, want=%v", r.calls, want)
			}
		})
	}
}

func TestCmdPlayNativePreviewDoesNotCheckMappings(t *testing.T) {
	for _, target := range [][]string{{"Focus Mix"}, {"--playlist-id", "A"}} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v/dry=%t", target, dryRun), func(t *testing.T) {
				r := recordPlayBackend(t)
				cfg := &native.Config{Defaults: native.DefaultsConfig{Backend: "native", Rooms: []string{"Bedroom"}}}
				_, err := runPlayJSON(t, cfg, target, dryRun)
				if dryRun && err != nil {
					t.Fatal(err)
				}
				if !dryRun && (err == nil || !strings.Contains(err.Error(), "no native mapping")) {
					t.Fatalf("error=%v, want missing native mapping", err)
				}
				var want []string
				if !dryRun && target[0] == "--playlist-id" {
					want = []string{"lookup:A"}
				}
				if !slices.Equal(r.calls, want) {
					t.Fatalf("calls=%v, want=%v", r.calls, want)
				}
			})
		}
	}
}
