package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func mustCompileAutomation(t *testing.T, cfg *native.Config, doc *automationFile) resolvedAutomationPlan {
	t.Helper()
	plan, err := compileAutomationPlan(cfg, doc)
	if err != nil {
		t.Fatalf("compileAutomationPlan: %v", err)
	}
	return plan
}

func TestAutomationCommandsShareResolvedPlan(t *testing.T) {
	origOutputs, origVolume, origShuffle := setCurrentOutputs, setDeviceVolume, setShuffle
	origSearch, origPlay, origNow, origStop := searchPlaylists, playPlaylistByID, getNowPlaying, stopPlayback
	t.Cleanup(func() {
		setCurrentOutputs, setDeviceVolume, setShuffle = origOutputs, origVolume, origShuffle
		searchPlaylists, playPlaylistByID, getNowPlaying, stopPlayback = origSearch, origPlay, origNow, origStop
	})
	var calls []string
	setCurrentOutputs = func(_ context.Context, rooms []string) error {
		calls = append(calls, "outputs:"+strings.Join(rooms, ","))
		return nil
	}
	setDeviceVolume = func(_ context.Context, room string, value int) error {
		calls = append(calls, fmt.Sprintf("volume:%s:%d", room, value))
		return nil
	}
	setShuffle = func(_ context.Context, value bool) error {
		calls = append(calls, fmt.Sprintf("shuffle:%t", value))
		return nil
	}
	searchPlaylists = func(_ context.Context, query string) ([]music.UserPlaylist, error) {
		calls = append(calls, "search:"+query)
		return []music.UserPlaylist{{Name: "Focus", PersistentID: "P1"}}, nil
	}
	playPlaylistByID = func(_ context.Context, id string) error {
		calls = append(calls, "play:"+id)
		return nil
	}
	getNowPlaying = func(context.Context) (music.NowPlaying, error) {
		calls = append(calls, "state")
		return music.NowPlaying{PlayerState: "playing"}, nil
	}
	stopPlayback = func(context.Context) error {
		calls = append(calls, "stop")
		return nil
	}
	file := filepath.Join(t.TempDir(), "routine.yaml")
	if err := os.WriteFile(file, []byte(`version: "1"
name: shared-plan
defaults:
  rooms: [Office]
  volume: 20
  shuffle: false
steps:
  - type: out.set
    rooms: [Kitchen]
  - type: play
    query: Focus
  - type: volume.set
    rooms: [Bedroom]
    value: 0
  - type: wait
    state: playing
    timeout: 1000ms
  - type: transport
    action: stop
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &native.Config{Defaults: native.DefaultsConfig{
		Backend: "airplay", Rooms: []string{"Config Room"}, Volume: intPtr(99), Shuffle: true,
	}}
	var reference []any
	for _, mode := range []string{"plan", "dry-run", "run"} {
		out := captureStdout(t, func() {
			args := []string{"-f", file, "--json"}
			if mode == "plan" {
				cmdAutomationPlan(cfg, args)
			} else {
				if mode == "dry-run" {
					args = append(args, "--dry-run")
				}
				cmdAutomationRun(context.Background(), cfg, args)
			}
		})
		var result struct {
			Mode  string `json:"mode"`
			OK    bool   `json:"ok"`
			Steps []struct {
				Resolved any `json:"resolved"`
			} `json:"steps"`
		}
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatal(err)
		}
		if result.Mode != mode || !result.OK {
			t.Fatalf("%s result: %s", mode, out)
		}
		resolved := make([]any, len(result.Steps))
		for i, step := range result.Steps {
			if step.Resolved == nil {
				t.Fatalf("%s step %d lost resolved payload", mode, i)
			}
			resolved[i] = step.Resolved
		}
		if mode == "plan" {
			reference = resolved
		} else if !reflect.DeepEqual(reference, resolved) {
			t.Fatalf("%s resolved=%+v, plan=%+v", mode, resolved, reference)
		}
		if mode != "run" && len(calls) != 0 {
			t.Fatalf("%s made runtime calls: %v", mode, calls)
		}
	}
	wantCalls := []string{
		"outputs:Kitchen", "outputs:Office", "volume:Office:20", "shuffle:false",
		"search:Focus", "play:P1", "volume:Bedroom:0", "state", "stop",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls=%v, want %v", calls, wantCalls)
	}
}

func TestAutomationResolvedJSON(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		cfg      *native.Config
		defaults automationDefaults
		step     automationStep
		want     string
	}{
		{
			name: "output payload excludes unrelated input fields",
			step: automationStep{Type: "out.set", Rooms: []string{"Office"}, Query: "ignored", Value: intPtr(99)},
			want: `{"backend":"airplay","rooms":["Office"]}`,
		},
		{
			name:     "native play omits ineffective settings",
			defaults: automationDefaults{Backend: "native", Rooms: []string{"Office"}, Volume: intPtr(0), Shuffle: boolPtr(false)},
			step:     automationStep{Type: "play", Query: "Focus"},
			want:     `{"backend":"native","query":"Focus","rooms":["Office"]}`,
		},
		{
			name:     "roomless airplay play omits volume",
			defaults: automationDefaults{Volume: intPtr(20)},
			step:     automationStep{Type: "play", PlaylistID: "P1"},
			want:     `{"backend":"airplay","playlistId":"P1"}`,
		},
		{
			name: "config defaults preserve zero and false",
			cfg:  &native.Config{Defaults: native.DefaultsConfig{Rooms: []string{"Office"}, Volume: intPtr(0), Shuffle: false}},
			step: automationStep{Type: "play", PlaylistID: "P1"},
			want: `{"backend":"airplay","playlistId":"P1","rooms":["Office"],"volume":0,"shuffle":false}`,
		},
		{
			name: "volume defers current outputs",
			step: automationStep{Type: "volume.set", Value: intPtr(0)},
			want: `{"backend":"airplay","value":0}`,
		},
		{
			name:     "volume uses file defaults before config",
			cfg:      &native.Config{Defaults: native.DefaultsConfig{Rooms: []string{"Config Room"}}},
			defaults: automationDefaults{Rooms: []string{"Office"}},
			step:     automationStep{Type: "volume.set", Value: intPtr(30)},
			want:     `{"backend":"airplay","value":30,"rooms":["Office"]}`,
		},
		{
			name: "wait keeps duration spelling and normalizes state",
			step: automationStep{Type: " wait ", State: " playing ", Timeout: "1000ms"},
			want: `{"backend":"airplay","state":"playing","timeout":"1000ms"}`,
		},
		{
			name: "transport normalizes action",
			step: automationStep{Type: "transport", Action: " stop "},
			want: `{"backend":"airplay","action":"stop"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := mustCompileAutomation(t, tc.cfg, &automationFile{
				Version: "1", Name: tc.name, Defaults: tc.defaults, Steps: []automationStep{tc.step},
			})
			got, err := json.Marshal(plan.Steps[0].Payload)
			if err != nil {
				t.Fatal(err)
			}
			var gotJSON, wantJSON any
			if err := json.Unmarshal(got, &gotJSON); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.want), &wantJSON); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotJSON, wantJSON) {
				t.Fatalf("resolved=%s, want %s", got, tc.want)
			}
			if !reflect.DeepEqual(plan.Steps[0].Input, tc.step) {
				t.Fatalf("raw input changed: %+v", plan.Steps[0].Input)
			}
		})
	}
}

