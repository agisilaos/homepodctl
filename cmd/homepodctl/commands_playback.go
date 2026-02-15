package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func cmdVolume(ctx context.Context, cfg *native.Config, name string, args []string) {
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

	value := -1
	if v, ok, err := flags.intStrict("value"); err != nil {
		die(err)
	} else if ok {
		value = v
	} else if v, ok, err := flags.intStrict("volume"); err != nil {
		die(err)
	} else if ok {
		value = v
	}
	if value < 0 && len(positionals) > 0 {
		v, err := strconv.Atoi(positionals[0])
		if err != nil {
			die(usageErrf("usage: homepodctl %s <0-100> [<room> ...] [--backend airplay|native]", name))
		}
		value = v
		positionals = positionals[1:]
	}
	if value < 0 || value > 100 {
		die(usageErrf("volume must be 0-100"))
	}

	rooms := append([]string(nil), flags.strings("room")...)
	if len(rooms) == 0 && len(positionals) > 0 {
		rooms = append(rooms, positionals...)
	}
	if len(rooms) == 0 {
		rooms = append(rooms, cfg.Defaults.Rooms...)
	}

	switch backend {
	case "airplay":
		if len(rooms) == 0 {
			rooms = inferSelectedOutputs(ctx)
		}
		if len(rooms) == 0 {
			die(usageErrf("no rooms provided (pass room names, set defaults.rooms via `homepodctl config-init`, or select outputs in Music.app / `homepodctl out set`)"))
		}
		debugf("%s: backend=airplay value=%d rooms=%v", name, value, rooms)
		if opts.DryRun {
			writeActionOutput(name, opts.JSON, opts.Plain, actionOutput{
				DryRun:  true,
				Backend: backend,
				Rooms:   rooms,
			})
			return
		}
		if err := setVolumeForRooms(ctx, rooms, value); err != nil {
			die(err)
		}
		if np, err := getNowPlaying(ctx); err == nil {
			writeActionOutput(name, opts.JSON, opts.Plain, actionOutput{
				Backend:    backend,
				Rooms:      rooms,
				NowPlaying: &np,
			})
		} else {
			writeActionOutput(name, opts.JSON, opts.Plain, actionOutput{
				Backend: backend,
				Rooms:   rooms,
			})
		}
	case "native":
		debugf("%s: backend=native value=%d rooms=%v", name, value, rooms)
		if opts.DryRun {
			writeActionOutput(name, opts.JSON, opts.Plain, actionOutput{
				DryRun:  true,
				Backend: backend,
				Rooms:   rooms,
			})
			return
		}
		if err := runNativeVolumeShortcuts(ctx, cfg, rooms, value); err != nil {
			die(fmt.Errorf("%w (config-native volume is discrete)", err))
		}
		if np, err := getNowPlaying(ctx); err == nil {
			writeActionOutput(name, opts.JSON, opts.Plain, actionOutput{
				Backend:    backend,
				Rooms:      rooms,
				NowPlaying: &np,
			})
		} else {
			writeActionOutput(name, opts.JSON, opts.Plain, actionOutput{
				Backend: backend,
				Rooms:   rooms,
			})
		}
	default:
		die(usageErrf("unknown backend: %q", backend))
	}
}

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

func setVolumeForRooms(ctx context.Context, rooms []string, value int) error {
	for _, room := range rooms {
		if err := setDeviceVolume(ctx, room, value); err != nil {
			return err
		}
	}
	return nil
}

func resolveNativePlaylistShortcut(cfg *native.Config, room, playlist string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("native backend requires config")
	}
	if cfg.Native.Playlists == nil {
		return "", fmt.Errorf("no native mapping for room=%q playlist=%q", room, playlist)
	}
	m := cfg.Native.Playlists[room]
	shortcut := ""
	if m != nil {
		shortcut = m[playlist]
	}
	if strings.TrimSpace(shortcut) == "" {
		return "", fmt.Errorf("no native mapping for room=%q playlist=%q", room, playlist)
	}
	return shortcut, nil
}

func resolveNativeVolumeShortcut(cfg *native.Config, room string, value int) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("native backend requires config")
	}
	if cfg.Native.VolumeShortcuts == nil {
		return "", fmt.Errorf("no native volume mapping for room=%q value=%d", room, value)
	}
	m := cfg.Native.VolumeShortcuts[room]
	shortcut := ""
	if m != nil {
		shortcut = m[fmt.Sprint(value)]
	}
	if strings.TrimSpace(shortcut) == "" {
		return "", fmt.Errorf("no native volume mapping for room=%q value=%d", room, value)
	}
	return shortcut, nil
}

func runNativePlaylistShortcuts(ctx context.Context, cfg *native.Config, rooms []string, playlist string) error {
	for _, room := range rooms {
		shortcut, err := resolveNativePlaylistShortcut(cfg, room, playlist)
		if err != nil {
			return err
		}
		if err := runNativeShortcut(ctx, shortcut); err != nil {
			return err
		}
	}
	return nil
}

func runNativeVolumeShortcuts(ctx context.Context, cfg *native.Config, rooms []string, value int) error {
	for _, room := range rooms {
		shortcut, err := resolveNativeVolumeShortcut(cfg, room, value)
		if err != nil {
			return err
		}
		if err := runNativeShortcut(ctx, shortcut); err != nil {
			return err
		}
	}
	return nil
}

func validateAirplayVolumeSelection(volumeExplicit bool, volume int, rooms []string) error {
	if volumeExplicit && volume >= 0 && len(rooms) == 0 {
		return usageErrf("cannot set volume without rooms (pass --room <name> or select outputs first via `homepodctl out set`)")
	}
	return nil
}

func inferSelectedOutputs(ctx context.Context) []string {
	np, err := getNowPlaying(ctx)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var rooms []string
	for _, o := range np.Outputs {
		name := strings.TrimSpace(o.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		rooms = append(rooms, name)
	}
	return rooms
}
