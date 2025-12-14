# Changelog

All notable changes to this project will be documented in this file.

The format is based on *Keep a Changelog*, and this project adheres to *Semantic Versioning*.

## [Unreleased]

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
