package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

var (
	version              = "dev"
	commit               = "none"
	date                 = "unknown"
	getNowPlaying        = music.GetNowPlaying
	searchPlaylists      = music.SearchUserPlaylists
	setCurrentOutputs    = music.SetCurrentAirPlayDevices
	setDeviceVolume      = music.SetAirPlayDeviceVolume
	setShuffle           = music.SetShuffleEnabled
	playPlaylistByID     = music.PlayUserPlaylistByPersistentID
	findPlaylistNameByID = music.FindUserPlaylistNameByPersistentID
	runNativeShortcut    = native.RunShortcut
	stopPlayback         = music.Stop
	lookPath             = exec.LookPath
	configPath           = native.ConfigPath
	loadConfigOptional   = native.LoadConfigOptional
	sleepFn              = time.Sleep
	verbose              bool
	jsonErrorOut         bool
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
  homepodctl config <validate|get|set> [args]
  homepodctl automation <run|validate|plan|init> [args]
  homepodctl completion <bash|zsh|fish>
  homepodctl completion install <bash|zsh|fish> [--path <file-or-dir>]
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

type jsonErrorResponse struct {
	OK    bool             `json:"ok"`
	Error jsonErrorPayload `json:"error"`
}

type jsonErrorPayload struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	ExitCode int    `json:"exitCode"`
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
	jsonErrorOut = wantsJSONErrors(os.Args[1:])
	if runtime.GOOS != "darwin" {
		die(errors.New("homepodctl only supports macOS (darwin)"))
	}

	opts, cmd, args, err := parseGlobalOptions(os.Args[1:])
	if err != nil {
		if !jsonErrorOut {
			usage()
		}
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
		cmdAutomation(ctx, loadCfg(), args)
	case "config":
		cmdConfig(args)
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
		if !jsonErrorOut {
			usage()
		}
		die(usageErrf("unknown command: %q (run `homepodctl --help`)", cmd))
	}
}

func die(err error) {
	code := classifyExitCode(err)
	if verbose {
		fmt.Fprintf(os.Stderr, "debug: exit_code=%d error_type=%T\n", code, err)
	}
	if jsonErrorOut {
		enc := json.NewEncoder(os.Stderr)
		enc.SetIndent("", "  ")
		_ = enc.Encode(jsonErrorResponse{
			OK: false,
			Error: jsonErrorPayload{
				Code:     classifyErrorCode(err),
				Message:  formatError(err),
				ExitCode: code,
			},
		})
		os.Exit(code)
	}
	fmt.Fprintln(os.Stderr, "error:", formatError(err))
	os.Exit(code)
}

func wantsJSONErrors(args []string) bool {
	for _, a := range args {
		if a == "--json" {
			return true
		}
		if strings.HasPrefix(a, "--json=") {
			v := strings.TrimSpace(strings.TrimPrefix(a, "--json="))
			switch strings.ToLower(v) {
			case "1", "true", "yes", "on":
				return true
			}
		}
	}
	return false
}

