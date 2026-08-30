package main

import (
	"context"
	"maps"
	"strings"
	"time"

	"github.com/agisilaos/homepodctl/internal/native"
)

// A resolved plan is an offline recipe. Playlist searches and current-output
// lookups remain execution-time operations, since earlier steps can affect them.
type resolvedAutomationPlan struct {
	Name         string
	Version      string
	Steps        []resolvedAutomationStep
	nativeConfig *native.Config
}

type resolvedAutomationStep struct {
	Input   automationStep
	Payload automationStepPayload
}

// Only typed operations can enter a compiled plan or its resolved JSON output.
type automationStepPayload interface {
	stepType() string
	execute(context.Context, *native.Config) error
}

type automationOutSet struct {
	Backend string   `json:"backend"`
	Rooms   []string `json:"rooms"`
}

type automationPlay struct {
	Backend    string   `json:"backend"`
	Query      string   `json:"query,omitempty"`
	PlaylistID string   `json:"playlistId,omitempty"`
	Rooms      []string `json:"rooms,omitempty"`
	Volume     *int     `json:"volume,omitempty"`
	Shuffle    *bool    `json:"shuffle,omitempty"`
}

type automationVolumeSet struct {
	Backend string `json:"backend"`
	Value   int    `json:"value"`
	// Empty rooms mean current outputs for AirPlay, resolved when this step runs.
	Rooms []string `json:"rooms,omitempty"`
}

type automationWait struct {
	// Backend is retained as plan metadata; waiting always observes Music.
	Backend string `json:"backend"`
	State   string `json:"state"`
	Timeout string `json:"timeout"`
	// Keep the original JSON spelling while executing the parsed duration.
	timeout time.Duration
}

type automationTransport struct {
	// Backend is retained as plan metadata; stop always controls Music.
	Backend string `json:"backend"`
	Action  string `json:"action"`
}

func (automationOutSet) stepType() string    { return "out.set" }
func (automationPlay) stepType() string      { return "play" }
func (automationVolumeSet) stepType() string { return "volume.set" }
func (automationWait) stepType() string      { return "wait" }
func (automationTransport) stepType() string { return "transport" }

func compileAutomationPlan(cfg *native.Config, doc *automationFile) (resolvedAutomationPlan, error) {
	if err := validateAutomation(doc); err != nil {
		return resolvedAutomationPlan{}, err
	}
	defaults := resolveAutomationDefaults(cfg, doc.Defaults)
	plan := resolvedAutomationPlan{
		Name: doc.Name, Version: doc.Version,
		Steps: make([]resolvedAutomationStep, 0, len(doc.Steps)),
	}
	if cfg != nil {
		// Snapshot mappings without checking preconditions: a missing mapping must
		// still fail at its step, after any earlier steps have executed.
		plan.nativeConfig = &native.Config{Native: native.NativeConfig{
			Playlists:       cloneAutomationMappings(cfg.Native.Playlists),
			VolumeShortcuts: cloneAutomationMappings(cfg.Native.VolumeShortcuts),
		}}
	}
	for _, st := range doc.Steps {
		var payload automationStepPayload
		switch strings.TrimSpace(st.Type) {
		case "out.set":
			payload = automationOutSet{Backend: defaults.Backend, Rooms: append([]string(nil), st.Rooms...)}
		case "play":
			play := automationPlay{
				Backend: defaults.Backend, Query: st.Query,
				PlaylistID: strings.TrimSpace(st.PlaylistID),
				Rooms:      append([]string(nil), defaults.Rooms...),
			}
			if strings.TrimSpace(play.Query) == "" {
				play.Query = ""
			}
			if play.Backend == "native" {
				play.Query = strings.TrimSpace(play.Query)
			}
			if play.Backend == "airplay" {
				play.Shuffle = cloneAutomationValue(defaults.Shuffle)
				if len(play.Rooms) > 0 {
					play.Volume = cloneAutomationValue(defaults.Volume)
				}
			}
			payload = play
		case "volume.set":
			rooms := st.Rooms
			if len(rooms) == 0 {
				rooms = defaults.Rooms
			}
			payload = automationVolumeSet{
				Backend: defaults.Backend, Value: *st.Value,
				Rooms: append([]string(nil), rooms...),
			}
		case "wait":
			timeout, err := time.ParseDuration(st.Timeout)
			if err != nil {
				return resolvedAutomationPlan{}, err
			}
			payload = automationWait{
				Backend: defaults.Backend, State: strings.TrimSpace(st.State),
				Timeout: st.Timeout, timeout: timeout,
			}
		case "transport":
			payload = automationTransport{Backend: defaults.Backend, Action: strings.TrimSpace(st.Action)}
		default:
			return resolvedAutomationPlan{}, automationValidationErrf("unsupported step type %q", st.Type)
		}
		// Input is retained for reporting only; execution cannot reinterpret it.
		st.Rooms = append([]string(nil), st.Rooms...)
		st.Value = cloneAutomationValue(st.Value)
		plan.Steps = append(plan.Steps, resolvedAutomationStep{Input: st, Payload: payload})
	}
	return plan, nil
}

func resolveAutomationDefaults(cfg *native.Config, in automationDefaults) automationDefaults {
	out := in
	if cfg != nil {
		if strings.TrimSpace(out.Backend) == "" {
			out.Backend = cfg.Defaults.Backend
		}
		if len(out.Rooms) == 0 {
			out.Rooms = cfg.Defaults.Rooms
		}
		if out.Volume == nil {
			out.Volume = cfg.Defaults.Volume
		}
		if out.Shuffle == nil {
			out.Shuffle = &cfg.Defaults.Shuffle
		}
	}
	out.Backend = strings.TrimSpace(out.Backend)
	if out.Backend == "" {
		out.Backend = "airplay"
	}
	return out
}

func cloneAutomationValue[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneAutomationMappings(in map[string]map[string]string) map[string]map[string]string {
	out := maps.Clone(in)
	for room, mappings := range out {
		out[room] = maps.Clone(mappings)
	}
	return out
}
