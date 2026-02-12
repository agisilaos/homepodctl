# Changelog

All notable changes to this project will be documented in this file.

The format is based on *Keep a Changelog*, and this project adheres to *Semantic Versioning*.

## [Unreleased]

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
