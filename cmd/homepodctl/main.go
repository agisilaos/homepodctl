package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
	"gopkg.in/yaml.v3"
)

var (
	version       = "dev"
	commit        = "none"
	date          = "unknown"
	getNowPlaying = music.GetNowPlaying
	verbose       bool
)

const (
	exitGeneric = 1
	exitUsage   = 2
	exitConfig  = 3
	exitBackend = 4
)

func usage() {
	fmt.Fprintf(os.Stderr, `homepodctl - control Apple Music + HomePods (macOS)

Usage:
  homepodctl [--verbose] --help
  homepodctl [--verbose] <command> [args]
  homepodctl --help
  homepodctl help [<command>]
  homepodctl version
  homepodctl automation <run|validate|plan|init> [args]
  homepodctl completion <bash|zsh|fish>
  homepodctl doctor [--json] [--plain]
  homepodctl devices [--json] [--plain] [--include-network]
  homepodctl out list [--json] [--plain] [--include-network]
  homepodctl out set [<room> ...] [--backend airplay] [--json] [--plain] [--dry-run]
  homepodctl playlists [--query <substr>] [--limit N] [--json] [--plain]
  homepodctl status [--json] [--plain] [--watch <duration>]
  homepodctl now [--json] [--plain] [--watch <duration>]
  homepodctl aliases [--json] [--plain]
  homepodctl run <alias> [--json] [--plain] [--dry-run]
  homepodctl pause [--json] [--plain]
  homepodctl stop [--json] [--plain]
  homepodctl next [--json] [--plain]
  homepodctl prev [--json] [--plain]
  homepodctl play <playlist-query> [--backend airplay|native] [--room <name> ...] [--shuffle] [--volume 0-100] [--choose] [--json] [--plain] [--dry-run]
  homepodctl play --playlist <name> | --playlist-id <id> [--backend airplay|native] [--room <name> ...] [--shuffle] [--volume 0-100] [--choose] [--json] [--plain] [--dry-run]
  homepodctl volume <0-100> [<room> ...] [--backend airplay|native] [--json] [--plain] [--dry-run]
  homepodctl vol <0-100> [<room> ...] [--backend airplay|native] [--json] [--plain] [--dry-run]
  homepodctl native-run --shortcut <name> [--json] [--dry-run]
  homepodctl config-init

Notes:
  - backend=airplay uses Music.app AirPlay selection (Mac is the sender).
  - backend=native runs a Shortcut you map in the config file (HomePod plays natively if your Shortcut/Scene is set up that way).
  - defaults come from config.json (run homepodctl config-init); commands use defaults when flags/args are omitted.
  - if no rooms are provided and defaults.rooms is empty, airplay commands fall back to Music.app’s currently selected AirPlay outputs (when possible).
  - --verbose (or HOMEPODCTL_VERBOSE=1) prints backend diagnostics to stderr.
  - exit codes: 2 usage/flag errors, 3 config errors, 4 backend command failures.
`)
}

type globalOptions struct {
	help    bool
	verbose bool
}

func parseGlobalOptions(args []string) (globalOptions, string, []string, error) {
	opts := globalOptions{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return opts, "", nil, usageErrf("missing command")
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			return opts, a, args[i+1:], nil
		}
		switch a {
		case "-h", "--help":
			opts.help = true
		case "-v", "--verbose":
			opts.verbose = true
		default:
			return globalOptions{}, "", nil, usageErrf("unknown global flag: %s (tip: run `homepodctl --help`)", a)
		}
	}
	return opts, "", nil, nil
}

