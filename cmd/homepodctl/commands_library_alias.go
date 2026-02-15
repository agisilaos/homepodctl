package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func cmdDevices(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("devices", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "output JSON")
	includeNetwork := fs.Bool("include-network", false, "include network address (MAC) in JSON output")
	plain := fs.Bool("plain", false, "plain (no header) output")
	if err := fs.Parse(args); err != nil {
		exitCode(exitUsage)
	}

	devs, err := music.ListAirPlayDevices(ctx)
	if err != nil {
		die(err)
	}
	if *jsonOut {
		if !*includeNetwork {
			for i := range devs {
				devs[i].NetworkAddress = ""
			}
		}
		writeJSON(devs)
		return
	}
	printDevicesTable(os.Stdout, devs, *plain)
}

func cmdPlaylists(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("playlists", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	query := fs.String("query", "", "filter playlists by substring (case-insensitive)")
	limit := fs.Int("limit", 50, "max playlists to return (0 = no limit)")
	jsonOut := fs.Bool("json", false, "output JSON")
	plain := fs.Bool("plain", false, "plain (no header) output")
	if err := fs.Parse(args); err != nil {
		exitCode(exitUsage)
	}

	playlists, err := music.ListUserPlaylists(ctx, *query, *limit)
	if err != nil {
		die(err)
	}
	if *jsonOut {
		writeJSON(playlists)
		return
	}
	if !*plain {
		fmt.Println("PERSISTENT_ID\tNAME")
	}
	for _, p := range playlists {
		fmt.Printf("%s\t%s\n", p.PersistentID, p.Name)
	}
}

func cmdAliases(cfg *native.Config, args []string) {
	fs := flag.NewFlagSet("aliases", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "output JSON")
	plain := fs.Bool("plain", false, "plain (no header) output")
	if err := fs.Parse(args); err != nil {
		exitCode(exitUsage)
	}
	rows := buildAliasRows(cfg)
	if len(rows) == 0 {
		if *jsonOut {
			writeJSON([]aliasRow{})
			return
		}
		path, _ := native.ConfigPath()
		if path != "" {
			if _, err := os.Stat(path); err != nil {
				fmt.Printf("No aliases configured. Run `homepodctl config-init` to create %s\n", path)
				return
			}
		}
		fmt.Println("No aliases configured in config.json")
		return
	}
	if *jsonOut {
		writeJSON(rows)
		return
	}
	printAliasesTable(os.Stdout, rows, *plain)
}

func cmdRun(ctx context.Context, cfg *native.Config, args []string) {
	flags, positionals, err := parseArgs(args)
	if err != nil {
		die(err)
	}
	if len(positionals) != 1 {
		die(usageErrf("usage: homepodctl run <alias>"))
	}
	opts, err := parseOutputOptions(flags)
	if err != nil {
		die(err)
	}
	aliasName := positionals[0]
	a, ok := cfg.Aliases[aliasName]
	if !ok {
		path, _ := native.ConfigPath()
		if path != "" {
			if _, err := os.Stat(path); err != nil {
				die(usageErrf("unknown alias: %q (no config found; run `homepodctl config-init` to create %s)", aliasName, path))
			}
		}
		die(usageErrf("unknown alias: %q (run `homepodctl aliases` or edit config.json)", aliasName))
	}
	backend := a.Backend
	if backend == "" {
		backend = cfg.Defaults.Backend
	}
	rooms := a.Rooms
	if len(rooms) == 0 {
		rooms = cfg.Defaults.Rooms
	}
	if a.Shortcut != "" {
		if !opts.DryRun {
			if err := native.RunShortcut(ctx, a.Shortcut); err != nil {
				die(err)
			}
		}
		writeActionOutput("run", opts.JSON, opts.Plain, actionOutput{
			DryRun:   opts.DryRun,
			Backend:  backend,
			Rooms:    rooms,
			Shortcut: a.Shortcut,
		})
		return
	}
	switch backend {
	case "airplay":
		if len(rooms) == 0 {
			die(fmt.Errorf("alias %q requires rooms (set defaults.rooms or alias.rooms)", aliasName))
		}
		if opts.DryRun {
			writeActionOutput("run", opts.JSON, opts.Plain, actionOutput{
				DryRun:     true,
				Backend:    backend,
				Rooms:      rooms,
				Playlist:   a.Playlist,
				PlaylistID: a.PlaylistID,
			})
			return
		}
		if err := setCurrentOutputs(ctx, rooms); err != nil {
			die(err)
		}
		if a.Volume != nil {
			if err := setVolumeForRooms(ctx, rooms, *a.Volume); err != nil {
				die(err)
			}
		} else if cfg.Defaults.Volume != nil {
			if err := setVolumeForRooms(ctx, rooms, *cfg.Defaults.Volume); err != nil {
				die(err)
			}
		}
		if a.Shuffle != nil {
			if err := setShuffle(ctx, *a.Shuffle); err != nil {
				die(err)
			}
		}
		if a.PlaylistID != "" || a.Playlist != "" {
			id := a.PlaylistID
			if id == "" {
				matches, err := searchPlaylists(ctx, a.Playlist)
				if err != nil {
					die(err)
				}
				if len(matches) == 0 {
					die(fmt.Errorf("alias %q playlist %q not found (tip: set playlistId to pin an exact playlist)", aliasName, a.Playlist))
				}
				best, _ := music.PickBestPlaylist(a.Playlist, matches)
				id = best.PersistentID
				if len(matches) > 1 {
					fmt.Fprintf(os.Stderr, "picked %q (%s) for alias %q (set playlistId to pin)\n", best.Name, best.PersistentID, aliasName)
				}
			}
			if err := playPlaylistByID(ctx, id); err != nil {
				die(err)
			}
		}
		np, err := getNowPlaying(ctx)
		if err == nil {
			writeActionOutput("run", opts.JSON, opts.Plain, actionOutput{
				Backend:    backend,
				Rooms:      rooms,
				PlaylistID: a.PlaylistID,
				NowPlaying: &np,
			})
		} else {
			writeActionOutput("run", opts.JSON, opts.Plain, actionOutput{
				Backend:    backend,
				Rooms:      rooms,
				PlaylistID: a.PlaylistID,
			})
		}
	case "native":
		if len(rooms) == 0 {
			die(fmt.Errorf("alias %q requires rooms (set defaults.rooms or alias.rooms)", aliasName))
		}
		if a.Playlist == "" && a.PlaylistID == "" {
			die(fmt.Errorf("alias %q requires playlist (native mapping is per room+playlist)", aliasName))
		}
		name := a.Playlist
		if opts.DryRun {
			if name == "" {
				name = a.PlaylistID
			}
			writeActionOutput("run", opts.JSON, opts.Plain, actionOutput{
				DryRun:   true,
				Backend:  backend,
				Rooms:    rooms,
				Playlist: name,
			})
			return
		}
		if name == "" {
			name, err = findPlaylistNameByID(ctx, a.PlaylistID)
			if err != nil {
				die(err)
			}
		}
		if err := runNativePlaylistShortcuts(ctx, cfg, rooms, name); err != nil {
			die(fmt.Errorf("%w (edit config)", err))
		}
		writeActionOutput("run", opts.JSON, opts.Plain, actionOutput{
			DryRun:   opts.DryRun,
			Backend:  backend,
			Rooms:    rooms,
			Playlist: name,
		})
	default:
		die(fmt.Errorf("unknown backend in alias %q: %q", aliasName, backend))
	}
}

func cmdNativeRun(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("native-run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	shortcutName := fs.String("shortcut", "", "Shortcut name to run")
	jsonOut := fs.Bool("json", false, "output JSON")
	dryRun := fs.Bool("dry-run", false, "resolve and print action without running")
	if err := fs.Parse(args); err != nil {
		exitCode(exitUsage)
	}

	if strings.TrimSpace(*shortcutName) == "" {
		die(usageErrf("--shortcut is required"))
	}
	if !*dryRun {
		if err := runNativeShortcut(ctx, *shortcutName); err != nil {
			die(err)
		}
	}
	if *jsonOut {
		writeJSON(actionResult{
			OK:       true,
			Action:   "native-run",
			DryRun:   *dryRun,
			Shortcut: *shortcutName,
		})
	} else if *dryRun {
		fmt.Printf("dry-run action=native-run shortcut=%q\n", *shortcutName)
	}
}

func cmdConfigInit() {
	path, err := native.InitConfig()
	if err != nil {
		die(err)
	}
	fmt.Printf("Wrote %s\n", path)
}
