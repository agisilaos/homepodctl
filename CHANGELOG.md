# Changelog

All notable changes to this project will be documented in this file.

The format is based on *Keep a Changelog*, and this project adheres to *Semantic Versioning*.

## [Unreleased]

## [v0.1.3] - 2026-02-13

### Added

- diagnostics: global `--verbose` flag (or `HOMEPODCTL_VERBOSE=1`) to print command/backend resolution details to stderr.
- CLI: `homepodctl plan` for agent-friendly previews that resolve and return dry-run JSON plans for `run`, `play`, `volume/vol`, `native-run`, and `out set`.
- CLI: `homepodctl schema` to list/emit JSON schemas for stable machine output contracts.
- CLI: `homepodctl aliases --json` for machine-readable alias listings.
- CLI: `homepodctl doctor` command with `--json`/`--plain` output for environment/config/backend diagnostics.
- CLI: `homepodctl completion <bash|zsh|fish>` to generate shell completion scripts.
- CLI: `homepodctl config validate|get|set` for config inspection/editing of `defaults.*`.

### Changed

- errors/exit-codes: standardized process exit codes:
  - `2` usage/flag validation issues
  - `3` config read/parse/write failures
  - `4` backend execution failures (`osascript`/`shortcuts`)
  - `1` generic runtime failures
- output: added `--json` support to `run`, `play`, `volume`, `out set`, transport commands, and `native-run`.
- output: added `--plain` support across status/aliases/playlists and action commands for stable script-friendly text mode.
- safety: added `--dry-run` to mutating commands (`run`, `play`, `volume/vol`, `out set`, `native-run`) with structured action previews.
- errors: common AppleScript backend failures now map to concise user-friendly messages in non-verbose mode.
- automation: `homepodctl automation run` now executes steps (not only dry-run), stops on first failure, and marks remaining steps as skipped.
- completion: generated scripts now include config-derived alias/room suggestions for `run` and room-targeted commands.

## [v0.1.2] - 2026-02-12

### Changed

- docs/cli: expanded in-tool help via `homepodctl help <command>` and refreshed README quick-start/mental-model sections.
- airplay UX: when rooms are omitted and `defaults.rooms` is empty, `play`/`volume` now fall back to currently selected Music.app outputs where possible.
- errors: improved unknown-flag and missing-room guidance to point users to `homepodctl help`, `homepodctl devices`, and `config-init`.

### Fixed

- help examples now render quotes correctly (no escaped `\"` sequences in `homepodctl help` output).

## [v0.1.1] - 2025-12-14

- chore(release): normalize tap README install section (aab2e89)
- fix(release): avoid zsh trap nounset error (a552b27)

## [v0.1.0] - 2025-12-14

### Added

- Initial `homepodctl` CLI.
- AirPlay backend via Music.app AppleScript (output selection, playlist playback, volume).
- Native backend via Shortcuts (`shortcuts run`) using config mappings.
- `status` output (track/album/playlist + outputs) and `--watch` polling.
- Config defaults and aliases (`aliases`, `run`), plus interactive playlist selection (`--choose`).