func TestExecuteAutomationPlanFailurePositions(t *testing.T) {
	origStop := stopPlayback
	t.Cleanup(func() { stopPlayback = origStop })
	plan := mustCompileAutomation(t, nil, &automationFile{
		Version: "1", Name: "failure",
		Steps: []automationStep{{Type: "transport", Action: "stop"}, {Type: "transport", Action: "stop"}, {Type: "transport", Action: "stop"}},
	})
	preview := previewAutomationPlan("plan", plan)
	for _, failAt := range []int{-1, 0, 1, 2} {
		t.Run(fmt.Sprintf("failure_%d", failAt), func(t *testing.T) {
			calls := 0
			stopPlayback = func(context.Context) error {
				index := calls
				calls++
				if index == failAt {
					return errors.New("boom")
				}
				return nil
			}
			result := executeAutomationPlan(context.Background(), plan)
			if result.OK != (failAt == -1) || len(result.Steps) != len(plan.Steps) {
				t.Fatalf("unexpected result: %+v", result)
			}
			wantCalls := len(plan.Steps)
			if failAt >= 0 {
				wantCalls = failAt + 1
			}
			if calls != wantCalls {
				t.Fatalf("calls=%d want %d", calls, wantCalls)
			}
			for i, step := range result.Steps {
				wantOK := failAt == -1 || i < failAt
				wantSkipped := failAt >= 0 && i > failAt
				if step.OK != wantOK || step.Skipped != wantSkipped || step.Index != i {
					t.Fatalf("step %d state: %+v", i, step)
				}
				if i == failAt && step.Error != "boom" {
					t.Fatalf("failed step error=%q", step.Error)
				}
				if wantOK && step.Error != "" {
					t.Fatalf("successful step error=%q", step.Error)
				}
				if wantSkipped && (step.DurationMS != 0 || step.Error != "skipped due to previous step failure") {
					t.Fatalf("skipped step: %+v", step)
				}
				if !reflect.DeepEqual(step.Resolved, preview.Steps[i].Resolved) || !reflect.DeepEqual(step.Input, preview.Steps[i].Input) {
					t.Fatalf("step %d lost its plan: %+v", i, step)
				}
			}
			if after := previewAutomationPlan("plan", plan); !reflect.DeepEqual(after.Steps, preview.Steps) {
				t.Fatal("execution mutated the plan")
			}
		})
	}
}

