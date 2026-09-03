package main

import (
	"context"
	"fmt"
	"os"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func cmdPlay(ctx context.Context, cfg *native.Config, args []string) {
	req, err := resolvePlayRequest(ctx, cfg, args)
	if err != nil {
		die(err)
	}
	out := req.actionOutput()
	if req.output.DryRun {
		writeActionOutput("play", req.output.JSON, req.output.Plain, out)
		return
	}

	switch req.backend {
	case playAirplay:
		id, err := resolveAirplayPlaylist(ctx, req)
		if err != nil {
			die(err)
		}
		debugf("play: backend=airplay rooms=%v playlist_id=%q target=%q shuffle=%t volume=%d volume_source=%s choose=%t", req.rooms, id, req.target.value, req.shuffle, req.volume.value, req.volume.source, req.choose)

		// Without resolved rooms, keep Music.app's current outputs and volume.
		if len(req.rooms) > 0 {
			if err := playbackApp.SetRoute(ctx, req.rooms); err != nil {
				die(err)
			}
			if req.volume.source != playVolumeAbsent {
				if err := setVolumeForRooms(ctx, req.rooms, req.volume.value); err != nil {
					die(err)
				}
			}
		}
		if err := setShuffle(ctx, req.shuffle); err != nil {
			die(err)
		}
		if err := playPlaylistByID(ctx, id); err != nil {
			die(err)
		}
		out.PlaylistID = id
		if np, err := playbackApp.NowPlaying(ctx); err == nil {
			out.NowPlaying = &np
		}
	case playNative:
		name := req.target.value
		if req.target.kind == playIDTarget {
			name, err = findPlaylistNameByID(ctx, req.target.value)
			if err != nil {
				die(err)
			}
		}
		debugf("play: backend=native rooms=%v playlist=%q target=%q", req.rooms, name, req.target.value)
		if err := runNativePlaylistShortcuts(ctx, cfg, req.rooms, name); err != nil {
			die(fmt.Errorf("%w (edit config)", err))
		}
		out.Playlist = name
	}
	writeActionOutput("play", req.output.JSON, req.output.Plain, out)
}

func resolveAirplayPlaylist(ctx context.Context, req playRequest) (string, error) {
	if req.target.kind == playIDTarget {
		return req.target.value, nil
	}
	query := req.target.value
	matches, err := searchPlaylists(ctx, query)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no playlists match %q (tip: run `homepodctl playlists --query %q`)", query, query)
	}
	if req.choose {
		selected, err := choosePlaylist(matches, !req.noInput)
		if err != nil {
			return "", err
		}
		if len(matches) > 1 {
			fmt.Fprintf(os.Stderr, "picked %q (%s)\n", selected.Name, selected.PersistentID)
		}
		return selected.PersistentID, nil
	}
	best, ok := music.PickBestPlaylist(query, matches)
	if !ok {
		return "", fmt.Errorf("no playlists match %q", query)
	}
	if len(matches) > 1 {
		fmt.Fprintf(os.Stderr, "picked %q (%s) (use --choose to select)\n", best.Name, best.PersistentID)
	}
	return best.PersistentID, nil
}
