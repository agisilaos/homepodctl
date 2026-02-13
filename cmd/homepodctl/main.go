package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
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
	newStatusTicker      = func(d time.Duration) statusTicker { return realStatusTicker{ticker: time.NewTicker(d)} }
	sleepFn              = time.Sleep
	verbose              bool
	jsonErrorOut         bool
)

type statusTicker interface {
	Chan() <-chan time.Time
	Stop()
}

type realStatusTicker struct {
	ticker *time.Ticker
}

func (t realStatusTicker) Chan() <-chan time.Time {
	return t.ticker.C
}

func (t realStatusTicker) Stop() {
	t.ticker.Stop()
}

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
  homepodctl plan <run|play|volume|vol|native-run|out set> [args]
  homepodctl schema [<name>] [--json]
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
	case "plan":
		cmdPlan(args)
	case "schema":
		cmdSchema(args)
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
	case "plan":
		fmt.Fprint(os.Stdout, `homepodctl plan - preview resolved command execution

Usage:
  homepodctl plan <run|play|volume|vol|native-run|out set> [args] [--json]

Notes:
  - plan executes the target command in dry-run JSON mode.
  - use --json for a machine-friendly envelope containing the planned action.
`)
	case "schema":
		fmt.Fprint(os.Stdout, `homepodctl schema - inspect machine-readable JSON contracts

Usage:
  homepodctl schema [<name>] [--json]

Examples:
  homepodctl schema
  homepodctl schema action-result --json
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