func classifyErrorCode(err error) string {
	var autoValErr *automationValidationError
	if errors.As(err, &autoValErr) {
		return "AUTOMATION_VALIDATION_ERROR"
	}
	switch classifyExitCode(err) {
	case exitUsage:
		return "USAGE_ERROR"
	case exitConfig:
		return "CONFIG_ERROR"
	case exitBackend:
		return "BACKEND_ERROR"
	default:
		return "GENERIC_ERROR"
	}
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

type automationValidationError struct {
	msg string
}

func (e *automationValidationError) Error() string { return e.msg }

func automationValidationErrf(format string, args ...any) error {
	return &automationValidationError{msg: fmt.Sprintf(format, args...)}
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
	var autoValErr *automationValidationError
	if errors.As(err, &autoValErr) {
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
	case "run":
		fmt.Fprint(os.Stdout, `homepodctl run - execute a configured alias

Usage:
  homepodctl run <alias> [--json] [--plain] [--dry-run]

Notes:
  - Aliases come from config.json (see homepodctl aliases).
  - --dry-run resolves backend/rooms/targets without executing backend calls.
`)
	case "native-run":
		fmt.Fprint(os.Stdout, `homepodctl native-run - execute a Shortcut by name

Usage:
  homepodctl native-run --shortcut <name> [--json] [--dry-run]

Notes:
  - --dry-run validates arguments and prints the planned action only.
`)
	case "doctor":
		fmt.Fprint(os.Stdout, `homepodctl doctor - run environment and config diagnostics

Usage:
  homepodctl doctor [--json] [--plain]
`)
	case "completion":
		fmt.Fprint(os.Stdout, `homepodctl completion - generate shell completion scripts

Usage:
  homepodctl completion <bash|zsh|fish>
  homepodctl completion install <bash|zsh|fish> [--path <file-or-dir>]
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
		fmt.Fprint(os.Stdout, `homepodctl automation - declarative playback routines (v1)

Usage:
  homepodctl automation init --preset <morning|focus|winddown|party|reset> [--name <string>] [--json]
  homepodctl automation validate -f <file|-> [--json]
  homepodctl automation plan -f <file|-> [--json]
  homepodctl automation run -f <file|-> [--dry-run] [--json] [--no-input]

Notes:
  - run executes steps sequentially and stops on first failed step.
  - automation run is non-interactive by default (no confirmation prompt).
  - Use --dry-run to preview resolved actions without executing.
  - Use --json --no-input for agent-safe usage.
`)
	case "config":
		fmt.Fprint(os.Stdout, `homepodctl config - inspect and update config values

Usage:
  homepodctl config validate [--json]
  homepodctl config get <path> [--json]
  homepodctl config set <path> <value...>

Supported paths:
  defaults.backend
  defaults.shuffle
  defaults.volume
  defaults.rooms
  aliases.<name>.backend
  aliases.<name>.rooms
  aliases.<name>.playlist
  aliases.<name>.playlistId
  aliases.<name>.shuffle
  aliases.<name>.volume
  aliases.<name>.shortcut
  native.playlists.<room>.<playlist>
  native.volumeShortcuts.<room>.<0-100>
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

	if _, err := lookPath("osascript"); err != nil {
		add(doctorCheck{Name: "osascript", Status: "fail", Message: "osascript not found", Tip: "Install/restore macOS command-line tools."})
	} else {
		add(doctorCheck{Name: "osascript", Status: "pass", Message: "osascript available"})
	}
	if _, err := lookPath("shortcuts"); err != nil {
		add(doctorCheck{Name: "shortcuts", Status: "warn", Message: "shortcuts command not found", Tip: "Native backend requires the Shortcuts CLI."})
	} else {
		add(doctorCheck{Name: "shortcuts", Status: "pass", Message: "shortcuts available"})
	}

	path, err := configPath()
	if err != nil {
		add(doctorCheck{Name: "config-path", Status: "fail", Message: fmt.Sprintf("cannot resolve config path: %v", err)})
	} else {
		add(doctorCheck{Name: "config-path", Status: "pass", Message: path})
		cfg, cfgErr := loadConfigOptional()
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
	if _, err := getNowPlaying(backendCtx); err != nil {
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
	if len(args) == 0 {
		die(usageErrf("usage: homepodctl completion <bash|zsh|fish>\n       homepodctl completion install <bash|zsh|fish> [--path <file-or-dir>]"))
	}
	if args[0] == "install" {
		cmdCompletionInstall(args[1:])
		return
	}
	if len(args) != 1 {
		die(usageErrf("usage: homepodctl completion <bash|zsh|fish>\n       homepodctl completion install <bash|zsh|fish> [--path <file-or-dir>]"))
	}
	shell := strings.ToLower(strings.TrimSpace(args[0]))
	script, err := completionScript(shell)
	if err != nil {
		die(err)
	}
	fmt.Print(script)
}

func cmdCompletionInstall(args []string) {
	var shell string
	var path string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--path=") {
			path = strings.TrimSpace(strings.TrimPrefix(a, "--path="))
			continue
		}
		if a == "--path" {
			if i+1 >= len(args) {
				die(usageErrf("usage: homepodctl completion install <bash|zsh|fish> [--path <file-or-dir>]"))
			}
			i++
			path = strings.TrimSpace(args[i])
			continue
		}
		if strings.HasPrefix(a, "-") {
			die(usageErrf("unknown flag: %s", a))
		}
		if shell != "" {
			die(usageErrf("usage: homepodctl completion install <bash|zsh|fish> [--path <file-or-dir>]"))
		}
		shell = strings.ToLower(strings.TrimSpace(a))
	}
	if shell == "" {
		die(usageErrf("usage: homepodctl completion install <bash|zsh|fish> [--path <file-or-dir>]"))
	}
	installedPath, err := installCompletion(shell, path)
	if err != nil {
		die(err)
	}
	fmt.Printf("Installed %s completion: %s\n", shell, installedPath)
}

func completionInstallPath(shell string, override string) (string, error) {
	name, err := completionFileName(shell)
	if err != nil {
		return "", err
	}
	target := strings.TrimSpace(override)
	if target != "" {
		target = expandHomePath(target)
		base := filepath.Base(target)
		info, statErr := os.Stat(target)
		if statErr == nil && info.IsDir() {
			return filepath.Join(target, name), nil
		}
		if strings.HasSuffix(target, string(os.PathSeparator)) {
			return filepath.Join(target, name), nil
		}
		if statErr != nil && os.IsNotExist(statErr) && filepath.Ext(target) == "" && base != name {
			return filepath.Join(target, name), nil
		}
		return target, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch shell {
	case "bash":
		return filepath.Join(home, ".local", "share", "bash-completion", "completions", name), nil
	case "zsh":
		return filepath.Join(home, ".zsh", "completions", name), nil
	case "fish":
		return filepath.Join(home, ".config", "fish", "completions", name), nil
	default:
		return "", usageErrf("unknown shell %q (expected bash, zsh, or fish)", shell)
	}
}

func completionFileName(shell string) (string, error) {
	switch shell {
	case "bash":
		return "homepodctl", nil
	case "zsh":
		return "_homepodctl", nil
	case "fish":
		return "homepodctl.fish", nil
	default:
		return "", usageErrf("unknown shell %q (expected bash, zsh, or fish)", shell)
	}
}

func installCompletion(shell string, override string) (string, error) {
	target, err := completionInstallPath(shell, override)
	if err != nil {
		return "", err
	}
	script, err := completionScript(shell)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, []byte(script), 0o644); err != nil {
		return "", err
	}
	return target, nil
}

func expandHomePath(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
		return path
	}
	prefix := "~" + string(os.PathSeparator)
	if !strings.HasPrefix(path, prefix) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, prefix))
}