func main() {
	if runtime.GOOS != "darwin" {
		fmt.Fprintln(os.Stderr, "error: homepodctl only supports macOS (darwin)")
		os.Exit(exitGeneric)
	}

	opts, cmd, args, err := parseGlobalOptions(os.Args[1:])
	if err != nil {
		usage()
		die(err)
	}
	verbose = opts.verbose || envTruthy(os.Getenv("HOMEPODCTL_VERBOSE"))
	debugf("command=%q args=%q", cmd, args)

	if opts.help || cmd == "" {
		usage()
		if cmd == "" && !opts.help {
			os.Exit(exitUsage)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var cfg *native.Config
	loadCfg := func() *native.Config {
		if cfg != nil {
			return cfg
		}
		loadedCfg, cfgErr := native.LoadConfigOptional()
		if cfgErr != nil {
			die(cfgErr)
		}
		cfg = loadedCfg
		debugf("config: default_backend=%q default_rooms=%v aliases=%d", cfg.Defaults.Backend, cfg.Defaults.Rooms, len(cfg.Aliases))
		return cfg
	}

	switch cmd {
	case "help":
		cmdHelp(args)
	case "version":
		fmt.Printf("homepodctl %s (%s) %s\n", version, commit, date)
	case "automation":
		cmdAutomation(loadCfg(), args)
	case "completion":
		cmdCompletion(args)
	case "doctor":
		cmdDoctor(ctx, args)
	case "devices":
		fs := flag.NewFlagSet("devices", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		jsonOut := fs.Bool("json", false, "output JSON")
		includeNetwork := fs.Bool("include-network", false, "include network address (MAC) in JSON output")
		plain := fs.Bool("plain", false, "plain (no header) output")
		if err := fs.Parse(args); err != nil {
			os.Exit(exitUsage)
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
		plain := fs.Bool("plain", false, "plain (no header) output")
		if err := fs.Parse(args); err != nil {
			os.Exit(exitUsage)
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
	case "status":
		cmdStatus(ctx, args)
	case "now":
		cmdStatus(ctx, args)
	case "out":
		cmdOut(ctx, loadCfg(), args)
	case "aliases":
		cfg := loadCfg()
		fs := flag.NewFlagSet("aliases", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		jsonOut := fs.Bool("json", false, "output JSON")
		plain := fs.Bool("plain", false, "plain (no header) output")
		if err := fs.Parse(args); err != nil {
			os.Exit(exitUsage)
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
	case "run":
		cfg := loadCfg()
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
			writeActionOutput("run", opts.JSON, opts.Plain, actionOutput{
				DryRun:   opts.DryRun,
				Backend:  backend,
				Rooms:    rooms,
				Playlist: name,
			})
		default:
			die(fmt.Errorf("unknown backend in alias %q: %q", aliasName, backend))
		}
	case "pause":
		cmdTransport(ctx, args, "pause", music.Pause)
	case "stop":
		cmdTransport(ctx, args, "stop", music.Stop)
	case "next":
		cmdTransport(ctx, args, "next", music.NextTrack)
	case "prev":
		cmdTransport(ctx, args, "prev", music.PreviousTrack)
	case "play":
		cmdPlay(ctx, loadCfg(), args)
	case "volume":
		cmdVolume(ctx, loadCfg(), "volume", args)
	case "vol":
		cmdVolume(ctx, loadCfg(), "vol", args)
	case "native-run":
		fs := flag.NewFlagSet("native-run", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		shortcutName := fs.String("shortcut", "", "Shortcut name to run")
		jsonOut := fs.Bool("json", false, "output JSON")
		dryRun := fs.Bool("dry-run", false, "resolve and print action without running")
		if err := fs.Parse(args); err != nil {
			os.Exit(exitUsage)
		}

		if strings.TrimSpace(*shortcutName) == "" {
			die(usageErrf("--shortcut is required"))
		}
		if !*dryRun {
			if err := native.RunShortcut(ctx, *shortcutName); err != nil {
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
	case "config-init":
		path, err := native.InitConfig()
		if err != nil {
			die(err)
		}
		fmt.Printf("Wrote %s\n", path)
	default:
		usage()
		die(usageErrf("unknown command: %q (run `homepodctl --help`)", cmd))
	}
}

func die(err error) {
	if verbose {
		fmt.Fprintf(os.Stderr, "debug: exit_code=%d error_type=%T\n", classifyExitCode(err), err)
	}
	fmt.Fprintln(os.Stderr, "error:", formatError(err))
	os.Exit(classifyExitCode(err))
}

func formatError(err error) string {
	if verbose {
		return err.Error()
	}
	var scriptErr *music.ScriptError
	if errors.As(err, &scriptErr) {
		if msg := friendlyScriptError(scriptErr.Output); msg != "" {
			return msg
		}
		return "backend command failed (Music/AppleScript). Re-run with --verbose for details."
	}
	var shortcutErr *native.ShortcutError
	if errors.As(err, &shortcutErr) {
		return "backend command failed (Shortcuts). Re-run with --verbose for details."
	}
	return err.Error()
}

func friendlyScriptError(output string) string {
	o := strings.ToLower(output)
	switch {
	case strings.Contains(o, "not authorised"), strings.Contains(o, "not authorized"), strings.Contains(o, "not permitted"):
		return "Music automation is not permitted. Grant Automation permission to your terminal/binary in System Settings."
	case strings.Contains(o, "connection invalid"):
		return "Could not connect to Music app. Open Music and retry. Use --verbose for backend details."
	case strings.Contains(o, "airplay device"):
		return "AirPlay device lookup failed. Run `homepodctl devices` and use the exact room name."
	default:
		return ""
	}
}

type usageError struct {
	msg string
}

func (e *usageError) Error() string { return e.msg }

func usageErrf(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

func classifyExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ue *usageError
	if errors.As(err, &ue) {
		return exitUsage
	}
	var cfgErr *native.ConfigError
	if errors.As(err, &cfgErr) {
		return exitConfig
	}
	var scriptErr *music.ScriptError
	if errors.As(err, &scriptErr) {
		return exitBackend
	}
	var shortcutErr *native.ShortcutError
	if errors.As(err, &shortcutErr) {
		return exitBackend
	}
	return exitGeneric
}

func debugf(format string, args ...any) {
	if !verbose {
		return
	}
	fmt.Fprintf(os.Stderr, "debug: "+format+"\n", args...)
}

func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func cmdHelp(args []string) {
	if len(args) == 0 {
		usage()
		return
	}
	switch args[0] {
	case "play":
		fmt.Fprint(os.Stdout, `homepodctl play - play an Apple Music playlist

Usage:
  homepodctl play <playlist-query> [--backend airplay|native] [--room <name> ...] [--shuffle] [--volume 0-100] [--choose] [--json] [--plain] [--dry-run]
  homepodctl play --playlist <name> | --playlist-id <id> [--backend airplay|native] [--room <name> ...] [--shuffle] [--volume 0-100] [--choose] [--json] [--plain] [--dry-run]

Notes:
  - <playlist-query> is a fuzzy search against your Music.app user playlists.
  - If --room is omitted, homepodctl uses defaults.rooms from config.json; if that is empty it falls back to Music.app’s currently selected AirPlay outputs (airplay backend).

Examples:
  homepodctl play chill
  homepodctl play "Songs I've been obsessed recently pt. 2"
  homepodctl play autumn --choose
  homepodctl play --room "Bedroom" --playlist-id <PERSISTENT_ID>
`)
	case "out":
		fmt.Fprint(os.Stdout, `homepodctl out - list/set Music.app AirPlay outputs

Usage:
  homepodctl out list [--json] [--plain] [--include-network]
  homepodctl out set [<room> ...] [--backend airplay] [--json] [--plain] [--dry-run]

Notes:
  - Room names must match the AirPlay device names shown by: homepodctl devices
  - out set changes Music.app’s current outputs; it does not modify config.json.

Examples:
  homepodctl out list
  homepodctl out set "Bedroom"
  homepodctl out set "Bedroom" "Living Room"
`)
	case "volume", "vol":
		fmt.Fprint(os.Stdout, `homepodctl volume - set output volume

Usage:
  homepodctl volume <0-100> [<room> ...] [--backend airplay|native] [--json] [--plain] [--dry-run]
  homepodctl vol <0-100> [<room> ...] [--backend airplay|native] [--json] [--plain] [--dry-run]

Notes:
  - If no rooms are provided, homepodctl uses defaults.rooms; if empty it uses Music.app’s currently selected outputs (airplay).

Examples:
  homepodctl volume 35
  homepodctl volume 35 "Living Room"
`)
	case "config-init":
		path, _ := native.ConfigPath()
		fmt.Fprintf(os.Stdout, `homepodctl config-init - create a starter config file

Writes a starter config to:
  %s

Notes:
  - If the file already exists, this command is a no-op.
  - Edit defaults.rooms to your AirPlay device names (homepodctl devices).
`, path)
	case "automation":
		fmt.Fprint(os.Stdout, `homepodctl automation - declarative playback routines (v1 scaffold)

Usage:
  homepodctl automation init --preset <morning|focus|winddown|party|reset> [--name <string>] [--json]
  homepodctl automation validate -f <file|-> [--json]
  homepodctl automation plan -f <file|-> [--json]
  homepodctl automation run -f <file|-> --dry-run [--json] [--no-input]

Notes:
  - run currently supports --dry-run only (execution scaffolding).
  - Use --json --no-input for agent-safe usage.
`)
	default:
		usage()
	}
}

func writeJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

type actionResult struct {
	OK         bool              `json:"ok"`
	Action     string            `json:"action"`
	DryRun     bool              `json:"dryRun,omitempty"`
	Backend    string            `json:"backend,omitempty"`
	Rooms      []string          `json:"rooms,omitempty"`
	Playlist   string            `json:"playlist,omitempty"`
	PlaylistID string            `json:"playlistId,omitempty"`
	Shortcut   string            `json:"shortcut,omitempty"`
	NowPlaying *music.NowPlaying `json:"nowPlaying,omitempty"`
}

type actionOutput struct {
	Backend    string
	DryRun     bool
	Rooms      []string
	Playlist   string
	PlaylistID string
	Shortcut   string
	NowPlaying *music.NowPlaying
}

type outputOptions struct {
	JSON   bool
	Plain  bool
	DryRun bool
}

func parseOutputFlags(flags parsedArgs) (bool, bool, error) {
	jsonOut, _, err := flags.boolStrict("json")
	if err != nil {
		return false, false, err
	}
	plainOut, _, err := flags.boolStrict("plain")
	if err != nil {
		return false, false, err
	}
	return jsonOut, plainOut, nil
}

func parseOutputOptions(flags parsedArgs) (outputOptions, error) {
	jsonOut, plainOut, err := parseOutputFlags(flags)
	if err != nil {
		return outputOptions{}, err
	}
	dryRun, _, err := flags.boolStrict("dry-run")
	if err != nil {
		return outputOptions{}, err
	}
	return outputOptions{
		JSON:   jsonOut,
		Plain:  plainOut,
		DryRun: dryRun,
	}, nil
}

func writeActionOutput(action string, jsonOut bool, plainOut bool, out actionOutput) {
	if jsonOut {
		writeJSON(actionResult{
			OK:         true,
			Action:     action,
			DryRun:     out.DryRun,
			Backend:    out.Backend,
			Rooms:      out.Rooms,
			Playlist:   out.Playlist,
			PlaylistID: out.PlaylistID,
			Shortcut:   out.Shortcut,
			NowPlaying: out.NowPlaying,
		})
		return
	}
	if out.NowPlaying != nil {
		if plainOut {
			printNowPlayingPlain(*out.NowPlaying)
		} else {
			printNowPlaying(*out.NowPlaying)
		}
		return
	}
	if out.DryRun {
		fmt.Printf("dry-run action=%s backend=%s rooms=%s playlist=%q playlist_id=%q shortcut=%q\n",
			action,
			out.Backend,
			strings.Join(out.Rooms, ","),
			out.Playlist,
			out.PlaylistID,
			out.Shortcut,
		)
	}
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

func printNowPlayingPlain(np music.NowPlaying) {
	var outputNames []string
	for _, o := range np.Outputs {
		outputNames = append(outputNames, o.Name)
	}
	fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\n",
		np.PlayerState,
		np.Track.Name,
		np.Track.Artist,
		np.Track.Album,
		np.PlaylistName,
		strings.Join(outputNames, ","),
	)
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

type aliasRow struct {
	Name    string   `json:"name"`
	Backend string   `json:"backend"`
	Rooms   []string `json:"rooms"`
	Target  string   `json:"target"`
}

func buildAliasRows(cfg *native.Config) []aliasRow {
	names := make([]string, 0, len(cfg.Aliases))
	for name := range cfg.Aliases {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([]aliasRow, 0, len(names))
	for _, name := range names {
		a := cfg.Aliases[name]
		backend := a.Backend
		if backend == "" {
			backend = cfg.Defaults.Backend
		}
		rooms := append([]string(nil), a.Rooms...)
		if len(rooms) == 0 {
			rooms = append(rooms, cfg.Defaults.Rooms...)
		}
		target := a.Playlist
		if target == "" {
			target = a.PlaylistID
		}
		if a.Shortcut != "" {
			target = "shortcut:" + a.Shortcut
		}
		rows = append(rows, aliasRow{
			Name:    name,
			Backend: backend,
			Rooms:   rooms,
			Target:  target,
		})
	}
	return rows
}

func printAliasesTable(w io.Writer, rows []aliasRow, plain bool) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !plain {
		fmt.Fprintln(tw, "NAME\tBACKEND\tROOMS\tTARGET")
	}
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", row.Name, row.Backend, strings.Join(row.Rooms, ","), row.Target)
	}
	_ = tw.Flush()
}

type automationFile struct {
	Version  string             `json:"version" yaml:"version"`
	Name     string             `json:"name" yaml:"name"`
	Defaults automationDefaults `json:"defaults" yaml:"defaults"`
	Steps    []automationStep   `json:"steps" yaml:"steps"`
}

type automationDefaults struct {
	Backend string   `json:"backend,omitempty" yaml:"backend,omitempty"`
	Rooms   []string `json:"rooms,omitempty" yaml:"rooms,omitempty"`
	Volume  *int     `json:"volume,omitempty" yaml:"volume,omitempty"`
	Shuffle *bool    `json:"shuffle,omitempty" yaml:"shuffle,omitempty"`
}

type automationStep struct {
	Type       string   `json:"type" yaml:"type"`
	Rooms      []string `json:"rooms,omitempty" yaml:"rooms,omitempty"`
	Query      string   `json:"query,omitempty" yaml:"query,omitempty"`
	PlaylistID string   `json:"playlistId,omitempty" yaml:"playlistId,omitempty"`
	Value      *int     `json:"value,omitempty" yaml:"value,omitempty"`
	State      string   `json:"state,omitempty" yaml:"state,omitempty"`
	Timeout    string   `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Action     string   `json:"action,omitempty" yaml:"action,omitempty"`
}

type automationStepResult struct {
	Index      int            `json:"index"`
	Type       string         `json:"type"`
	Input      automationStep `json:"input"`
	Resolved   any            `json:"resolved,omitempty"`
	OK         bool           `json:"ok"`
	Skipped    bool           `json:"skipped"`
	Error      string         `json:"error,omitempty"`
	DurationMS int64          `json:"durationMs"`
}

type automationCommandResult struct {
	Name       string                 `json:"name"`
	Version    string                 `json:"version"`
	Mode       string                 `json:"mode"`
	OK         bool                   `json:"ok"`
	StartedAt  string                 `json:"startedAt"`
	EndedAt    string                 `json:"endedAt"`
	DurationMS int64                  `json:"durationMs"`
	Steps      []automationStepResult `json:"steps"`
}

type automationInitResult struct {
	Preset  string `json:"preset"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

func cmdAutomation(cfg *native.Config, args []string) {
	if len(args) == 0 {
		die(usageErrf("usage: homepodctl automation <run|validate|plan|init> [args]"))
	}
	switch args[0] {
	case "run":
		cmdAutomationRun(cfg, args[1:])
	case "validate":
		cmdAutomationValidate(cfg, args[1:])
	case "plan":
		cmdAutomationPlan(cfg, args[1:])
	case "init":
		cmdAutomationInit(args[1:])
	default:
		die(usageErrf("unknown automation subcommand: %q", args[0]))
	}
}

func cmdAutomationRun(cfg *native.Config, args []string) {
	fs := flag.NewFlagSet("automation run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	filePath := fs.String("file", "", "automation file path or - for stdin")
	fs.StringVar(filePath, "f", "", "automation file path or - for stdin")
	dryRun := fs.Bool("dry-run", false, "resolve and print without executing")
	jsonOut := fs.Bool("json", false, "output JSON")
	_ = fs.Bool("no-input", false, "disable interactive fallback")
	if err := fs.Parse(args); err != nil {
		die(usageErrf("usage: homepodctl automation run -f <file|-> [--dry-run] [--json] [--no-input]"))
	}
	if strings.TrimSpace(*filePath) == "" {
		die(usageErrf("--file is required"))
	}
	if !*dryRun {
		die(usageErrf("automation run without --dry-run is not implemented yet (use --dry-run, plan, or validate)"))
	}
	doc, err := loadAutomationFile(*filePath)
	if err != nil {
		die(err)
	}
	if err := validateAutomation(doc); err != nil {
		die(err)
	}
	result := buildAutomationResult("dry-run", doc, resolveAutomationSteps(cfg, doc))
	emitAutomationResult(result, *jsonOut)
}

func cmdAutomationValidate(_ *native.Config, args []string) {
	fs := flag.NewFlagSet("automation validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	filePath := fs.String("file", "", "automation file path or - for stdin")
	fs.StringVar(filePath, "f", "", "automation file path or - for stdin")
	jsonOut := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		die(usageErrf("usage: homepodctl automation validate -f <file|-> [--json]"))
	}
	if strings.TrimSpace(*filePath) == "" {
		die(usageErrf("--file is required"))
	}
	doc, err := loadAutomationFile(*filePath)
	if err != nil {
		die(err)
	}
	if err := validateAutomation(doc); err != nil {
		die(err)
	}
	result := buildAutomationResult("validate", doc, resolveAutomationSteps(nil, doc))
	emitAutomationResult(result, *jsonOut)
}

func cmdAutomationPlan(cfg *native.Config, args []string) {
	fs := flag.NewFlagSet("automation plan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	filePath := fs.String("file", "", "automation file path or - for stdin")
	fs.StringVar(filePath, "f", "", "automation file path or - for stdin")
	jsonOut := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		die(usageErrf("usage: homepodctl automation plan -f <file|-> [--json]"))
	}
	if strings.TrimSpace(*filePath) == "" {
		die(usageErrf("--file is required"))
	}
	doc, err := loadAutomationFile(*filePath)
	if err != nil {
		die(err)
	}
	if err := validateAutomation(doc); err != nil {
		die(err)
	}
	result := buildAutomationResult("plan", doc, resolveAutomationSteps(cfg, doc))
	emitAutomationResult(result, *jsonOut)
}

func cmdAutomationInit(args []string) {
	fs := flag.NewFlagSet("automation init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	preset := fs.String("preset", "", "preset name: morning|focus|winddown|party|reset")
	name := fs.String("name", "", "override routine name")
	jsonOut := fs.Bool("json", false, "output JSON metadata")
	if err := fs.Parse(args); err != nil {
		die(usageErrf("usage: homepodctl automation init --preset <name> [--name <string>] [--json]"))
	}
	if strings.TrimSpace(*preset) == "" {
		die(usageErrf("--preset is required"))
	}
	doc, err := automationPreset(*preset)
	if err != nil {
		die(err)
	}
	if strings.TrimSpace(*name) != "" {
		doc.Name = strings.TrimSpace(*name)
	}
	b, err := yaml.Marshal(doc)
	if err != nil {
		die(fmt.Errorf("encode preset: %w", err))
	}
	if *jsonOut {
		writeJSON(automationInitResult{Preset: strings.TrimSpace(*preset), Name: doc.Name, Content: string(b)})
		return
	}
	fmt.Print(string(b))
}

func loadAutomationFile(path string) (*automationFile, error) {
	b, err := readAutomationInput(path)
	if err != nil {
		return nil, err
	}
	doc, err := parseAutomationBytes(b)
	if err != nil {
		return nil, err
	}
	return doc, nil
}

func readAutomationInput(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return b, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read automation file %q: %w", path, err)
	}
	return b, nil
}

func parseAutomationBytes(b []byte) (*automationFile, error) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil, usageErrf("automation file is empty")
	}
	var doc automationFile
	if b[0] == '{' {
		if err := json.Unmarshal(b, &doc); err != nil {
			return nil, usageErrf("invalid automation JSON: %v", err)
		}
		return &doc, nil
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, usageErrf("invalid automation YAML: %v", err)
	}
	return &doc, nil
}

func validateAutomation(doc *automationFile) error {
	if doc == nil {
		return usageErrf("automation file is required")
	}
	if strings.TrimSpace(doc.Version) != "1" {
		return usageErrf("version: expected \"1\"")
	}
	if strings.TrimSpace(doc.Name) == "" {
		return usageErrf("name: required")
	}
	if err := validateAutomationDefaults("defaults", doc.Defaults); err != nil {
		return err
	}
	if len(doc.Steps) == 0 {
		return usageErrf("steps: must contain at least one step")
	}
	for i, st := range doc.Steps {
		if err := validateAutomationStep(i, st); err != nil {
			return err
		}
	}
	return nil
}

func validateAutomationDefaults(path string, d automationDefaults) error {
	if d.Backend != "" && d.Backend != "airplay" && d.Backend != "native" {
		return usageErrf("%s.backend: expected airplay or native", path)
	}
	if d.Volume != nil && (*d.Volume < 0 || *d.Volume > 100) {
		return usageErrf("%s.volume: expected 0..100", path)
	}
	for i, r := range d.Rooms {
		if strings.TrimSpace(r) == "" {
			return usageErrf("%s.rooms[%d]: must be non-empty", path, i)
		}
	}
	return nil
}

func validateAutomationStep(i int, st automationStep) error {
	path := fmt.Sprintf("steps[%d]", i)
	t := strings.TrimSpace(st.Type)
	if t == "" {
		return usageErrf("%s.type: required", path)
	}
	switch t {
	case "out.set":
		if len(st.Rooms) == 0 {
			return usageErrf("%s.rooms: required for out.set", path)
		}
		for j, r := range st.Rooms {
			if strings.TrimSpace(r) == "" {
				return usageErrf("%s.rooms[%d]: must be non-empty", path, j)
			}
		}
	case "play":
		hasQ := strings.TrimSpace(st.Query) != ""
		hasID := strings.TrimSpace(st.PlaylistID) != ""
		if hasQ == hasID {
			return usageErrf("%s: play requires exactly one of query or playlistId", path)
		}
	case "volume.set":
		if st.Value == nil {
			return usageErrf("%s.value: required for volume.set", path)
		}
		if *st.Value < 0 || *st.Value > 100 {
			return usageErrf("%s.value: expected 0..100", path)
		}
	case "wait":
		s := strings.TrimSpace(st.State)
		if s != "playing" && s != "paused" && s != "stopped" {
			return usageErrf("%s.state: expected playing|paused|stopped", path)
		}
		if strings.TrimSpace(st.Timeout) == "" {
			return usageErrf("%s.timeout: required", path)
		}
		d, err := time.ParseDuration(st.Timeout)
		if err != nil {
			return usageErrf("%s.timeout: invalid duration", path)
		}
		if d < time.Second || d > 10*time.Minute {
			return usageErrf("%s.timeout: expected between 1s and 10m", path)
		}
	case "transport":
		if strings.TrimSpace(st.Action) != "stop" {
			return usageErrf("%s.action: only \"stop\" is supported in v1", path)
		}
	default:
		return usageErrf("%s.type: unsupported step type %q", path, st.Type)
	}
	return nil
}

func resolveAutomationSteps(cfg *native.Config, doc *automationFile) []automationStepResult {
	resolvedDefaults := doc.Defaults
	if cfg != nil {
		if strings.TrimSpace(resolvedDefaults.Backend) == "" {
			resolvedDefaults.Backend = cfg.Defaults.Backend
		}
		if len(resolvedDefaults.Rooms) == 0 {
			resolvedDefaults.Rooms = append([]string(nil), cfg.Defaults.Rooms...)
		}
		if resolvedDefaults.Volume == nil && cfg.Defaults.Volume != nil {
			v := *cfg.Defaults.Volume
			resolvedDefaults.Volume = &v
		}
		if resolvedDefaults.Shuffle == nil {
			v := cfg.Defaults.Shuffle
			resolvedDefaults.Shuffle = &v
		}
	}

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

func buildAutomationResult(mode string, doc *automationFile, steps []automationStepResult) automationCommandResult {
	started := time.Now().UTC()
	ended := started
	return automationCommandResult{
		Name:       doc.Name,
		Version:    doc.Version,
		Mode:       mode,
		OK:         true,
		StartedAt:  started.Format(time.RFC3339),
		EndedAt:    ended.Format(time.RFC3339),
		DurationMS: ended.Sub(started).Milliseconds(),
		Steps:      steps,
	}
}

func emitAutomationResult(result automationCommandResult, jsonOut bool) {
	if jsonOut {
		writeJSON(result)
		return
	}
	fmt.Printf("automation name=%q mode=%s ok=%t steps=%d\n", result.Name, result.Mode, result.OK, len(result.Steps))
	for _, st := range result.Steps {
		fmt.Printf("%d/%d %s ok=%t\n", st.Index+1, len(result.Steps), st.Type, st.OK)
	}
}

func automationPreset(name string) (automationFile, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "morning":
		return automationFile{
			Version:  "1",
			Name:     "morning",
			Defaults: automationDefaults{Backend: "airplay", Rooms: []string{"Bedroom"}, Volume: intPtr(30), Shuffle: boolPtr(false)},
			Steps:    []automationStep{{Type: "out.set", Rooms: []string{"Bedroom"}}, {Type: "play", Query: "Morning Mix"}, {Type: "volume.set", Value: intPtr(30)}, {Type: "wait", State: "playing", Timeout: "20s"}},
		}, nil
	case "focus":
		return automationFile{
			Version:  "1",
			Name:     "focus",
			Defaults: automationDefaults{Backend: "airplay", Rooms: []string{"Office"}, Volume: intPtr(25), Shuffle: boolPtr(false)},
			Steps:    []automationStep{{Type: "out.set", Rooms: []string{"Office"}}, {Type: "play", Query: "Deep Focus"}, {Type: "volume.set", Value: intPtr(25)}, {Type: "wait", State: "playing", Timeout: "20s"}},
		}, nil
	case "winddown":
		return automationFile{
			Version:  "1",
			Name:     "winddown",
			Defaults: automationDefaults{Backend: "airplay", Rooms: []string{"Bedroom"}, Volume: intPtr(20), Shuffle: boolPtr(false)},
			Steps:    []automationStep{{Type: "out.set", Rooms: []string{"Bedroom"}}, {Type: "play", Query: "Evening Ambient"}, {Type: "volume.set", Value: intPtr(20)}, {Type: "wait", State: "playing", Timeout: "20s"}},
		}, nil
	case "party":
		return automationFile{
			Version:  "1",
			Name:     "party",
			Defaults: automationDefaults{Backend: "airplay", Rooms: []string{"Living Room", "Kitchen"}, Volume: intPtr(55), Shuffle: boolPtr(true)},
			Steps:    []automationStep{{Type: "out.set", Rooms: []string{"Living Room", "Kitchen"}}, {Type: "play", Query: "Party Mix"}, {Type: "volume.set", Value: intPtr(55)}, {Type: "wait", State: "playing", Timeout: "30s"}},
		}, nil
	case "reset":
		return automationFile{
			Version:  "1",
			Name:     "reset",
			Defaults: automationDefaults{Backend: "airplay", Rooms: []string{"Bedroom"}, Volume: intPtr(25)},
			Steps:    []automationStep{{Type: "transport", Action: "stop"}, {Type: "out.set", Rooms: []string{"Bedroom"}}, {Type: "volume.set", Value: intPtr(25)}},
		}, nil
	default:
		return automationFile{}, usageErrf("unknown preset %q (expected morning, focus, winddown, party, reset)", name)
	}
}

func intPtr(v int) *int { return &v }

func boolPtr(v bool) *bool { return &v }

type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // pass|warn|fail
	Message string `json:"message"`
	Tip     string `json:"tip,omitempty"`
}

type doctorReport struct {
	OK        bool          `json:"ok"`
	CheckedAt string        `json:"checkedAt"`
	Checks    []doctorCheck `json:"checks"`
}

func cmdDoctor(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "output JSON")
	plain := fs.Bool("plain", false, "plain output")
	if err := fs.Parse(args); err != nil {
		os.Exit(exitUsage)
	}
	report := runDoctorChecks(ctx)
	if *jsonOut {
		writeJSON(report)
	} else {
		printDoctorReport(report, *plain)
	}
	if !report.OK {
		os.Exit(exitGeneric)
	}
}

func runDoctorChecks(ctx context.Context) doctorReport {
	report := doctorReport{
		OK:        true,
		CheckedAt: time.Now().Format(time.RFC3339),
	}
	add := func(c doctorCheck) {
		if c.Status == "fail" {
			report.OK = false
		}
		report.Checks = append(report.Checks, c)
	}

	if _, err := exec.LookPath("osascript"); err != nil {
		add(doctorCheck{Name: "osascript", Status: "fail", Message: "osascript not found", Tip: "Install/restore macOS command-line tools."})
	} else {
		add(doctorCheck{Name: "osascript", Status: "pass", Message: "osascript available"})
	}
	if _, err := exec.LookPath("shortcuts"); err != nil {
		add(doctorCheck{Name: "shortcuts", Status: "warn", Message: "shortcuts command not found", Tip: "Native backend requires the Shortcuts CLI."})
	} else {
		add(doctorCheck{Name: "shortcuts", Status: "pass", Message: "shortcuts available"})
	}

	path, err := native.ConfigPath()
	if err != nil {
		add(doctorCheck{Name: "config-path", Status: "fail", Message: fmt.Sprintf("cannot resolve config path: %v", err)})
	} else {
		add(doctorCheck{Name: "config-path", Status: "pass", Message: path})
		cfg, cfgErr := native.LoadConfigOptional()
		if cfgErr != nil {
			add(doctorCheck{Name: "config", Status: "fail", Message: cfgErr.Error(), Tip: "Fix JSON syntax or re-run `homepodctl config-init`."})
		} else if len(cfg.Aliases) == 0 {
			add(doctorCheck{Name: "config", Status: "warn", Message: "no aliases configured", Tip: "Run `homepodctl config-init` and edit defaults/aliases."})
		} else {
			add(doctorCheck{Name: "config", Status: "pass", Message: fmt.Sprintf("aliases=%d", len(cfg.Aliases))})
		}
	}

	backendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := music.GetNowPlaying(backendCtx); err != nil {
		add(doctorCheck{
			Name:    "music-backend",
			Status:  "warn",
			Message: formatError(err),
			Tip:     "Open Music.app and grant Automation permissions if prompted.",
		})
	} else {
		add(doctorCheck{Name: "music-backend", Status: "pass", Message: "Music backend reachable"})
	}
	return report
}

func printDoctorReport(report doctorReport, plain bool) {
	if plain {
		fmt.Println("STATUS\tCHECK\tMESSAGE\tTIP")
		for _, c := range report.Checks {
			fmt.Printf("%s\t%s\t%s\t%s\n", c.Status, c.Name, c.Message, c.Tip)
		}
		return
	}
	fmt.Printf("doctor ok=%t checked_at=%s\n", report.OK, report.CheckedAt)
	for _, c := range report.Checks {
		if c.Tip != "" {
			fmt.Printf("%s\t%s\t%s (tip: %s)\n", c.Status, c.Name, c.Message, c.Tip)
			continue
		}
		fmt.Printf("%s\t%s\t%s\n", c.Status, c.Name, c.Message)
	}
}

func cmdCompletion(args []string) {
	if len(args) != 1 {
		die(usageErrf("usage: homepodctl completion <bash|zsh|fish>"))
	}
	shell := strings.ToLower(strings.TrimSpace(args[0]))
	script, err := completionScript(shell)
	if err != nil {
		die(err)
	}
	fmt.Print(script)
}

func completionScript(shell string) (string, error) {
	switch shell {
	case "bash":
		return `# bash completion for homepodctl
_homepodctl_completion() {
  local cur prev
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"
  local cmds="help version automation completion doctor devices out playlists status now aliases run pause stop next prev play volume vol native-run config-init"
  if [[ $COMP_CWORD -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "$cmds --help --verbose" -- "$cur") )
    return 0
  fi
  COMPREPLY=( $(compgen -W "--json --plain --help --verbose --backend --room --playlist --playlist-id --shuffle --volume --watch --query --limit --shortcut --include-network --file --dry-run --no-input --preset --name" -- "$cur") )
}
complete -F _homepodctl_completion homepodctl
`, nil
	case "zsh":
		return `#compdef homepodctl
_homepodctl() {
  local -a commands
  commands=(
    'help:Show help'
    'version:Show version'
    'automation:Run automation routines'
    'completion:Generate shell completion'
    'doctor:Run diagnostics'
    'devices:List devices'
    'out:Manage outputs'
    'playlists:List playlists'
    'status:Show now playing'
    'now:Alias of status'
    'aliases:List aliases'
    'run:Run alias'
    'pause:Pause playback'
    'stop:Stop playback'
    'next:Next track'
    'prev:Previous track'
    'play:Play playlist'
    'volume:Set volume'
    'vol:Set volume'
    'native-run:Run shortcut'
    'config-init:Write starter config'
  )
  _arguments '*::command:->command'
  case $state in
    command) _describe -t commands "homepodctl command" commands ;;
  esac
}
_homepodctl "$@"
`, nil
	case "fish":
		return `# fish completion for homepodctl
complete -c homepodctl -f -a "help version automation completion doctor devices out playlists status now aliases run pause stop next prev play volume vol native-run config-init"
complete -c homepodctl -l json
complete -c homepodctl -l plain
complete -c homepodctl -l verbose
complete -c homepodctl -l backend
complete -c homepodctl -l room
complete -c homepodctl -l playlist
complete -c homepodctl -l playlist-id
complete -c homepodctl -l shuffle
complete -c homepodctl -l volume
complete -c homepodctl -l watch
complete -c homepodctl -l query
complete -c homepodctl -l limit
complete -c homepodctl -l shortcut
complete -c homepodctl -l include-network
complete -c homepodctl -l file
complete -c homepodctl -l dry-run
complete -c homepodctl -l no-input
complete -c homepodctl -l preset
complete -c homepodctl -l name
`, nil
	default:
		return "", usageErrf("unknown shell %q (expected bash, zsh, or fish)", shell)
	}
}

func cmdStatus(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "output JSON")
	plain := fs.Bool("plain", false, "plain output")
	watch := fs.Duration("watch", 0, "poll interval (e.g. 1s); 0 prints once")
	if err := fs.Parse(args); err != nil {
		os.Exit(exitUsage)
	}
	debugf("status: json=%t plain=%t watch=%s", *jsonOut, *plain, watch.String())
	printOnce := func() error {
		np, err := music.GetNowPlaying(ctx)
		if err != nil {
			return err
		}
		if *jsonOut {
			writeJSON(np)
			return nil
		}
		if *plain {
			printNowPlayingPlain(np)
		} else {
			printNowPlaying(np)
		}
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

func cmdTransport(ctx context.Context, args []string, action string, fn func(context.Context) error) {
	flags, positionals, err := parseArgs(args)
	if err != nil {
		die(err)
	}
	if len(positionals) != 0 {
		die(usageErrf("usage: homepodctl %s [--json] [--plain]", action))
	}
	jsonOut, plainOut, err := parseOutputFlags(flags)
	if err != nil {
		die(err)
	}
	if err := fn(ctx); err != nil {
		die(err)
	}
	if np, err := music.GetNowPlaying(ctx); err == nil {
		writeActionOutput(action, jsonOut, plainOut, actionOutput{NowPlaying: &np})
		return
	}
	writeActionOutput(action, jsonOut, plainOut, actionOutput{})
}

func cmdOut(ctx context.Context, cfg *native.Config, args []string) {
	if len(args) < 1 {
		die(usageErrf("usage: homepodctl out <list|set> [args]"))
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("out list", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		jsonOut := fs.Bool("json", false, "output JSON")
		includeNetwork := fs.Bool("include-network", false, "include network address (MAC) in JSON output")
		plain := fs.Bool("plain", false, "plain (no header) output")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(exitUsage)
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
		jsonOut, plainOut, err := parseOutputFlags(flags)
		if err != nil {
			die(err)
		}
		dryRun, _, err := flags.boolStrict("dry-run")
		if err != nil {
			die(err)
		}
		backend := strings.TrimSpace(flags.string("backend"))
		if backend == "" {
			backend = cfg.Defaults.Backend
		}
		if backend != "airplay" {
			die(usageErrf("out set only supports backend=airplay (got %q)", backend))
		}
		rooms := positionals
		if len(rooms) == 0 {
			rooms = append(rooms, cfg.Defaults.Rooms...)
		}
		if len(rooms) == 0 {
			die(usageErrf("no rooms provided (usage: homepodctl out set <room> ...; tip: run `homepodctl devices` to list names)"))
		}
		debugf("out set: backend=%s rooms=%v", backend, rooms)
		if dryRun {
			writeActionOutput("out.set", jsonOut, plainOut, actionOutput{
				DryRun:  true,
				Backend: backend,
				Rooms:   rooms,
			})
			return
		}
		if err := music.SetCurrentAirPlayDevices(ctx, rooms); err != nil {
			die(err)
		}
		if np, err := music.GetNowPlaying(ctx); err == nil {
			writeActionOutput("out.set", jsonOut, plainOut, actionOutput{
				Backend:    backend,
				Rooms:      rooms,
				NowPlaying: &np,
			})
		} else {
			writeActionOutput("out.set", jsonOut, plainOut, actionOutput{
				Backend: backend,
				Rooms:   rooms,
			})
		}
	default:
		die(usageErrf("usage: homepodctl out <list|set> [args]"))
	}
}

func cmdVolume(ctx context.Context, cfg *native.Config, name string, args []string) {
	flags, positionals, err := parseArgs(args)
	if err != nil {
		die(err)
	}
	jsonOut, plainOut, err := parseOutputFlags(flags)
	if err != nil {
		die(err)
	}
	dryRun, _, err := flags.boolStrict("dry-run")
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
		if dryRun {
			writeActionOutput(name, jsonOut, plainOut, actionOutput{
				DryRun:  true,
				Backend: backend,
				Rooms:   rooms,
			})
			return
		}
		for _, room := range rooms {
			if err := music.SetAirPlayDeviceVolume(ctx, room, value); err != nil {
				die(err)
			}
		}
		if np, err := music.GetNowPlaying(ctx); err == nil {
			writeActionOutput(name, jsonOut, plainOut, actionOutput{
				Backend:    backend,
				Rooms:      rooms,
				NowPlaying: &np,
			})
		} else {
			writeActionOutput(name, jsonOut, plainOut, actionOutput{
				Backend: backend,
				Rooms:   rooms,
			})
		}
	case "native":
		debugf("%s: backend=native value=%d rooms=%v", name, value, rooms)
		if dryRun {
			writeActionOutput(name, jsonOut, plainOut, actionOutput{
				DryRun:  true,
				Backend: backend,
				Rooms:   rooms,
			})
			return
		}
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
			writeActionOutput(name, jsonOut, plainOut, actionOutput{
				Backend:    backend,
				Rooms:      rooms,
				NowPlaying: &np,
			})
		} else {
			writeActionOutput(name, jsonOut, plainOut, actionOutput{
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
	jsonOut, plainOut, err := parseOutputFlags(flags)
	if err != nil {
		die(err)
	}
	dryRun, _, err := flags.boolStrict("dry-run")
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
		if dryRun {
			if strings.TrimSpace(query) == "" && strings.TrimSpace(playlistID) == "" {
				die(usageErrf("playlist is required (pass <playlist-query>, --playlist, or --playlist-id)"))
			}
			writeActionOutput("play", jsonOut, plainOut, actionOutput{
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
		debugf("play: backend=airplay rooms=%v playlist_id=%q query=%q shuffle=%t volume=%d explicit_volume=%t choose=%t", rooms, id, query, shuffle, volume, volumeExplicit, choose)

		// If we have rooms, select outputs first. If we don't, keep Music.app's current outputs.
		if len(rooms) > 0 {
			if err := music.SetCurrentAirPlayDevices(ctx, rooms); err != nil {
				die(err)
			}
		}
		if err := validateAirplayVolumeSelection(volumeExplicit, volume, rooms); err != nil {
			die(err)
		}
		if volume >= 0 && len(rooms) > 0 {
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
			writeActionOutput("play", jsonOut, plainOut, actionOutput{
				Backend:    backend,
				Rooms:      rooms,
				Playlist:   query,
				PlaylistID: id,
				NowPlaying: &np,
			})
		} else {
			writeActionOutput("play", jsonOut, plainOut, actionOutput{
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
		if dryRun {
			name := strings.TrimSpace(query)
			if name == "" {
				name = playlistID
			}
			writeActionOutput("play", jsonOut, plainOut, actionOutput{
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
			name, err = music.FindUserPlaylistNameByPersistentID(ctx, playlistID)
			if err != nil {
				die(err)
			}
		}
		debugf("play: backend=native rooms=%v playlist=%q playlist_id=%q", rooms, name, playlistID)
		for _, room := range rooms {
			shortcutName, ok := cfg.Native.Playlists[room][name]
			if !ok || strings.TrimSpace(shortcutName) == "" {
				die(fmt.Errorf("no native mapping for room=%q playlist=%q (edit config)", room, name))
			}
			if err := native.RunShortcut(ctx, shortcutName); err != nil {
				die(err)
			}
		}
		writeActionOutput("play", jsonOut, plainOut, actionOutput{
			Backend:  backend,
			Rooms:    rooms,
			Playlist: name,
		})
	default:
		die(usageErrf("unknown backend: %q", backend))
	}
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
		return 0, true, usageErrf("--%s requires a value", key)
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, true, usageErrf("invalid --%s %q", key, s)
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
	return false, true, usageErrf("invalid --%s %q (expected true/false)", key, p.string(key))
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
							return parsedArgs{}, nil, usageErrf("--room requires a value")
						}
						i++
						val = args[i]
					}
					push("room", val)
					continue
				}
				if val == "" {
					if i+1 >= len(args) {
						return parsedArgs{}, nil, usageErrf("--%s requires a value", key)
					}
					i++
					val = args[i]
				}
				push(key, val)
			case "shuffle", "choose", "json", "plain", "dry-run":
				if val == "" && i+1 < len(args) && isBoolWord(args[i+1]) {
					i++
					val = args[i]
				}
				if val == "" {
					val = "true"
				}
				push(key, val)
			default:
				return parsedArgs{}, nil, usageErrf("unknown flag: %s (tip: rooms use --room <name>; run `homepodctl help`)", a)
			}
			continue
		}

		return parsedArgs{}, nil, usageErrf("unknown flag: %s (tip: run `homepodctl help`)", a)
	}
	return out, positionals, nil
}
