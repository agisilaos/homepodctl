package main

import (
	"fmt"
	"os"

	"github.com/agisilaos/homepodctl/internal/native"
)

func usage() {
	fmt.Fprintf(os.Stderr, `homepodctl - control Apple Music + HomePods (macOS)

Usage:
  homepodctl [--verbose] [--quiet] --help
  homepodctl [--verbose] [--quiet] --version
  homepodctl [--verbose] [--quiet] <command> [args]
  homepodctl --help
  homepodctl --version
  homepodctl help [<command>]
  homepodctl version
  homepodctl config <validate|get|set> [args]
  homepodctl automation <run|validate|plan|init> [args]
  homepodctl plan <run|play|volume|vol|native-run|out set|automation run> [args]
  homepodctl schema [<name>] [--json]
  homepodctl completion <bash|zsh|fish>
  homepodctl completion install <bash|zsh|fish> [--path <file-or-dir>]
  homepodctl setup [--backend airplay|native] [--room <name> ...] [--json] [--no-input]
  homepodctl doctor [--json] [--plain]
  homepodctl devices [--json] [--plain] [--include-network]
  homepodctl out list [--json] [--plain] [--include-network]
  homepodctl out set [--room <name> ...] [<room> ...] [--backend airplay] [--json] [--plain] [--dry-run]
  homepodctl playlists [--query <substr>] [--limit N] [--json] [--plain]
  homepodctl status [--json] [--plain] [--watch <duration>]
  homepodctl now [--json] [--plain] [--watch <duration>]
  homepodctl aliases [--json] [--plain]
  homepodctl run <alias> [--json] [--plain] [--dry-run]
  homepodctl pause [--json] [--plain]
  homepodctl stop [--json] [--plain]
  homepodctl next [--json] [--plain]
  homepodctl prev [--json] [--plain]
  homepodctl play <playlist-query> [--backend airplay|native] [--room <name> ...] [--shuffle] [--volume 0-100] [--choose] [--no-input] [--json] [--plain] [--dry-run]
  homepodctl play --playlist <name> | --playlist-id <id> [--backend airplay|native] [--room <name> ...] [--shuffle] [--volume 0-100] [--choose] [--no-input] [--json] [--plain] [--dry-run]
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
  - --quiet suppresses non-essential human-readable success output.
  - exit codes: 0 success, 1 runtime failures, 2 usage/flag errors, 3 config/automation validation errors, 4 backend command failures outside setup diagnostics and automation execution.
  - setup diagnostic failures exit 1 and retain saved configuration, including in JSON and quiet modes.
  - automation execution failures always exit 1, including backend errors, missing preconditions, and timeouts.
`)
}

