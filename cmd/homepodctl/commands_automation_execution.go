package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func resolveAutomationSteps(cfg *native.Config, doc *automationFile) []automationStepResult {
	resolvedDefaults := resolveAutomationDefaults(cfg, doc.Defaults)

	out := make([]automationStepResult, 0, len(doc.Steps))
	for i, st := range doc.Steps {
		resolved := map[string]any{"backend": resolvedDefaults.Backend}
		switch st.Type {
		case "out.set":
			resolved["rooms"] = st.Rooms
		case "play":
			if strings.TrimSpace(st.Query) != "" {
				resolved["query"] = st.Query
			}
			if strings.TrimSpace(st.PlaylistID) != "" {
				resolved["playlistId"] = st.PlaylistID
			}
			if resolvedDefaults.Shuffle != nil {
				resolved["shuffle"] = *resolvedDefaults.Shuffle
			}
			if resolvedDefaults.Volume != nil {
				resolved["volume"] = *resolvedDefaults.Volume
			}
			if len(resolvedDefaults.Rooms) > 0 {
				resolved["rooms"] = resolvedDefaults.Rooms
			}
		case "volume.set":
			if st.Value != nil {
				resolved["value"] = *st.Value
			}
			if len(st.Rooms) > 0 {
				resolved["rooms"] = st.Rooms
			} else if len(resolvedDefaults.Rooms) > 0 {
				resolved["rooms"] = resolvedDefaults.Rooms
			}
		case "wait":
			resolved["state"] = st.State
			resolved["timeout"] = st.Timeout
		case "transport":
			resolved["action"] = st.Action
		}
		out = append(out, automationStepResult{
			Index:      i,
			Type:       st.Type,
			Input:      st,
			Resolved:   resolved,
			OK:         true,
			Skipped:    false,
			DurationMS: 0,
		})
	}
	return out
}

func resolveAutomationDefaults(cfg *native.Config, in automationDefaults) automationDefaults {
	out := in
	if cfg == nil {
		return out
	}
	if strings.TrimSpace(out.Backend) == "" {
		out.Backend = cfg.Defaults.Backend
	}
	if len(out.Rooms) == 0 {
		out.Rooms = append([]string(nil), cfg.Defaults.Rooms...)
	}
	if out.Volume == nil && cfg.Defaults.Volume != nil {
		v := *cfg.Defaults.Volume
		out.Volume = &v
	}
	if out.Shuffle == nil {
		v := cfg.Defaults.Shuffle
		out.Shuffle = &v
	}
	return out
}

func executeAutomationSteps(ctx context.Context, cfg *native.Config, doc *automationFile) ([]automationStepResult, bool) {
	defaults := resolveAutomationDefaults(cfg, doc.Defaults)
	results := make([]automationStepResult, 0, len(doc.Steps))
	ok := true

	for i, st := range doc.Steps {
		stepStart := time.Now()
		res := automationStepResult{
			Index: i,
			Type:  st.Type,
			Input: st,
		}
		err := executeAutomationStep(ctx, cfg, defaults, st)
		res.DurationMS = time.Since(stepStart).Milliseconds()
		if err != nil {
			res.OK = false
			res.Error = err.Error()
			ok = false
			results = append(results, res)
			// mark remaining steps as skipped so callers can inspect full plan shape.
			for j := i + 1; j < len(doc.Steps); j++ {
				results = append(results, automationStepResult{
					Index:   j,
					Type:    doc.Steps[j].Type,
					Input:   doc.Steps[j],
					OK:      false,
					Skipped: true,
					Error:   "skipped due to previous step failure",
				})
			}
			break
		}
		res.OK = true
		results = append(results, res)
	}
	return results, ok
}