func completionData(cfg *native.Config) (aliases []string, rooms []string) {
	aliasSet := map[string]bool{}
	roomSet := map[string]bool{}
	if cfg == nil {
		return nil, nil
	}
	for name, a := range cfg.Aliases {
		if strings.TrimSpace(name) != "" {
			aliasSet[name] = true
		}
		for _, room := range a.Rooms {
			room = strings.TrimSpace(room)
			if room != "" {
				roomSet[room] = true
			}
		}
	}
	for _, room := range cfg.Defaults.Rooms {
		room = strings.TrimSpace(room)
		if room != "" {
			roomSet[room] = true
		}
	}
	for room := range cfg.Native.Playlists {
		if strings.TrimSpace(room) != "" {
			roomSet[room] = true
		}
	}
	for room := range cfg.Native.VolumeShortcuts {
		if strings.TrimSpace(room) != "" {
			roomSet[room] = true
		}
	}
	for a := range aliasSet {
		aliases = append(aliases, a)
	}
	for r := range roomSet {
		rooms = append(rooms, r)
	}
	sort.Strings(aliases)
	sort.Strings(rooms)
	return aliases, rooms
}

func joinBashWords(words []string) string {
	escaped := make([]string, 0, len(words))
	for _, w := range words {
		escaped = append(escaped, strings.ReplaceAll(w, " ", `\ `))
	}
	return strings.Join(escaped, " ")
}

func joinZshWords(words []string) string {
	quoted := make([]string, 0, len(words))
	for _, w := range words {
		quoted = append(quoted, "'"+strings.ReplaceAll(w, "'", `'\''`)+"'")
	}
	return strings.Join(quoted, " ")
}

