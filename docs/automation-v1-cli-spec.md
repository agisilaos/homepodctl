# homepodctl Automation v1 CLI Spec

Status: implemented for v1 (run/validate/plan/init).

For authoritative command help, run `homepodctl help automation` or read its
[generated snapshot](help/automation.txt). The [plan help snapshot](help/plan.txt)
covers the top-level wrapper. Regenerate snapshots with `scripts/update-help.sh`;
`scripts/check-help.sh` and `scripts/docs-check.sh` enforce them.

## One-liner

Declarative playback routines for HomePod + Apple Music, optimized for both humans and non-interactive agents.

## Design principles

- HomePod-specific, not a generic workflow engine.
- Small surface area, deterministic behavior.
- Stable machine contract with `--json`.
- No hidden prompts in automation execution.

## Command tree

```text
homepodctl automation run -f <file|-> [--dry-run] [--json] [--no-input]
homepodctl automation validate -f <file|-> [--json]
homepodctl automation plan -f <file|-> [--json]
homepodctl automation init --preset <morning|focus|winddown|party|reset> [--name <string>] [--json]
```

## Usage and flags

### `homepodctl automation run`

Purpose: validate + resolve + execute an automation file.

```text
Usage:
  homepodctl automation run -f <file|-> [--dry-run] [--json] [--no-input]

Flags:
  -f, --file <path|->   Automation YAML/JSON path, or "-" for stdin (required)
      --dry-run         Print an offline recipe without backend calls (no short alias)
      --json            Emit single JSON object to stdout
      --no-input        Explicit non-interactive mode (automation is non-interactive by default)
```

### `homepodctl automation validate`

Purpose: check the automation file without applying config defaults or checking live state.

```text
Usage:
  homepodctl automation validate -f <file|-> [--json]
```

### `homepodctl automation plan`

Purpose: compile an offline recipe with file/config defaults (no backend calls).
Use `--json` to inspect resolved step fields; human output shows step outcomes only.

```text
Usage:
  homepodctl automation plan -f <file|-> [--json]
```

### `homepodctl automation init`

Purpose: generate a starter routine from a canonical preset.

```text
Usage:
  homepodctl automation init --preset <name> [--name <string>] [--json]

Flags:
      --preset <name>   One of: morning, focus, winddown, party, reset (required)
      --name <string>   Override routine name in emitted file
      --json            Print preset, name, and YAML content in one object
```

Without `--json`, init prints YAML to stdout. It never writes a file itself;
redirect stdout to save the routine.

## Automation file format (v1)

Supported file types: YAML or JSON.

```yaml
version: "1"
name: morning
defaults:
  backend: airplay
  rooms: ["Bedroom"]
  volume: 30
  shuffle: false
steps:
  - type: out.set
    rooms: ["Bedroom"]
  - type: play
    query: "Morning Jazz"
  - type: volume.set
    value: 30
  - type: wait
    state: playing
    timeout: 20s
```

### Top-level keys

- `version`: required, must equal `"1"`.
- `name`: required, non-empty.
- `defaults`: optional.
- `steps`: required, non-empty ordered array.

### `defaults`

- `backend`: `airplay` or `native`.
- `rooms`: array of device names.
- `volume`: integer `0..100`.
- `shuffle`: boolean.

### Step types (only these in v1)

- `out.set`: select current outputs.
  - required: `rooms` (non-empty list)
- `play`: start playlist.
  - required: exactly one of `query` or `playlistId`
- `volume.set`: set volume.
  - required: `value` (`0..100`)
  - optional: `rooms` (if omitted, fallback rules apply)
- `wait`: wait for player state.
  - required: `state` (`playing|paused|stopped`)
  - required: `timeout` (`1s` to `10m`)
- `transport`:
  - required: `action`
  - allowed action in v1: `stop`

Not supported in v1: branching, retries, loops, conditions, arbitrary scripts.

## Resolution and execution semantics

