package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func usage() {
	fmt.Fprintf(os.Stderr, `homepodctl - control Apple Music + HomePods (macOS)

Usage:
  homepodctl version
  homepodctl devices [--json] [--plain] [--include-network]
  homepodctl out list [--json] [--plain] [--include-network]
  homepodctl out set [<room> ...] [--backend airplay]
  homepodctl playlists [--query <substr>] [--limit N] [--json]
  homepodctl status [--json] [--watch <duration>]
  homepodctl now [--json] [--watch <duration>]
  homepodctl aliases
  homepodctl run <alias>
  homepodctl pause
  homepodctl stop
  homepodctl next
  homepodctl prev
  homepodctl play <playlist-query> [--backend airplay|native] [--room <name> ...] [--shuffle] [--volume 0-100] [--choose]
  homepodctl play --playlist <name> | --playlist-id <id> [--backend airplay|native] [--room <name> ...] [--shuffle] [--volume 0-100] [--choose]
  homepodctl volume <0-100> [<room> ...] [--backend airplay|native]
  homepodctl vol <0-100> [<room> ...] [--backend airplay|native]
  homepodctl native-run --shortcut <name>
  homepodctl config-init

Notes:
  - backend=airplay uses Music.app AirPlay selection (Mac is the sender).
  - backend=native runs a Shortcut you map in the config file (HomePod plays natively if your Shortcut/Scene is set up that way).
  - defaults come from config.json (run homepodctl config-init); commands use defaults when flags/args are omitted.
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
		includeNetwork := fs.Bool("include-network", false, "include network address (MAC) in JSON output")
		plain := fs.Bool("plain", false, "plain (no header) output")
		if err := fs.Parse(args); err != nil {
			os.Exit(2)
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
		cmdStatus(ctx, args)
	case "now":
		cmdStatus(ctx, args)
	case "out":
		cmdOut(ctx, cfg, args)
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
					matches, err := music.SearchUserPlaylists(ctx, a.Playlist)
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
		cmdPlay(ctx, cfg, args)
	case "volume":
		cmdVolume(ctx, cfg, "volume", args)
	case "vol":
		cmdVolume(ctx, cfg, "vol", args)
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

func printDevicesTable(w io.Writer, devs []music.AirPlayDevice, plain bool) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !plain {
		fmt.Fprintln(tw, "NAME\tKIND\tAVAILABLE\tSELECTED\tVOLUME")
	}
	for _, d := range devs {
		kind := d.Kind
		if kind == "" {
			kind = "unknown"
		}
		fmt.Fprintf(tw, "%s\t%s\t%t\t%t\t%d\n", d.Name, kind, d.Available, d.Selected, d.Volume)
	}
	_ = tw.Flush()
}

func cmdStatus(ctx context.Context, args []string) {
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
}

func cmdOut(ctx context.Context, cfg *native.Config, args []string) {
	if len(args) < 1 {
		die(fmt.Errorf("usage: homepodctl out <list|set> [args]"))
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("out list", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		jsonOut := fs.Bool("json", false, "output JSON")
		includeNetwork := fs.Bool("include-network", false, "include network address (MAC) in JSON output")
		plain := fs.Bool("plain", false, "plain (no header) output")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
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
	case "set":
		flags, positionals, err := parseArgs(args[1:])
		if err != nil {
			die(err)
		}
		backend := strings.TrimSpace(flags.string("backend"))
		if backend == "" {
			backend = cfg.Defaults.Backend
		}
		if backend != "airplay" {
			die(fmt.Errorf("out set only supports backend=airplay (got %q)", backend))
		}
		rooms := positionals
		if len(rooms) == 0 {
			rooms = append(rooms, cfg.Defaults.Rooms...)
		}
		if len(rooms) == 0 {
			die(fmt.Errorf("no rooms provided and defaults.rooms is empty"))
		}
		if err := music.SetCurrentAirPlayDevices(ctx, rooms); err != nil {
			die(err)
		}
		if np, err := music.GetNowPlaying(ctx); err == nil {
			printNowPlaying(np)
		}
	default:
		die(fmt.Errorf("usage: homepodctl out <list|set> [args]"))
	}
}

func cmdVolume(ctx context.Context, cfg *native.Config, name string, args []string) {
	flags, positionals, err := parseArgs(args)
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
			die(fmt.Errorf("usage: homepodctl %s <0-100> [<room> ...] [--backend airplay|native]", name))
		}
		value = v
		positionals = positionals[1:]
	}
	if value < 0 || value > 100 {
		die(fmt.Errorf("volume must be 0-100"))
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
			die(fmt.Errorf("no rooms provided and defaults.rooms is empty"))
		}
		for _, room := range rooms {
			if err := music.SetAirPlayDeviceVolume(ctx, room, value); err != nil {
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
				shortcutName = m[fmt.Sprint(value)]
			}
			if strings.TrimSpace(shortcutName) == "" {
				die(fmt.Errorf("no native volume mapping for room=%q value=%d (config-native volume is discrete)", room, value))
			}
			if err := native.RunShortcut(ctx, shortcutName); err != nil {
				die(err)
			}
		}
		if np, err := music.GetNowPlaying(ctx); err == nil {
			printNowPlaying(np)
		}
	default:
		die(fmt.Errorf("unknown backend: %q", backend))
	}
}

func cmdPlay(ctx context.Context, cfg *native.Config, args []string) {
	flags, positionals, err := parseArgs(args)
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
	if v, ok, err := flags.intStrict("volume"); err != nil {
		die(err)
	} else if ok {
		volume = v
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
			die(fmt.Errorf("no rooms provided and defaults.rooms is empty"))
		}

		id := playlistID
		if id == "" {
			if strings.TrimSpace(query) == "" {
				die(fmt.Errorf("playlist is required (pass <playlist-query>, --playlist, or --playlist-id)"))
			}
			matches, err := music.SearchUserPlaylists(ctx, query)
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

		if err := music.SetCurrentAirPlayDevices(ctx, rooms); err != nil {
			die(err)
		}
		if volume >= 0 {
			for _, room := range rooms {
				if err := music.SetAirPlayDeviceVolume(ctx, room, volume); err != nil {
					die(err)
				}
			}
		}
		if err := music.SetShuffleEnabled(ctx, shuffle); err != nil {
			die(err)
		}
		if err := music.PlayUserPlaylistByPersistentID(ctx, id); err != nil {
			die(err)
		}
		if np, err := music.GetNowPlaying(ctx); err == nil {
			printNowPlaying(np)
		}
	case "native":
		if len(rooms) == 0 {
			die(fmt.Errorf("no rooms provided and defaults.rooms is empty"))
		}
		if strings.TrimSpace(query) == "" && playlistID == "" {
			die(fmt.Errorf("playlist is required (pass <playlist-query>, --playlist, or --playlist-id)"))
		}
		name := strings.TrimSpace(query)
		if name == "" {
			var err error
			name, err = music.FindUserPlaylistNameByPersistentID(ctx, playlistID)
			if err != nil {
				die(err)
			}
		}
		for _, room := range rooms {
			shortcutName, ok := cfg.Native.Playlists[room][name]
			if !ok || strings.TrimSpace(shortcutName) == "" {
				die(fmt.Errorf("no native mapping for room=%q playlist=%q (edit config)", room, name))
			}
			if err := native.RunShortcut(ctx, shortcutName); err != nil {
				die(err)
			}
		}
	default:
		die(fmt.Errorf("unknown backend: %q", backend))
	}
}

type parsedArgs struct {
	kv map[string][]string
}

func (p parsedArgs) has(key string) bool {
	v := p.strings(key)
	return len(v) > 0
}

func (p parsedArgs) strings(key string) []string {
	if p.kv == nil {
		return nil
	}
	return p.kv[key]
}

func (p parsedArgs) string(key string) string {
	v := p.strings(key)
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1]
}

func (p parsedArgs) int(key string, def int) int {
	s := strings.TrimSpace(p.string(key))
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func (p parsedArgs) intStrict(key string) (int, bool, error) {
	if !p.has(key) {
		return 0, false, nil
	}
	s := strings.TrimSpace(p.string(key))
	if s == "" {
		return 0, true, fmt.Errorf("--%s requires a value", key)
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, true, fmt.Errorf("invalid --%s %q", key, s)
	}
	return n, true, nil
}

func (p parsedArgs) bool(key string) (bool, bool) {
	v := p.strings(key)
	if len(v) == 0 {
		return false, false
	}
	s := strings.TrimSpace(v[len(v)-1])
	if s == "" {
		return true, true
	}
	switch strings.ToLower(s) {
	case "true", "1", "yes", "y", "on":
		return true, true
	case "false", "0", "no", "n", "off":
		return false, true
	default:
		return false, false
	}
}

func (p parsedArgs) boolStrict(key string) (bool, bool, error) {
	if !p.has(key) {
		return false, false, nil
	}
	b, ok := p.bool(key)
	if ok {
		return b, true, nil
	}
	return false, true, fmt.Errorf("invalid --%s %q (expected true/false)", key, p.string(key))
}

func (p parsedArgs) boolDefault(key string, def bool) bool {
	b, ok := p.bool(key)
	if !ok {
		return def
	}
	return b
}

func parseArgs(args []string) (parsedArgs, []string, error) {
	out := parsedArgs{kv: map[string][]string{}}
	var positionals []string

	push := func(k, v string) {
		out.kv[k] = append(out.kv[k], v)
	}

	isBoolWord := func(s string) bool {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true", "false", "1", "0", "yes", "no", "y", "n", "on", "off":
			return true
		default:
			return false
		}
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if a == "-h" || a == "--help" {
			usage()
			os.Exit(0)
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positionals = append(positionals, a)
			continue
		}

		if strings.HasPrefix(a, "--") {
			key := strings.TrimPrefix(a, "--")
			val := ""
			if eq := strings.IndexByte(key, '='); eq >= 0 {
				val = key[eq+1:]
				key = key[:eq]
			}

			switch key {
			case "backend", "playlist", "playlist-id", "volume", "value", "room":
				if key == "room" {
					if val == "" {
						if i+1 >= len(args) {
							return parsedArgs{}, nil, fmt.Errorf("--room requires a value")
						}
						i++
						val = args[i]
					}
					push("room", val)
					continue
				}
				if val == "" {
					if i+1 >= len(args) {
						return parsedArgs{}, nil, fmt.Errorf("--%s requires a value", key)
					}
					i++
					val = args[i]
				}
				push(key, val)
			case "shuffle", "choose":
				if val == "" && i+1 < len(args) && isBoolWord(args[i+1]) {
					i++
					val = args[i]
				}
				if val == "" {
					val = "true"
				}
				push(key, val)
			default:
				return parsedArgs{}, nil, fmt.Errorf("unknown flag: %s", a)
			}
			continue
		}

		return parsedArgs{}, nil, fmt.Errorf("unknown flag: %s", a)
	}
	return out, positionals, nil
}
