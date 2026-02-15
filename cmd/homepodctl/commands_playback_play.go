package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func cmdPlay(ctx context.Context, cfg *native.Config, args []string) {
	flags, positionals, err := parseArgs(args)
	if err != nil {
		die(err)
	}
	opts, err := parseOutputOptions(flags)
	if err != nil {
		die(err)
	}

	backend := strings.TrimSpace(flags.string("backend"))
	if backend == "" {
		backend = cfg.Defaults.Backend
	}
	rooms := append([]string(nil), flags.strings("room")...)
	if len(rooms) == 0 {
		rooms = append(rooms, cfg.Defaults.Rooms...)
	}

	volume := -1
	volumeExplicit := false
	if v, ok, err := flags.intStrict("volume"); err != nil {
		die(err)
	} else if ok {
		volume = v
		volumeExplicit = true
	}
	if volume < 0 && cfg.Defaults.Volume != nil {
		volume = *cfg.Defaults.Volume
	}
	shuffle, shuffleSet, err := flags.boolStrict("shuffle")
	if err != nil {
		die(err)
	}
	if !shuffleSet {
		shuffle = cfg.Defaults.Shuffle
	}
	choose, _, err := flags.boolStrict("choose")
	if err != nil {
		die(err)
	}

	playlistID := strings.TrimSpace(flags.string("playlist-id"))
	playlistName := strings.TrimSpace(flags.string("playlist"))
	query := playlistName
	if query == "" && playlistID == "" && len(positionals) > 0 {
		query = strings.Join(positionals, " ")
	}

	switch backend {
	case "airplay":
		if len(rooms) == 0 {
			rooms = inferSelectedOutputs(ctx)
		}
		if opts.DryRun {
			if strings.TrimSpace(query) == "" && strings.TrimSpace(playlistID) == "" {
				die(usageErrf("playlist is required (pass <playlist-query>, --playlist, or --playlist-id)"))
			}
			writeActionOutput("play", opts.JSON, opts.Plain, actionOutput{
				DryRun:     true,
				Backend:    backend,
				Rooms:      rooms,
				Playlist:   query,
				PlaylistID: playlistID,
			})
			return
		}

		id := playlistID
		if id == "" {
			if strings.TrimSpace(query) == "" {
				die(usageErrf("playlist is required (pass <playlist-query>, --playlist, or --playlist-id)"))
			}
			matches, err := searchPlaylists(ctx, query)
			if err != nil {
				die(err)
			}
			if len(matches) == 0 {
				die(fmt.Errorf("no playlists match %q (tip: run `homepodctl playlists --query %q`)", query, query))
			}
			if choose {
				selected, err := choosePlaylist(matches)
				if err != nil {
					die(err)
				}
				id = selected.PersistentID
				if len(matches) > 1 {
					fmt.Fprintf(os.Stderr, "picked %q (%s)\n", selected.Name, selected.PersistentID)
				}
			} else {
				best, ok := music.PickBestPlaylist(query, matches)
				if !ok {
					die(fmt.Errorf("no playlists match %q", query))
				}
				id = best.PersistentID
				if len(matches) > 1 {
					fmt.Fprintf(os.Stderr, "picked %q (%s) (use --choose to select)\n", best.Name, best.PersistentID)
				}
			}
		}
		debugf("play: backend=airplay rooms=%v playlist_id=%q query=%q shuffle=%t volume=%d explicit_volume=%t choose=%t", rooms, id, query, shuffle, volume, volumeExplicit, choose)

		// If we have rooms, select outputs first. If we don't, keep Music.app's current outputs.
		if len(rooms) > 0 {
			if err := setCurrentOutputs(ctx, rooms); err != nil {
				die(err)
			}
		}
		if err := validateAirplayVolumeSelection(volumeExplicit, volume, rooms); err != nil {
			die(err)
		}
		if volume >= 0 && len(rooms) > 0 {
			if err := setVolumeForRooms(ctx, rooms, volume); err != nil {
				die(err)
			}
		}
		if err := setShuffle(ctx, shuffle); err != nil {
			die(err)
		}
		if err := playPlaylistByID(ctx, id); err != nil {
			die(err)
		}
		if np, err := getNowPlaying(ctx); err == nil {
			writeActionOutput("play", opts.JSON, opts.Plain, actionOutput{
				Backend:    backend,
				Rooms:      rooms,
				Playlist:   query,
				PlaylistID: id,
				NowPlaying: &np,
			})
		} else {
			writeActionOutput("play", opts.JSON, opts.Plain, actionOutput{
				Backend:    backend,
				Rooms:      rooms,
				Playlist:   query,
				PlaylistID: id,
			})
		}
	case "native":
		if len(rooms) == 0 {
			die(usageErrf("no rooms provided (pass --room <name> ... or set defaults.rooms via `homepodctl config-init`)"))
		}
		if strings.TrimSpace(query) == "" && playlistID == "" {
			die(usageErrf("playlist is required (pass <playlist-query>, --playlist, or --playlist-id)"))
		}
		if opts.DryRun {
			name := strings.TrimSpace(query)
			if name == "" {
				name = playlistID
			}
			writeActionOutput("play", opts.JSON, opts.Plain, actionOutput{
				DryRun:   true,
				Backend:  backend,
				Rooms:    rooms,
				Playlist: name,
			})
			return
		}
		name := strings.TrimSpace(query)
		if name == "" {
			var err error
			name, err = findPlaylistNameByID(ctx, playlistID)
			if err != nil {
				die(err)
			}
		}
		debugf("play: backend=native rooms=%v playlist=%q playlist_id=%q", rooms, name, playlistID)
		if err := runNativePlaylistShortcuts(ctx, cfg, rooms, name); err != nil {
			die(fmt.Errorf("%w (edit config)", err))
		}
		writeActionOutput("play", opts.JSON, opts.Plain, actionOutput{
			Backend:  backend,
			Rooms:    rooms,
			Playlist: name,
		})
	default:
		die(usageErrf("unknown backend: %q", backend))
	}
}