func executeAutomationStep(ctx context.Context, cfg *native.Config, defaults automationDefaults, st automationStep) error {
	backend := strings.TrimSpace(defaults.Backend)
	if backend == "" {
		backend = "airplay"
	}

	switch st.Type {
	case "out.set":
		if backend != "airplay" {
			return fmt.Errorf("out.set only supports backend=airplay")
		}
		return setCurrentOutputs(ctx, st.Rooms)
	case "play":
		return executeAutomationPlay(ctx, cfg, backend, defaults, st)
	case "volume.set":
		if st.Value == nil {
			return fmt.Errorf("volume.set requires value")
		}
		return executeAutomationVolume(ctx, cfg, backend, defaults, *st.Value, st.Rooms)
	case "wait":
		return executeAutomationWait(ctx, st.State, st.Timeout)
	case "transport":
		if strings.TrimSpace(st.Action) != "stop" {
			return fmt.Errorf("unsupported transport action %q", st.Action)
		}
		return stopPlayback(ctx)
	default:
		return fmt.Errorf("unsupported step type %q", st.Type)
	}
}

func executeAutomationPlay(ctx context.Context, cfg *native.Config, backend string, defaults automationDefaults, st automationStep) error {
	switch backend {
	case "airplay":
		rooms := append([]string(nil), defaults.Rooms...)
		if len(rooms) > 0 {
			if err := setCurrentOutputs(ctx, rooms); err != nil {
				return err
			}
		}
		if defaults.Volume != nil && len(rooms) > 0 {
			if err := setVolumeForRooms(ctx, rooms, *defaults.Volume); err != nil {
				return err
			}
		}
		if defaults.Shuffle != nil {
			if err := setShuffle(ctx, *defaults.Shuffle); err != nil {
				return err
			}
		}
		id := strings.TrimSpace(st.PlaylistID)
		if id == "" {
			matches, err := searchPlaylists(ctx, st.Query)
			if err != nil {
				return err
			}
			best, ok := music.PickBestPlaylist(st.Query, matches)
			if !ok {
				return fmt.Errorf("no playlists match %q", st.Query)
			}
			id = best.PersistentID
		}
		return playPlaylistByID(ctx, id)
	case "native":
		if cfg == nil {
			return fmt.Errorf("native backend requires config")
		}
		rooms := append([]string(nil), defaults.Rooms...)
		if len(rooms) == 0 {
			return fmt.Errorf("native play requires rooms")
		}
		name := strings.TrimSpace(st.Query)
		if name == "" {
			var err error
			name, err = findPlaylistNameByID(ctx, st.PlaylistID)
			if err != nil {
				return err
			}
		}
		return runNativePlaylistShortcuts(ctx, cfg, rooms, name)
	default:
		return fmt.Errorf("unknown backend %q", backend)
	}
}

func executeAutomationVolume(ctx context.Context, cfg *native.Config, backend string, defaults automationDefaults, value int, overrideRooms []string) error {
	rooms := append([]string(nil), overrideRooms...)
	if len(rooms) == 0 {
		rooms = append(rooms, defaults.Rooms...)
	}
	switch backend {
	case "airplay":
		if len(rooms) == 0 {
			rooms = inferSelectedOutputs(ctx)
		}
		if len(rooms) == 0 {
			return fmt.Errorf("no rooms available for volume.set")
		}
		return setVolumeForRooms(ctx, rooms, value)
	case "native":
		if cfg == nil {
			return fmt.Errorf("native backend requires config")
		}
		if len(rooms) == 0 {
			return fmt.Errorf("native volume.set requires rooms")
		}
		return runNativeVolumeShortcuts(ctx, cfg, rooms, value)
	default:
		return fmt.Errorf("unknown backend %q", backend)
	}
}

func executeAutomationWait(ctx context.Context, wantState string, timeoutRaw string) error {
	timeout, err := time.ParseDuration(timeoutRaw)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	want := strings.ToLower(strings.TrimSpace(wantState))
	for {
		np, err := getNowPlaying(ctx)
		if err != nil {
			return err
		}
		if strings.ToLower(strings.TrimSpace(np.PlayerState)) == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait timeout after %s for state=%s", timeout.String(), want)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		sleepFn(1 * time.Second)
	}
}
