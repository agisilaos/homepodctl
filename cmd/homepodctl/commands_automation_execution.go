package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func executeAutomationPlan(ctx context.Context, plan resolvedAutomationPlan) automationCommandResult {
	started := time.Now()
	result := automationResultFromPlan("run", plan)
	for i, st := range plan.Steps {
		res := &result.Steps[i]
		if !result.OK {
			res.OK = false
			res.Skipped = true
			res.Error = "skipped due to previous step failure"
			continue
		}
		stepStart := time.Now()
		err := ctx.Err()
		if err == nil {
			err = st.Payload.execute(ctx, plan.nativeConfig)
		}
		res.DurationMS = time.Since(stepStart).Milliseconds()
		if err != nil {
			res.OK = false
			res.Error = err.Error()
			result.OK = false
		}
	}
	setAutomationTiming(&result, started, time.Now())
	return result
}

func (st automationOutSet) execute(ctx context.Context, _ *native.Config) error {
	if st.Backend != "airplay" {
		return fmt.Errorf("out.set only supports backend=airplay")
	}
	return setCurrentOutputs(ctx, append([]string(nil), st.Rooms...))
}

func (st automationTransport) execute(ctx context.Context, _ *native.Config) error {
	return stopPlayback(ctx)
}

func (st automationPlay) execute(ctx context.Context, cfg *native.Config) error {
	switch st.Backend {
	case "airplay":
		rooms := append([]string(nil), st.Rooms...)
		if len(rooms) > 0 {
			if err := setCurrentOutputs(ctx, rooms); err != nil {
				return err
			}
		}
		if st.Volume != nil && len(rooms) > 0 {
			if err := setVolumeForRooms(ctx, rooms, *st.Volume); err != nil {
				return err
			}
		}
		if st.Shuffle != nil {
			if err := setShuffle(ctx, *st.Shuffle); err != nil {
				return err
			}
		}
		id := st.PlaylistID
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
		rooms := append([]string(nil), st.Rooms...)
		if len(rooms) == 0 {
			return fmt.Errorf("native play requires rooms")
		}
		name := st.Query
		if name == "" {
			var err error
			name, err = findPlaylistNameByID(ctx, st.PlaylistID)
			if err != nil {
				return err
			}
		}
		return runNativePlaylistShortcuts(ctx, cfg, rooms, name)
	default:
		return fmt.Errorf("unknown backend %q", st.Backend)
	}
}

func (st automationVolumeSet) execute(ctx context.Context, cfg *native.Config) error {
	rooms := append([]string(nil), st.Rooms...)
	switch st.Backend {
	case "airplay":
		if len(rooms) == 0 {
			rooms = inferSelectedOutputs(ctx)
		}
		if len(rooms) == 0 {
			return fmt.Errorf("no rooms available for volume.set")
		}
		return setVolumeForRooms(ctx, rooms, st.Value)
	case "native":
		if cfg == nil {
			return fmt.Errorf("native backend requires config")
		}
		if len(rooms) == 0 {
			return fmt.Errorf("native volume.set requires rooms")
		}
		return runNativeVolumeShortcuts(ctx, cfg, rooms, st.Value)
	default:
		return fmt.Errorf("unknown backend %q", st.Backend)
	}
}

func (st automationWait) execute(ctx context.Context, _ *native.Config) error {
	deadline := time.Now().Add(st.timeout)
	want := st.State
	for {
		np, err := getNowPlaying(ctx)
		if err != nil {
			return err
		}
		if strings.ToLower(strings.TrimSpace(np.PlayerState)) == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait timeout after %s for state=%s", st.timeout.String(), want)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		sleepFn(1 * time.Second)
	}
}