func cmdHelp(args []string) {
	_, pos, err := parseArgs("help", args)
	if err != nil {
		die(err)
	}
	if len(pos) > 1 {
		die(usageErrf("usage: homepodctl help [<command>]"))
	}
	args = pos
	if len(args) == 0 {
		usage()
		return
	}
	switch args[0] {
	case "devices":
		fmt.Fprint(os.Stdout, `homepodctl devices - list Music.app AirPlay outputs

Usage:
  homepodctl devices [--json] [--plain] [--include-network]

Notes:
  - Read-only discovery through Music.app; macOS may request Automation permission.
  - Device names are the room names used by --room. Listing does not select outputs.
  - available, selected and active are Music.app's device properties: availability,
    output selection and activity respectively. Selection alone does not prove playback.
  - --json emits an array with name, kind, available, selected, active, volume and persistentID.
  - --include-network adds networkAddress to JSON when present; it does not expand discovery.
  - --plain removes the table header. The table shows name, kind, available, selected and volume.
  - Use homepodctl status to inspect playback and the current output route.

Examples:
  homepodctl devices
  homepodctl devices --json
  homepodctl devices --json --include-network
`)
	case "status", "now":
		fmt.Fprint(os.Stdout, `homepodctl status - inspect Music.app playback and output routing

Usage:
  homepodctl status [--json] [--plain] [--watch <duration>]
  homepodctl now [--json] [--plain] [--watch <duration>]

Notes:
  - now is an alias for status. Reading status does not change playback or outputs.
  - Reports player state, track when present, selected outputs, route and Music/Automation connectivity.
  - --json emits a status object: ok, player, connection and optional track, volume, outputs and route.
  - outputs[].deviceName identifies the device; route contains the selected room names.
  - --plain uses labeled text. --watch repeats at a positive duration such as 1s; Ctrl-C stops it.
  - --watch with --json prints successive JSON objects, not a single JSON array.
  - Use homepodctl devices for available outputs or homepodctl doctor for permission diagnostics.

Examples:
  homepodctl status
  homepodctl now --json
  homepodctl status --watch 1s
`)
	case "play":
		fmt.Fprint(os.Stdout, `homepodctl play - play an Apple Music playlist

Usage:
  homepodctl play <playlist-query> [--backend airplay|native] [--room <name> ...] [--shuffle] [--volume 0-100] [--choose] [--no-input] [--json] [--plain] [--dry-run]
  homepodctl play --playlist <name> | --playlist-id <id> [--backend airplay|native] [--room <name> ...] [--shuffle] [--volume 0-100] [--choose] [--no-input] [--json] [--plain] [--dry-run]

Notes:
  - Supply exactly one target: positional query words, --playlist, or --playlist-id.
  - AirPlay searches Music.app user playlists; native uses an exact configured name (or looks up a name by ID).
  - If --room is omitted, homepodctl uses defaults.rooms from config.json; if that is empty it falls back to Music.app’s currently selected AirPlay outputs (airplay backend).
  - AirPlay --choose prompts only for multiple matches and requires interactive stdin without --no-input; direct IDs bypass selection.
  - --volume must be 0-100. AirPlay requires resolved rooms for explicit volume; a default volume without rooms is skipped.
  - Native play ignores volume, shuffle, and --choose after validating option values.
  - --dry-run shares argument/default validation and room inference, but skips playlist lookup, prompting, and native mapping checks.
  - AirPlay previews show effective shuffle and volume (including false and 0); volume is omitted when unchanged. Native previews omit both.

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
  homepodctl out set [--room <name> ...] [<room> ...] [--backend airplay] [--json] [--plain] [--dry-run]

Notes:
  - Room names must match the AirPlay device names shown by: homepodctl devices
  - out set changes Music.app’s current outputs; it does not modify config.json.
  - Prefer repeatable --room flags; positional rooms are kept for compatibility.

Examples:
  homepodctl out list
  homepodctl out set --room "Bedroom"
  homepodctl out set --room "Bedroom" --room "Living Room"
`)
	case "volume", "vol":
		fmt.Fprint(os.Stdout, `homepodctl volume - set output volume

Usage:
  homepodctl volume <0-100> [<room> ...] [--backend airplay|native] [--json] [--plain] [--dry-run]
  homepodctl vol <0-100> [<room> ...] [--backend airplay|native] [--json] [--plain] [--dry-run]

Notes:
  - If no rooms are provided, homepodctl uses defaults.rooms; if empty it uses Music.app’s currently selected outputs (airplay).
  - Previews and JSON results include the requested volume, including 0. Native previews do not check Shortcut mappings.

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
  - AirPlay previews include alias volume (falling back to defaults.volume) and explicitly configured alias shuffle; unset shuffle is unchanged.
  - Native playlist aliases and direct Shortcuts omit volume/shuffle because they do not apply those settings.
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
	case "setup":
		fmt.Fprint(os.Stdout, `homepodctl setup - onboard and verify local environment

Usage:
  homepodctl setup [--backend airplay|native] [--room <name> ...] [--json] [--no-input]

Notes:
  - Ensures config exists (same as config-init behavior).
  - Runs doctor checks and lists current AirPlay devices.
  - Optionally updates defaults via --backend and --room.
  - Exits 1 when diagnostics report ok=false, including in JSON and quiet modes.
  - Warnings alone are nonfatal. Saved configuration is retained on diagnostic failure.
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
  - This does not repair invalid config. Repair the file, or move it aside as a backup before creating a new starter config.
  - Edit defaults.rooms to your AirPlay device names (homepodctl devices).
`, path)
	case "automation":
		fmt.Fprint(os.Stdout, `homepodctl automation - declarative playback routines (v1)

Usage:
  homepodctl automation init --preset <morning|focus|winddown|party|reset> [--name <string>] [--json]
  homepodctl automation validate -f <file|-> [--json]
  homepodctl automation plan -f <file|-> [--json]
  homepodctl automation run -f <file|-> [--dry-run] [--json] [--no-input]

Flags:
  -f, --file <file|->  Read YAML/JSON from a file or stdin (required except for init).
      --dry-run        Preview run without backend calls; no short alias.
      --json           Emit one result object on stdout; pre-execution errors go to stderr.
      --no-input       Explicit non-interactive mode (run never prompts).
      --preset <name>  Select an init preset (required for init).
      --name <string>  Override the generated routine name.

Notes:
  - validate checks the file without applying config defaults or checking live state.
  - plan and run --dry-run compile the same offline recipe using file/config defaults.
  - Use --json to inspect resolved steps; human output shows a summary and step outcomes.
  - Playlist searches, native playlist-ID lookups, current-output inference, and native mapping checks happen only when the relevant step executes.
  - A successful preview does not guarantee devices, playlists, permissions, or Shortcuts are available.
  - run executes sequentially, stops on the first failed step, and marks later steps skipped.
  - resolved fields remain the offline recipe even after execution-time lookups.
  - init prints YAML; init --json returns preset, name, and YAML content (it does not write a file).

Exit codes:
  0  Success (including validation and previews).
  1  Runtime failure, including file read errors and any failed execution step.
  2  Usage/argument error.
  3  Config or automation file validation error.
  Backend errors, missing preconditions, and wait timeouts during execution exit 1, not 4.
`)
	case "plan":
		fmt.Fprint(os.Stdout, `homepodctl plan - preview resolved command execution

Usage:
  homepodctl plan <run|play|volume|vol|native-run|out set|automation run> [args] [--json]

Notes:
  - plan executes the target command in dry-run JSON mode.
  - automation planning supports only automation run in this mode.
  - plan automation run compiles an offline recipe; live lookups and preconditions are deferred until execution.
  - the automation result appears under plan in the envelope, with mode=dry-run.
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
