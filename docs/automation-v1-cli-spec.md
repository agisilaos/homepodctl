# homepodctl Automation v1 CLI Spec

Status: implemented for v1 (run/validate/plan/init).

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
  -n, --dry-run         Print resolved execution with no state changes
      --json            Emit single JSON object to stdout
      --no-input        Explicit non-interactive mode (automation is non-interactive by default)
  -h, --help            Show help
```

### `homepodctl automation validate`

Purpose: schema and semantic checks only (no state changes).

```text
Usage:
  homepodctl automation validate -f <file|-> [--json]
```

### `homepodctl automation plan`

Purpose: print resolved steps and defaults precedence (no state changes).

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
      --json            Print metadata (preset + output path/target)
```

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

- Precedence: step fields > file defaults > `config.json` defaults > built-in defaults.
- Execution is sequential and fail-fast.
- `run --dry-run` performs full resolution but zero state changes.
- `plan` and `run --dry-run` must resolve to the same step plan.

## Output contract

- Human mode:
  - concise progress lines (`1/4 play ... ok`)
  - final summary (`ok=true`, elapsed)
- JSON mode:
  - exactly one JSON document on stdout
  - diagnostics and warnings stay on stderr

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
      "input": {"rooms": ["Bedroom"]},
      "resolved": {"backend": "airplay", "rooms": ["Bedroom"]},
      "ok": true,
      "skipped": false,
      "error": "",
      "durationMs": 210
    }
  ]
}
```

## Exit codes

- `0`: success
- `1`: runtime execution failure
- `2`: usage/argument error
- `3`: automation file validation failure
- `4`: unmet precondition (permission/device/timeout)

## Example commands

```sh
homepodctl automation init --preset morning > morning.yaml
homepodctl automation validate -f morning.yaml
homepodctl automation plan -f morning.yaml --json
homepodctl automation run -f morning.yaml --dry-run
homepodctl automation run -f morning.yaml --json --no-input
homepodctl automation validate -f - < morning.yaml
```