func TestExecuteAutomationPlanTiming(t *testing.T) {
	origStop := stopPlayback
	t.Cleanup(func() { stopPlayback = origStop })
	plan := mustCompileAutomation(t, nil, &automationFile{
		Version: "1", Name: "timing", Steps: []automationStep{{Type: "transport", Action: "stop"}},
	})
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("fail_%t", fail), func(t *testing.T) {
			var entered, left time.Time
			stopPlayback = func(context.Context) error {
				entered = time.Now()
				time.Sleep(12 * time.Millisecond)
				left = time.Now()
				if fail {
					return errors.New("timed failure")
				}
				return nil
			}
			before := time.Now()
			result := executeAutomationPlan(context.Background(), plan)
			after := time.Now()
			started, err := time.Parse(time.RFC3339Nano, result.StartedAt)
			if err != nil {
				t.Fatal(err)
			}
			ended, err := time.Parse(time.RFC3339Nano, result.EndedAt)
			if err != nil {
				t.Fatal(err)
			}
			if started.Before(before) || started.After(entered) || ended.Before(left) || ended.After(after) {
				t.Fatalf("timestamps do not surround execution: %+v; entered=%v left=%v", result, entered, left)
			}
			stepDuration := result.Steps[0].DurationMS
			if stepDuration < left.Sub(entered).Milliseconds() || result.DurationMS < stepDuration || result.DurationMS > after.Sub(before).Milliseconds() {
				t.Fatalf("durations do not include execution: %+v", result)
			}
			if result.OK == fail {
				t.Fatalf("ok=%t fail=%t", result.OK, fail)
			}
		})
	}
}

