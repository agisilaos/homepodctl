package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(val string) error {
	val = strings.TrimSpace(val)
	if val == "" {
		return nil
	}
	*s = append(*s, val)
	return nil
}

func usage() {
	fmt.Fprintf(os.Stderr, `homepodctl - control Apple Music + HomePods (macOS)

Usage:
  homepodctl version
  homepodctl devices [--json]
  homepodctl playlists [--query <substr>] [--limit N] [--json]
  homepodctl status [--json] [--watch <duration>]
  homepodctl aliases
  homepodctl run <alias>
  homepodctl pause
  homepodctl stop
  homepodctl next
  homepodctl prev
  homepodctl play --backend airplay --room <name> [--room <name> ...] (--playlist <name> | --playlist-id <id>) [--shuffle] [--volume 0-100]
  homepodctl play --backend native  --room <name> [--room <name> ...] --playlist <name>
  homepodctl volume --backend airplay --room <name> [--room <name> ...] --value 0-100
  homepodctl native-run --shortcut <name>
  homepodctl config-init

Notes:
  - backend=airplay uses Music.app AirPlay selection (Mac is the sender).
  - backend=native runs a Shortcut you map in the config file (HomePod plays natively if your Shortcut/Scene is set up that way).
`)
}

func main() {
	if runtime.GOOS != "darwin" {
		fmt.Fprintln(os.Stderr, "error: homepodctl only supports macOS (darwin)")
		os.Exit(1)
	}
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, cfgErr := native.LoadConfigOptional()
	if cfgErr != nil {
		die(cfgErr)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "version":
		fmt.Printf("homepodctl %s (%s) %s\n", version, commit, date)
	case "devices":
		fs := flag.NewFlagSet("devices", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		jsonOut := fs.Bool("json", false, "output JSON")
		if err := fs.Parse(args); err != nil {
			os.Exit(2)
		}

		devs, err := music.ListAirPlayDevices(ctx)
		if err != nil {
			die(err)
		}
		if *jsonOut {
			writeJSON(devs)
			return
		}
		for _, d := range devs {
			fmt.Printf("%s\tkind=%s\tavailable=%t\tselected=%t\tvolume=%d\tmac=%s\n", d.Name, d.Kind, d.Available, d.Selected, d.Volume, d.NetworkAddress)
		}
	case "playlists":
		fs := flag.NewFlagSet("playlists", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		query := fs.String("query", "", "filter playlists by substring (case-insensitive)")
		limit := fs.Int("limit", 50, "max playlists to return (0 = no limit)")
		jsonOut := fs.Bool("json", false, "output JSON")
		if err := fs.Parse(args); err != nil {
			os.Exit(2)
		}

		playlists, err := music.ListUserPlaylists(ctx, *query, *limit)
		if err != nil {
			die(err)
		}
		if *jsonOut {
			writeJSON(playlists)
			return
		}
		for _, p := range playlists {
			fmt.Printf("%s\t%s\n", p.PersistentID, p.Name)
		}
	case "status":
		fs := flag.NewFlagSet("status", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		jsonOut := fs.Bool("json", false, "output JSON")
		watch := fs.Duration("watch", 0, "poll interval (e.g. 1s); 0 prints once")
		if err := fs.Parse(args); err != nil {
			os.Exit(2)
		}
		printOnce := func() error {
			np, err := music.GetNowPlaying(ctx)
			if err != nil {
				return err
			}
			if *jsonOut {
				writeJSON(np)
				return nil
			}
			printNowPlaying(np)
			return nil
		}
		if *watch <= 0 {
			if err := printOnce(); err != nil {
				die(err)
			}
			return
		}
		ticker := time.NewTicker(*watch)
		defer ticker.Stop()
		for {
			if err := printOnce(); err != nil {
				die(err)
			}
			<-ticker.C
		}
	case "aliases":
		if len(cfg.Aliases) == 0 {
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
		for name, a := range cfg.Aliases {
			backend := a.Backend
			if backend == "" {
				backend = cfg.Defaults.Backend
			}
			rooms := a.Rooms
			if len(rooms) == 0 {
				rooms = cfg.Defaults.Rooms
			}
			target := a.Playlist
			if target == "" {
				target = a.PlaylistID
			}
			if a.Shortcut != "" {
				target = "shortcut:" + a.Shortcut
			}
			fmt.Printf("%s\tbackend=%s\trooms=%s\ttarget=%s\n", name, backend, strings.Join(rooms, ","), target)
		}
	case "run":
		if len(args) != 1 {
			die(fmt.Errorf("usage: homepodctl run <alias>"))
		}
		aliasName := args[0]
		a, ok := cfg.Aliases[aliasName]
		if !ok {
			path, _ := native.ConfigPath()
			if path != "" {
				if _, err := os.Stat(path); err != nil {
					die(fmt.Errorf("unknown alias: %q (no config found; run `homepodctl config-init` to create %s)", aliasName, path))
				}
			}
			die(fmt.Errorf("unknown alias: %q (run `homepodctl aliases` or edit config.json)", aliasName))
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
			if err := native.RunShortcut(ctx, a.Shortcut); err != nil {
				die(err)
			}
			return
		}
		switch backend {
		case "airplay":
			if len(rooms) == 0 {
				die(fmt.Errorf("alias %q requires rooms (set defaults.rooms or alias.rooms)", aliasName))
			}
			if err := music.SetCurrentAirPlayDevices(ctx, rooms); err != nil {
				die(err)
			}
			if a.Volume != nil {
				for _, r := range rooms {
					if err := music.SetAirPlayDeviceVolume(ctx, r, *a.Volume); err != nil {
						die(err)
					}
				}
			} else if cfg.Defaults.Volume != nil {
				for _, r := range rooms {
					if err := music.SetAirPlayDeviceVolume(ctx, r, *cfg.Defaults.Volume); err != nil {
						die(err)
					}
				}
			}
			if a.Shuffle != nil {
				if err := music.SetShuffleEnabled(ctx, *a.Shuffle); err != nil {
					die(err)
				}
			}
			if a.PlaylistID != "" || a.Playlist != "" {
				id := a.PlaylistID
				if id == "" {
					var err error
					id, err = music.FindUserPlaylistPersistentIDByName(ctx, a.Playlist)
					if err != nil {
						die(err)
					}
				}
				if err := music.PlayUserPlaylistByPersistentID(ctx, id); err != nil {
					die(err)
				}
			}
			np, err := music.GetNowPlaying(ctx)
			if err == nil {
				printNowPlaying(np)
			}
		case "native":
			if len(rooms) == 0 {
				die(fmt.Errorf("alias %q requires rooms (set defaults.rooms or alias.rooms)", aliasName))
			}
			if a.Playlist == "" && a.PlaylistID == "" {
				die(fmt.Errorf("alias %q requires playlist (native mapping is per room+playlist)", aliasName))
			}
			name := a.Playlist
			if name == "" {
				var err error
				name, err = music.FindUserPlaylistNameByPersistentID(ctx, a.PlaylistID)
				if err != nil {
					die(err)
				}
			}
			for _, room := range rooms {
				shortcutName := cfg.Native.Playlists[room][name]
				if strings.TrimSpace(shortcutName) == "" {
					die(fmt.Errorf("no native mapping for room=%q playlist=%q (edit config)", room, name))
				}
				if err := native.RunShortcut(ctx, shortcutName); err != nil {
					die(err)
				}
			}
		default:
			die(fmt.Errorf("unknown backend in alias %q: %q", aliasName, backend))
		}
	case "pause":
		if err := music.Pause(ctx); err != nil {
			die(err)
		}
		if np, err := music.GetNowPlaying(ctx); err == nil {
			printNowPlaying(np)
		}
	case "stop":
		if err := music.Stop(ctx); err != nil {
			die(err)
		}
		if np, err := music.GetNowPlaying(ctx); err == nil {
			printNowPlaying(np)
		}
	case "next":
		if err := music.NextTrack(ctx); err != nil {
			die(err)
		}
		if np, err := music.GetNowPlaying(ctx); err == nil {
			printNowPlaying(np)
		}
	case "prev":
		if err := music.PreviousTrack(ctx); err != nil {
			die(err)
		}
		if np, err := music.GetNowPlaying(ctx); err == nil {
			printNowPlaying(np)
		}
	case "play":
		fs := flag.NewFlagSet("play", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		backend := fs.String("backend", "", "airplay|native")
		var rooms stringSliceFlag
		fs.Var(&rooms, "room", "room/AirPlay device name (repeatable)")
		playlistName := fs.String("playlist", "", "user playlist name")
		playlistID := fs.String("playlist-id", "", "user playlist persistent ID")
		shuffle := fs.Bool("shuffle", false, "enable shuffle (airplay only)")
		volume := fs.Int("volume", -1, "set volume (0-100) for selected room(s)")
		choose := fs.Bool("choose", false, "interactively choose playlist when multiple match (airplay only)")
		if err := fs.Parse(args); err != nil {
			os.Exit(2)
		}

		b := strings.TrimSpace(*backend)
		if b == "" {
			b = cfg.Defaults.Backend
		}
		if len(rooms) == 0 {
			rooms = append(rooms, cfg.Defaults.Rooms...)
		}
		if *volume < 0 && cfg.Defaults.Volume != nil {
			*volume = *cfg.Defaults.Volume
		}

		switch b {
		case "airplay":
			if len(rooms) == 0 {
				die(fmt.Errorf("--room is required for backend=airplay"))
			}
			id := strings.TrimSpace(*playlistID)
			if id == "" {
				if strings.TrimSpace(*playlistName) == "" {
					die(fmt.Errorf("either --playlist or --playlist-id is required"))
				}
				if *choose {
					matches, err := music.SearchUserPlaylists(ctx, *playlistName)
					if err != nil {
						die(err)
					}
					if len(matches) == 0 {
						die(fmt.Errorf("no playlists match %q", *playlistName))
					}
					selected, err := choosePlaylist(matches)
					if err != nil {
						die(err)
					}
					id = selected.PersistentID
				} else {
					var err error
					id, err = music.FindUserPlaylistPersistentIDByName(ctx, *playlistName)
					if err != nil {
						die(err)
					}
				}
			}

			if err := music.SetCurrentAirPlayDevices(ctx, rooms); err != nil {
				die(err)
			}
			if *volume >= 0 {
				for _, room := range rooms {
					if err := music.SetAirPlayDeviceVolume(ctx, room, *volume); err != nil {
						die(err)
					}
				}
			}
			if err := music.SetShuffleEnabled(ctx, *shuffle); err != nil {
				die(err)
			}
			if err := music.PlayUserPlaylistByPersistentID(ctx, id); err != nil {
				die(err)
			}
			np, err := music.GetNowPlaying(ctx)
			if err == nil {
				printNowPlaying(np)
			}
		case "native":
			if len(rooms) == 0 {
				die(fmt.Errorf("--room is required for backend=native"))
			}
			if strings.TrimSpace(*playlistName) == "" && strings.TrimSpace(*playlistID) == "" {
				die(fmt.Errorf("--playlist is required for backend=native (names are used for config mapping)"))
			}

			name := strings.TrimSpace(*playlistName)
			if name == "" {
				var err error
				name, err = music.FindUserPlaylistNameByPersistentID(ctx, *playlistID)
				if err != nil {
					die(err)
				}
			}

			for _, room := range rooms {
				shortcutName, ok := cfg.Native.Playlists[room][name]
				if !ok || strings.TrimSpace(shortcutName) == "" {
					die(fmt.Errorf("no native mapping for room=%q playlist=%q (edit config-init output)", room, name))
				}
				if err := native.RunShortcut(ctx, shortcutName); err != nil {
					die(err)
				}
			}
		default:
			die(fmt.Errorf("unknown backend: %q", b))
		}
	case "volume":
		fs := flag.NewFlagSet("volume", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		backend := fs.String("backend", "", "airplay|native")
		var rooms stringSliceFlag
		fs.Var(&rooms, "room", "room/AirPlay device name (repeatable)")
		value := fs.Int("value", -1, "volume (0-100)")
		if err := fs.Parse(args); err != nil {
			os.Exit(2)
		}

		if *value < 0 || *value > 100 {
			die(fmt.Errorf("--value must be 0-100"))
		}

		b := strings.TrimSpace(*backend)
		if b == "" {
			b = cfg.Defaults.Backend
		}
		if len(rooms) == 0 {
			rooms = append(rooms, cfg.Defaults.Rooms...)
		}

		switch b {
		case "airplay":
			if len(rooms) == 0 {
				die(fmt.Errorf("--room is required for backend=airplay"))
			}
			for _, room := range rooms {
				if err := music.SetAirPlayDeviceVolume(ctx, room, *value); err != nil {
					die(err)
				}
			}
			if np, err := music.GetNowPlaying(ctx); err == nil {
				printNowPlaying(np)
			}
		case "native":
			for _, room := range rooms {
				m := cfg.Native.VolumeShortcuts[room]
				shortcutName := ""
				if m != nil {
					shortcutName = m[fmt.Sprint(*value)]
				}
				if strings.TrimSpace(shortcutName) == "" {
					die(fmt.Errorf("no native volume mapping for room=%q value=%d (config-native volume is discrete)", room, *value))
				}
				if err := native.RunShortcut(ctx, shortcutName); err != nil {
					die(err)
				}
			}
			if np, err := music.GetNowPlaying(ctx); err == nil {
				printNowPlaying(np)
			}
		default:
			die(fmt.Errorf("unknown backend: %q", b))
		}
	case "native-run":
		fs := flag.NewFlagSet("native-run", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		shortcutName := fs.String("shortcut", "", "Shortcut name to run")
		if err := fs.Parse(args); err != nil {
			os.Exit(2)
		}

		if strings.TrimSpace(*shortcutName) == "" {
			die(fmt.Errorf("--shortcut is required"))
		}
		if err := native.RunShortcut(ctx, *shortcutName); err != nil {
			die(err)
		}
	case "config-init":
		path, err := native.InitConfig()
		if err != nil {
			die(err)
		}
		fmt.Printf("Wrote %s\n", path)
	default:
		usage()
		os.Exit(2)
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func writeJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func printNowPlaying(np music.NowPlaying) {
	pos := formatClock(np.PlayerPositionS)
	dur := ""
	if np.Track.DurationS > 0 {
		dur = "/" + formatClock(np.Track.DurationS)
	}
	sh := "off"
	if np.ShuffleEnabled {
		sh = "on"
	}
	fmt.Printf("state=%s pos=%s%s shuffle=%s repeat=%s\n", np.PlayerState, pos, dur, sh, np.SongRepeat)
	if np.PlaylistName != "" {
		fmt.Printf("playlist=%q\n", np.PlaylistName)
	}
	if np.Track.Name != "" {
		fmt.Printf("track=%q artist=%q album=%q\n", np.Track.Name, np.Track.Artist, np.Track.Album)
	}
	if len(np.Outputs) > 0 {
		var parts []string
		for _, o := range np.Outputs {
			parts = append(parts, fmt.Sprintf("%s(vol=%d)", o.Name, o.Volume))
		}
		fmt.Printf("outputs=%s\n", strings.Join(parts, ", "))
	}
}

func formatClock(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	s := int(seconds + 0.5)
	h := s / 3600
	m := (s % 3600) / 60
	sec := s % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%d:%02d", m, sec)
}

func choosePlaylist(matches []music.UserPlaylist) (music.UserPlaylist, error) {
	if len(matches) == 1 {
		return matches[0], nil
	}
	fmt.Fprintln(os.Stderr, "Multiple playlists match. Choose one:")
	for i, p := range matches {
		fmt.Fprintf(os.Stderr, "  %d) %s\t%s\n", i+1, p.PersistentID, p.Name)
	}
	fmt.Fprint(os.Stderr, "Enter number: ")
	var n int
	if _, err := fmt.Fscan(os.Stdin, &n); err != nil {
		return music.UserPlaylist{}, fmt.Errorf("read selection: %w", err)
	}
	if n < 1 || n > len(matches) {
		return music.UserPlaylist{}, fmt.Errorf("invalid selection %d", n)
	}
	return matches[n-1], nil
}
