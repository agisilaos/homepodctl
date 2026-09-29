package main

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/agisilaos/homepodctl/internal/native"
)

func TestRunPreviewMatchesAppliedSettings(t *testing.T) {
	for _, tc := range []struct {
		name          string
		alias         native.Alias
		defaultVolume *int
		wantVolume    *int
		wantShuffle   *bool
	}{
		{"defaults", native.Alias{}, intPtr(25), intPtr(25), nil},
		{"overrides", native.Alias{Volume: intPtr(90), Shuffle: boolPtr(true)}, intPtr(25), intPtr(90), boolPtr(true)},
		{"zero and false", native.Alias{Volume: intPtr(0), Shuffle: boolPtr(false)}, intPtr(25), intPtr(0), boolPtr(false)},
		{"unchanged", native.Alias{}, nil, nil, nil},
		{"native ignores settings", native.Alias{Backend: "native", Playlist: "Focus", Volume: intPtr(90), Shuffle: boolPtr(true)}, intPtr(25), nil, nil},
	} {
		for _, dry := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/dry=%t", tc.name, dry), func(t *testing.T) {
				recorder := recordPlayBackend(t)
				cfg := &native.Config{
					Defaults: native.DefaultsConfig{Backend: "airplay", Rooms: []string{"Bedroom"}, Volume: tc.defaultVolume, Shuffle: true},
					Aliases:  map[string]native.Alias{"test": tc.alias},
					Native:   native.NativeConfig{Playlists: map[string]map[string]string{"Bedroom": {"Focus": "Play Focus"}}},
				}
				raw := captureStdout(t, func() {
					cmdRun(context.Background(), cfg, []string{"test", "--json", fmt.Sprintf("--dry-run=%t", dry)})
				})
				var result actionResult
				if err := json.Unmarshal([]byte(raw), &result); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(result.Volume, tc.wantVolume) || !reflect.DeepEqual(result.Shuffle, tc.wantShuffle) {
					t.Fatalf("unexpected settings: %s", raw)
				}
				var wantCalls []string
				if !dry {
					if tc.alias.Backend == "native" {
						wantCalls = []string{"shortcut:Play Focus"}
					} else {
						wantCalls = append(wantCalls, "outputs:Bedroom")
						if tc.wantVolume != nil {
							wantCalls = append(wantCalls, fmt.Sprintf("volume:Bedroom:%d", *tc.wantVolume))
						}
						if tc.wantShuffle != nil {
							wantCalls = append(wantCalls, fmt.Sprintf("shuffle:%t", *tc.wantShuffle))
						}
						wantCalls = append(wantCalls, "now")
					}
				}
				if !slices.Equal(recorder.calls, wantCalls) {
					t.Fatalf("calls=%v want=%v", recorder.calls, wantCalls)
				}
			})
		}
	}
}

func TestVolumePreviewMatchesAppliedSettings(t *testing.T) {
	for _, backend := range []string{"airplay", "native"} {
		for _, value := range []int{0, 90} {
			for _, dry := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%d/dry=%t", backend, value, dry), func(t *testing.T) {
					recorder := recordPlayBackend(t)
					cfg := &native.Config{
						Defaults: native.DefaultsConfig{Backend: backend, Rooms: []string{"Bedroom"}},
						Native:   native.NativeConfig{VolumeShortcuts: map[string]map[string]string{"Bedroom": {fmt.Sprint(value): "Set Volume"}}},
					}
					raw := captureStdout(t, func() {
						cmdVolume(context.Background(), cfg, "volume", []string{fmt.Sprint(value), "--json", fmt.Sprintf("--dry-run=%t", dry)})
					})
					var result actionResult
					if err := json.Unmarshal([]byte(raw), &result); err != nil {
						t.Fatal(err)
					}
					if result.Volume == nil || *result.Volume != value || result.Shuffle != nil {
						t.Fatalf("unexpected settings: %s", raw)
					}
					var wantCalls []string
					if !dry {
						if backend == "airplay" {
							wantCalls = []string{fmt.Sprintf("volume:Bedroom:%d", value), "now"}
						} else {
							wantCalls = []string{"shortcut:Set Volume", "now"}
						}
					}
					if !slices.Equal(recorder.calls, wantCalls) {
						t.Fatalf("calls=%v want=%v", recorder.calls, wantCalls)
					}
				})
			}
		}
	}
}

