package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestCLIPlayPreviewSettings(t *testing.T) {
	cli := newCLIHarness(t)
	for _, args := range [][]string{
		{"config", "set", "defaults.rooms", "Bedroom"},
		{"config", "set", "defaults.shuffle", "true"},
	} {
		if result := cli.run(t, args...); result.ExitCode != 0 {
			t.Fatalf("configure defaults: %+v", result)
		}
	}
	for _, tc := range []struct {
		name          string
		defaultVolume string
		args          []string
		volume        *int
		shuffle       *bool
	}{
		{"defaults", "25", nil, intPtr(25), boolPtr(true)},
		{"overrides", "25", []string{"--volume", "90", "--shuffle", "false"}, intPtr(90), boolPtr(false)},
		{"zero and false", "25", []string{"--volume", "0", "--shuffle", "false"}, intPtr(0), boolPtr(false)},
		{"unchanged volume", "null", nil, nil, boolPtr(true)},
		{"native ignores settings", "25", []string{"--backend", "native", "--volume", "90", "--shuffle", "false"}, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if result := cli.run(t, "config", "set", "defaults.volume", tc.defaultVolume); result.ExitCode != 0 {
				t.Fatalf("configure volume: %+v", result)
			}
			for _, plan := range []bool{false, true} {
				for _, format := range []string{"text", "plain", "json"} {
					if plan && format == "plain" {
						continue // plan has no --plain flag.
					}
					args := []string{"play", "chill", "--dry-run", "--no-input"}
					if plan {
						args = []string{"plan", "play", "chill", "--no-input"}
					}
					args = append(args, tc.args...)
					if format != "text" {
						args = append(args, "--"+format)
					}
					result := cli.run(t, args...)
					if result.ExitCode != 0 || result.Stderr != "" {
						t.Fatalf("preview failed: %+v", result)
					}
					if format == "json" {
						var payload map[string]any
						if err := json.Unmarshal([]byte(result.Stdout), &payload); err != nil {
							t.Fatal(err)
						}
						if plan {
							var ok bool
							payload, ok = payload["plan"].(map[string]any)
							if !ok {
								t.Fatal("missing plan object")
							}
						}
						volume, hasVolume := payload["volume"]
						shuffle, hasShuffle := payload["shuffle"]
						if hasVolume != (tc.volume != nil) || (tc.volume != nil && volume != float64(*tc.volume)) {
							t.Fatalf("unexpected volume: %s", result.Stdout)
						}
						if hasShuffle != (tc.shuffle != nil) || (tc.shuffle != nil && shuffle != *tc.shuffle) {
							t.Fatalf("unexpected shuffle: %s", result.Stdout)
						}
						continue
					}
					if tc.volume == nil {
						if strings.Contains(result.Stdout, " volume=") {
							t.Fatalf("unexpected volume: %s", result.Stdout)
						}
					} else if !strings.Contains(result.Stdout, fmt.Sprintf(" volume=%d", *tc.volume)) {
						t.Fatalf("missing effective volume: %s", result.Stdout)
					}
					if tc.shuffle == nil {
						if strings.Contains(result.Stdout, " shuffle=") {
							t.Fatalf("unexpected shuffle: %s", result.Stdout)
						}
					} else if !strings.Contains(result.Stdout, fmt.Sprintf(" shuffle=%t", *tc.shuffle)) {
						t.Fatalf("missing effective shuffle: %s", result.Stdout)
					}
				}
			}
		})
	}
}
