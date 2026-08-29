package main

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/agisilaos/homepodctl/internal/native"
)

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

func TestRunNativeShortcuts(t *testing.T) {
	originalRunNativeShortcut := runNativeShortcut
	t.Cleanup(func() { runNativeShortcut = originalRunNativeShortcut })

	type actionPath struct {
		name               string
		config             func(map[string]string) *native.Config
		run                func(context.Context, *native.Config, []string) error
		missingSecondError string
	}

	actionPaths := []actionPath{
		{
			name: "playlist",
			config: func(mappings map[string]string) *native.Config {
				playlists := make(map[string]map[string]string, len(mappings))
				for room, shortcut := range mappings {
					playlists[room] = map[string]string{"Focus": shortcut}
				}
				return &native.Config{Native: native.NativeConfig{Playlists: playlists}}
			},
			run: func(ctx context.Context, cfg *native.Config, rooms []string) error {
				return runNativePlaylistShortcuts(ctx, cfg, rooms, "Focus")
			},
			missingSecondError: `no native mapping for room="Kitchen" playlist="Focus"`,
		},
		{
			name: "volume",
			config: func(mappings map[string]string) *native.Config {
				volumes := make(map[string]map[string]string, len(mappings))
				for room, shortcut := range mappings {
					volumes[room] = map[string]string{"30": shortcut}
				}
				return &native.Config{Native: native.NativeConfig{VolumeShortcuts: volumes}}
			},
			run: func(ctx context.Context, cfg *native.Config, rooms []string) error {
				return runNativeVolumeShortcuts(ctx, cfg, rooms, 30)
			},
			missingSecondError: `no native volume mapping for room="Kitchen" value=30`,
		},
	}

	tests := []struct {
		name           string
		rooms          []string
		mappings       map[string]string
		failOnCall     int
		wantCalls      []string
		wantMappingErr bool
		wantRuntimeErr bool
	}{
		{
			name:           "missing second mapping executes nothing",
			rooms:          []string{"Bedroom", "Kitchen"},
			mappings:       map[string]string{"Bedroom": "Bedroom Shortcut"},
			wantMappingErr: true,
		},
		{
			name:      "success preserves order",
			rooms:     []string{"Bedroom", "Kitchen", "Office"},
			mappings:  map[string]string{"Bedroom": "Bedroom Shortcut", "Kitchen": "Kitchen Shortcut", "Office": "Office Shortcut"},
			wantCalls: []string{"Bedroom Shortcut", "Kitchen Shortcut", "Office Shortcut"},
		},
		{
			name:      "duplicate rooms execute duplicate shortcuts",
			rooms:     []string{"Bedroom", "Bedroom"},
			mappings:  map[string]string{"Bedroom": "Bedroom Shortcut"},
			wantCalls: []string{"Bedroom Shortcut", "Bedroom Shortcut"},
		},
		{
			name:           "second runtime failure stops execution",
			rooms:          []string{"Bedroom", "Kitchen", "Office"},
			mappings:       map[string]string{"Bedroom": "Bedroom Shortcut", "Kitchen": "Kitchen Shortcut", "Office": "Office Shortcut"},
			failOnCall:     2,
			wantCalls:      []string{"Bedroom Shortcut", "Kitchen Shortcut"},
			wantRuntimeErr: true,
		},
	}

	for _, path := range actionPaths {
		t.Run(path.name, func(t *testing.T) {
			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					var calls []string
					runtimeErr := errors.New("shortcut runtime failure")
					runNativeShortcut = func(_ context.Context, shortcut string) error {
						calls = append(calls, shortcut)
						if tc.failOnCall > 0 && len(calls) == tc.failOnCall {
							return runtimeErr
						}
						return nil
					}

					err := path.run(context.Background(), path.config(tc.mappings), tc.rooms)
					switch {
					case tc.wantMappingErr:
						if err == nil || err.Error() != path.missingSecondError {
							t.Fatalf("error=%v, want %q", err, path.missingSecondError)
						}
					case tc.wantRuntimeErr:
						if err != runtimeErr {
							t.Fatalf("error=%v, want original runtime error", err)
						}
					case err != nil:
						t.Fatalf("unexpected error: %v", err)
					}

					if !slices.Equal(calls, tc.wantCalls) {
						t.Fatalf("calls=%v, want %v", calls, tc.wantCalls)
					}
				})
			}
		})
	}
}
