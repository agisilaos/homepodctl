package main

import (
	"fmt"
	"os"

	"github.com/agisilaos/homepodctl/internal/native"
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
  homepodctl plan <run|play|volume|vol|native-run|out set|automation run> [args]
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
  homepodctl plan <run|play|volume|vol|native-run|out set|automation run> [args] [--json]

Notes:
  - plan executes the target command in dry-run JSON mode.
  - automation planning supports only automation run in this mode.
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