- `plan`, `run --dry-run`, and `run` compile the same offline recipe before any step executes. Planning makes no Music or Shortcuts calls, waits, or prompts.
- File defaults override `config.json` defaults; an absent backend falls back to `airplay`. `out.set` requires its own rooms. `volume.set` uses its step rooms, then file/config rooms, and always requires its own value. `play` uses file/config rooms, volume, and shuffle.
- `validate` checks the file using file/built-in defaults, without merging config defaults. All automation CLI subcommands currently load config first, so a malformed config can still prevent validation, planning, or init.
- AirPlay `play` searches for a query and selects the best match only when that step runs; a `playlistId` bypasses the search. Without default rooms, it leaves current outputs unchanged and skips default volume. Native `play` resolves a playlist ID to a name at execution time and ignores volume/shuffle defaults.
- AirPlay `volume.set` with no step or default rooms infers the currently selected outputs when it executes, so an earlier `out.set` can affect it. Planning does not fill in those rooms.
- Native mappings are captured from config when the plan is compiled, but required rooms and mappings are checked only at the relevant execution step. A missing mapping can fail after earlier steps have succeeded.
- `wait` observes Music state and `transport stop` controls Music even if the resolved backend metadata is `native`. `out.set` requires `airplay` at execution time.
- Execution is sequential and fail-fast: the first failed step stops execution, later steps are marked skipped, and earlier changes are not rolled back.
- A successful preview does not establish device, playlist, permission, or Shortcut availability. The `resolved` payload remains the offline recipe in run results; it is not rewritten with live lookup results.
- `homepodctl plan automation run -f <file> --json` wraps the dry-run result under `plan` in the top-level plan envelope. Its nested mode is `dry-run`; direct `automation plan` uses mode `plan`.

## Output contract

- Human mode:
  - summary (`automation name="morning" mode=run ok=true steps=4`), then step outcomes (`1/4 out.set ok=true`), emitted after completion
  - resolved payloads and step error details require `--json`
- JSON mode:
  - on success or execution failure, exactly one result document on stdout
  - usage, file read, config, and validation errors before execution leave stdout empty and emit an error envelope on stderr (`ok`, `error.code`, `error.message`, `error.exitCode`)
  - execution errors appear in the failed step's `error`; no separate stderr error envelope is emitted for a failed run
  - diagnostics and warnings stay on stderr

Previews have zero durations and equal start/end timestamps. Their `ok=true`
means compilation succeeded, not that steps ran. Successful steps omit `error`.

### JSON shape (stable contract)

```json
{
  "name": "morning",
  "version": "1",
  "mode": "run",
  "ok": true,
  "startedAt": "2026-02-12T21:00:00Z",
  "endedAt": "2026-02-12T21:00:02Z",
  "durationMs": 2012,
  "steps": [
    {
      "index": 0,
      "type": "out.set",
      "input": {"type": "out.set", "rooms": ["Bedroom"]},
      "resolved": {"backend": "airplay", "rooms": ["Bedroom"]},
      "ok": true,
      "skipped": false,
      "durationMs": 210
    }
  ]
}
```

## Exit codes

| Code | Automation contract |
| --- | --- |
| `0` | Success, including validation and previews. |
| `1` | Runtime failure: unreadable/missing input file, or any failed execution step (including backend errors, missing rooms/mappings/devices, permissions, wait timeouts, and cancellation). |
| `2` | Usage/argument error, such as missing `--file`, invalid flag values, or unsupported `-n`. |
| `3` | Config loading or automation file validation failure (invalid YAML/JSON or invalid routine fields). |

Exit `4` is the CLI's backend-error code outside automation execution. Automation
run results aggregate failures and always exit `1` when `ok=false`; they do not
classify failed steps into separate exit codes. These codes apply to direct
`automation` commands. The top-level `plan` wrapper reports a child-command
failure as a generic error with exit `1`.

## Example commands

```sh
homepodctl automation init --preset morning > morning.yaml
homepodctl automation validate -f morning.yaml
homepodctl automation plan -f morning.yaml --json
homepodctl automation run -f morning.yaml --dry-run
homepodctl automation run -f morning.yaml --json --no-input
homepodctl automation validate -f - < morning.yaml
```
