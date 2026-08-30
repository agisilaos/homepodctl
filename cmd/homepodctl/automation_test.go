package main

import (
	"context"
	"strings"
	"testing"

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
	plan := mustCompileAutomation(t, cfg, &automationFile{
		Version: "1", Name: "native",
		Defaults: automationDefaults{Backend: "native", Rooms: []string{"Bedroom"}},
		Steps:    []automationStep{{Type: "play", Query: "Focus"}},
	})
	result := executeAutomationPlan(context.Background(), plan)
	if !result.OK {
		t.Fatalf("executeAutomationPlan: %+v", result)
	}
	if called != 1 {
		t.Fatalf("runNativeShortcut calls=%d, want 1", called)
	}
}