func TestAutomationPlanSnapshotsInputsAndDefaults(t *testing.T) {
	origOutputs, origVolume, origShuffle, origPlay := setCurrentOutputs, setDeviceVolume, setShuffle, playPlaylistByID
	t.Cleanup(func() {
		setCurrentOutputs, setDeviceVolume, setShuffle, playPlaylistByID = origOutputs, origVolume, origShuffle, origPlay
	})
	cfg := &native.Config{Defaults: native.DefaultsConfig{
		Rooms: []string{"Office"}, Volume: intPtr(20), Shuffle: false,
	}}
	doc := &automationFile{
		Version: "1", Name: "snapshot",
		Steps: []automationStep{{Type: "play", PlaylistID: "P1"}, {Type: "volume.set", Rooms: []string{"Kitchen"}, Value: intPtr(30)}},
	}
	plan := mustCompileAutomation(t, cfg, doc)
	before, err := json.Marshal(previewAutomationPlan("plan", plan).Steps)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Defaults.Backend = "native"
	cfg.Defaults.Rooms[0] = "Changed"
	*cfg.Defaults.Volume = 99
	cfg.Defaults.Shuffle = true
	doc.Name = "changed"
	doc.Steps[0].PlaylistID = "P2"
	doc.Steps[1].Rooms[0] = "Changed"
	*doc.Steps[1].Value = 99
	// Even reporting metadata is not an execution source.
	plan.Steps[0].Input.PlaylistID = "not-an-operation"
	var calls []string
	setCurrentOutputs = func(_ context.Context, rooms []string) error {
		calls = append(calls, "outputs:"+strings.Join(rooms, ","))
		return nil
	}
	setDeviceVolume = func(_ context.Context, room string, value int) error {
		calls = append(calls, fmt.Sprintf("volume:%s:%d", room, value))
		return nil
	}
	setShuffle = func(_ context.Context, value bool) error {
		calls = append(calls, fmt.Sprintf("shuffle:%t", value))
		return nil
	}
	playPlaylistByID = func(_ context.Context, id string) error {
		calls = append(calls, "play:"+id)
		return nil
	}
	result := executeAutomationPlan(context.Background(), plan)
	want := []string{"outputs:Office", "volume:Office:20", "shuffle:false", "play:P1", "volume:Kitchen:30"}
	if !result.OK || result.Name != "snapshot" || !reflect.DeepEqual(calls, want) {
		t.Fatalf("result=%+v calls=%v want=%v", result, calls, want)
	}
	plan.Steps[0].Input.PlaylistID = "P1"
	after, err := json.Marshal(previewAutomationPlan("plan", plan).Steps)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("compiled plan changed: before=%s after=%s", before, after)
	}
}

func TestAutomationNativePlanSnapshotsMappingsAndDefersPlaylistLookup(t *testing.T) {
	origShortcut, origName := runNativeShortcut, findPlaylistNameByID
	t.Cleanup(func() { runNativeShortcut, findPlaylistNameByID = origShortcut, origName })
	var calls []string
	runNativeShortcut = func(_ context.Context, name string) error {
		calls = append(calls, name)
		return nil
	}
	findPlaylistNameByID = func(_ context.Context, id string) (string, error) {
		calls = append(calls, "lookup:"+id)
		return "Focus", nil
	}
	cfg := &native.Config{
		Defaults: native.DefaultsConfig{Backend: "native", Rooms: []string{"Office"}},
		Native: native.NativeConfig{
			Playlists:       map[string]map[string]string{"Office": {"Focus": "Office Focus"}},
			VolumeShortcuts: map[string]map[string]string{"Office": {"30": "Office 30"}},
		},
	}
	plan := mustCompileAutomation(t, cfg, &automationFile{
		Version: "1", Name: "native snapshot",
		Steps: []automationStep{{Type: "play", Query: "Focus"}, {Type: "play", PlaylistID: "P1"}, {Type: "volume.set", Value: intPtr(30)}},
	})
	if len(calls) != 0 {
		t.Fatalf("compilation accessed live state: %v", calls)
	}
	cfg.Native.Playlists["Office"]["Focus"] = "Changed"
	delete(cfg.Native.VolumeShortcuts["Office"], "30")
	result := executeAutomationPlan(context.Background(), plan)
	want := []string{"Office Focus", "lookup:P1", "Office Focus", "Office 30"}
	if !result.OK || !reflect.DeepEqual(calls, want) {
		t.Fatalf("result=%+v calls=%v want=%v", result, calls, want)
	}
}