func TestCLIAliasAndVolumePreviews(t *testing.T) {
	cli := newCLIHarness(t)
	for _, args := range [][]string{
		{"defaults.rooms", "Bedroom"}, {"defaults.volume", "25"}, {"defaults.shuffle", "true"},
		{"aliases.default.playlist", "Focus"}, {"aliases.zero.volume", "0"}, {"aliases.zero.shuffle", "false"},
		{"aliases.native.backend", "native"}, {"aliases.native.playlist", "Focus"}, {"aliases.native.volume", "90"}, {"aliases.native.shuffle", "true"},
		{"aliases.shortcut.shortcut", "Example"}, {"aliases.shortcut.volume", "90"}, {"aliases.shortcut.shuffle", "true"},
	} {
		if result := cli.run(t, append([]string{"config", "set"}, args...)...); result.ExitCode != 0 {
			t.Fatalf("configure: %+v", result)
		}
	}
	for _, tc := range []struct {
		name    string
		args    []string
		volume  *int
		shuffle *bool
	}{
		{"volume zero", []string{"volume", "0"}, intPtr(0), nil},
		{"vol override", []string{"vol", "90"}, intPtr(90), nil},
		{"native volume", []string{"volume", "0", "--backend", "native"}, intPtr(0), nil},
		{"alias default", []string{"run", "default"}, intPtr(25), nil},
		{"alias zero and false", []string{"run", "zero"}, intPtr(0), boolPtr(false)},
		{"native ignores settings", []string{"run", "native"}, nil, nil},
		{"shortcut ignores settings", []string{"run", "shortcut"}, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, plan := range []bool{false, true} {
				for _, format := range []string{"text", "plain", "json"} {
					if plan && format == "plain" {
						continue
					}
					args := slices.Clone(tc.args)
					if plan {
						args = append([]string{"plan"}, args...)
					} else {
						args = append(args, "--dry-run")
					}
					if format != "text" {
						args = append(args, "--"+format)
					}
					result := cli.run(t, args...)
					if result.ExitCode != 0 || result.Stderr != "" {
						t.Fatalf("preview: %+v", result)
					}
					if format == "json" {
						raw := []byte(result.Stdout)
						if plan {
							var envelope struct {
								Plan json.RawMessage `json:"plan"`
							}
							if err := json.Unmarshal(raw, &envelope); err != nil {
								t.Fatal(err)
							}
							raw = envelope.Plan
						}
						var action actionResult
						if err := json.Unmarshal(raw, &action); err != nil {
							t.Fatal(err)
						}
						if !action.DryRun || !reflect.DeepEqual(action.Volume, tc.volume) || !reflect.DeepEqual(action.Shuffle, tc.shuffle) {
							t.Fatalf("unexpected preview: %s", raw)
						}
					} else {
						for key, want := range map[string]any{"volume": tc.volume, "shuffle": tc.shuffle} {
							var token string
							switch v := want.(type) {
							case *int:
								if v != nil {
									token = fmt.Sprintf("%s=%d", key, *v)
								}
							case *bool:
								if v != nil {
									token = fmt.Sprintf("%s=%t", key, *v)
								}
							}
							if token == "" {
								if strings.Contains(result.Stdout, " "+key+"=") {
									t.Fatalf("unexpected %s: %s", key, result.Stdout)
								}
							} else if !slices.Contains(strings.Fields(result.Stdout), token) {
								t.Fatalf("missing %s: %s", token, result.Stdout)
							}
						}
					}
				}
			}
		})
	}
}