func completionScript(shell string) (string, error) {
	cfg, _ := native.LoadConfigOptional()
	aliases, rooms := completionData(cfg)
	aliasBash := joinBashWords(aliases)
	roomBash := joinBashWords(rooms)
	aliasZsh := joinZshWords(aliases)
	roomZsh := joinZshWords(rooms)

	switch shell {
	case "bash":
		return fmt.Sprintf(`# bash completion for homepodctl
_homepodctl_completion() {
  local cur prev
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"
  local aliases="%s"
  local rooms="%s"
  local cmds="help version config automation completion doctor devices out playlists status now aliases run pause stop next prev play volume vol native-run config-init"
  if [[ $COMP_CWORD -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "$cmds --help --verbose" -- "$cur") )
    return 0
  fi
  if [[ "${COMP_WORDS[1]}" == "run" && $COMP_CWORD -eq 2 ]]; then
    COMPREPLY=( $(compgen -W "$aliases" -- "$cur") )
    return 0
  fi
  if [[ "$prev" == "--room" ]]; then
    COMPREPLY=( $(compgen -W "$rooms" -- "$cur") )
    return 0
  fi
  if [[ "${COMP_WORDS[1]}" == "out" && "${COMP_WORDS[2]}" == "set" ]]; then
    COMPREPLY=( $(compgen -W "$rooms" -- "$cur") )
    return 0
  fi
  COMPREPLY=( $(compgen -W "--json --plain --help --verbose --backend --room --playlist --playlist-id --shuffle --volume --watch --query --limit --shortcut --include-network --file --dry-run --no-input --preset --name" -- "$cur") )
}
complete -F _homepodctl_completion homepodctl
`, aliasBash, roomBash), nil
	case "zsh":
		return fmt.Sprintf(`#compdef homepodctl
_homepodctl() {
  local -a commands
  local -a opts
  local -a aliases
  local -a rooms
  commands=(
    'help:Show help'
    'version:Show version'
    'config:Inspect/update config'
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
  aliases=(%s)
  rooms=(%s)
  opts=(
    '--json[output JSON]'
    '--plain[plain output]'
    '--verbose[verbose diagnostics]'
    '--dry-run[preview without side effects]'
    '--backend[backend]:backend:(airplay native)'
    '--room[room name]'
    '--playlist[playlist name]'
    '--playlist-id[playlist ID]'
    '--shuffle[shuffle toggle]'
    '--volume[volume 0-100]'
    '--watch[poll interval]'
    '--query[playlist filter]'
    '--limit[max results]'
    '--shortcut[shortcut name]'
    '--include-network[include network address]'
    '--file[input file]'
    '--no-input[non-interactive mode]'
    '--preset[preset name]'
    '--name[routine name]'
  )
  if [[ $CURRENT -eq 3 && ${words[2]} == run ]]; then
    _describe -t aliases "alias" aliases
    return
  fi
  if [[ ${words[CURRENT-1]} == --room ]]; then
    _describe -t rooms "room" rooms
    return
  fi
  _arguments $opts '*::command:->command'
  case $state in
    command) _describe -t commands "homepodctl command" commands ;;
  esac
}
_homepodctl "$@"
`, aliasZsh, roomZsh), nil
	case "fish":
		var fish strings.Builder
		fish.WriteString(`# fish completion for homepodctl
complete -c homepodctl -f -a "help version config automation completion doctor devices out playlists status now aliases run pause stop next prev play volume vol native-run config-init"
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
`)
		for _, a := range aliases {
			fish.WriteString(fmt.Sprintf("complete -c homepodctl -n '__fish_seen_subcommand_from run' -a %q\n", a))
		}
		for _, r := range rooms {
			fish.WriteString(fmt.Sprintf("complete -c homepodctl -n '__fish_seen_argument --room' -a %q\n", r))
			fish.WriteString(fmt.Sprintf("complete -c homepodctl -n '__fish_seen_subcommand_from out; and __fish_seen_subcommand_from set' -a %q\n", r))
		}
		return fish.String(), nil
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
		opts, err := parseOutputOptions(flags)
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
		if opts.DryRun {
			writeActionOutput("out.set", opts.JSON, opts.Plain, actionOutput{
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
			writeActionOutput("out.set", opts.JSON, opts.Plain, actionOutput{
				Backend:    backend,
				Rooms:      rooms,
				NowPlaying: &np,
			})
		} else {
			writeActionOutput("out.set", opts.JSON, opts.Plain, actionOutput{
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