func TestAutomationVolumeResolvesOutputsAfterEarlierStep(t *testing.T) {
	origOutputs, origVolume, origNow := setCurrentOutputs, setDeviceVolume, getNowPlaying
	t.Cleanup(func() { setCurrentOutputs, setDeviceVolume, getNowPlaying = origOutputs, origVolume, origNow })
	selected := "Old Room"
	var calls []string
	setCurrentOutputs = func(_ context.Context, rooms []string) error {
		selected = rooms[0]
		calls = append(calls, "outputs:"+selected)
		return nil
	}
	getNowPlaying = func(context.Context) (music.NowPlaying, error) {
		calls = append(calls, "current:"+selected)
		return music.NowPlaying{Outputs: []music.AirPlayDevice{{Name: selected}}}, nil
	}
	setDeviceVolume = func(_ context.Context, room string, value int) error {
		calls = append(calls, fmt.Sprintf("volume:%s:%d", room, value))
		return nil
	}
	plan := mustCompileAutomation(t, nil, &automationFile{
		Version: "1", Name: "live outputs",
		Steps: []automationStep{{Type: "out.set", Rooms: []string{"Kitchen"}}, {Type: "volume.set", Value: intPtr(30)}},
	})
	if len(calls) != 0 {
		t.Fatalf("compilation accessed live state: %v", calls)
	}
	result := executeAutomationPlan(context.Background(), plan)
	want := []string{"outputs:Kitchen", "current:Kitchen", "volume:Kitchen:30"}
	if !result.OK || !reflect.DeepEqual(calls, want) {
		t.Fatalf("result=%+v calls=%v want=%v", result, calls, want)
	}
	if !reflect.DeepEqual(result.Steps[1].Resolved, plan.Steps[1].Payload) {
		t.Fatal("live output lookup overwrote the resolved recipe")
	}
}

func TestAutomationPreconditionFailureRemainsAtItsStep(t *testing.T) {
	origStop, origShortcut := stopPlayback, runNativeShortcut
	t.Cleanup(func() { stopPlayback, runNativeShortcut = origStop, origShortcut })
	stops := 0
	stopPlayback = func(context.Context) error { stops++; return nil }
	runNativeShortcut = func(context.Context, string) error {
		t.Fatal("missing mapping must not run a shortcut")
		return nil
	}
	plan := mustCompileAutomation(t, &native.Config{}, &automationFile{
		Version: "1", Name: "precondition",
		Defaults: automationDefaults{Backend: "native", Rooms: []string{"Office"}},
		Steps:    []automationStep{{Type: "transport", Action: "stop"}, {Type: "play", Query: "Unmapped"}, {Type: "transport", Action: "stop"}},
	})
	result := executeAutomationPlan(context.Background(), plan)
	if result.OK || stops != 1 || !result.Steps[0].OK || result.Steps[1].Skipped || !result.Steps[2].Skipped {
		t.Fatalf("stops=%d result=%+v", stops, result)
	}
	if !strings.Contains(result.Steps[1].Error, "no native mapping") {
		t.Fatalf("unexpected precondition failure: %s", result.Steps[1].Error)
	}
}

func TestAutomationCancellationStopsLaterSteps(t *testing.T) {
	origStop := stopPlayback
	t.Cleanup(func() { stopPlayback = origStop })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	stopPlayback = func(context.Context) error { calls++; cancel(); return nil }
	plan := mustCompileAutomation(t, nil, &automationFile{
		Version: "1", Name: "cancel",
		Steps: []automationStep{{Type: "transport", Action: "stop"}, {Type: "transport", Action: "stop"}, {Type: "transport", Action: "stop"}},
	})
	result := executeAutomationPlan(ctx, plan)
	if result.OK || calls != 1 || !result.Steps[0].OK || result.Steps[1].Skipped || !result.Steps[2].Skipped {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
	if result.Steps[1].Error != context.Canceled.Error() {
		t.Fatalf("canceled step: %+v", result.Steps[1])
	}
}
