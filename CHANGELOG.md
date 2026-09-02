# Changelog

All notable changes to this project will be documented in this file.

The format is based on *Keep a Changelog*, and this project adheres to *Semantic Versioning*.

## [v0.4.0] - 2026-09-02

### Added

- Added the Preview `homepodctl tui` dashboard for Music.app now-playing details, complete AirPlay Room state, transport controls, staged multi-room routing, and focused-room volume changes.

### Changed

- Playback commands and the TUI now share one in-process application boundary. Interactive snapshots require complete device state while existing status and action output contracts retain their selected-output behavior.
- The TUI keeps retrying after backend failures, marks retained snapshots stale, disables mutations until state is current, honors `NO_COLOR`, and distinguishes Music/AirPlay observation from unobservable native Shortcut playback.

## [v0.3.0] - 2026-08-31

### Changed

- Commands now reject unrelated flags instead of silently ignoring them. Scripts that passed options to the wrong command must remove or relocate those options. Boolean parsing is shared across commands, config edits, and environment values. ([#13](https://github.com/agisilaos/homepodctl/pull/13))
- `play` now requires exactly one playlist target: positional query, `--playlist`, or `--playlist-id`. Competing targets and volumes outside 0–100 are rejected on both backends, including during previews. ([#7](https://github.com/agisilaos/homepodctl/pull/7))
- JSON consumers should read native playback ID previews from `playlistId`, not `playlist`, and expect the `plan` command's top-level `args` to show canonical `--dry-run=true` and `--json=true` flags. ([#7](https://github.com/agisilaos/homepodctl/pull/7), [#2](https://github.com/agisilaos/homepodctl/pull/2))
- Bash, Zsh, and Fish completions now share command-specific flags and value suggestions, including `--choose`, supported nested `plan` targets, and short aliases. Regenerate installed completions after upgrading with `homepodctl completion install <bash|zsh|fish>`. ([#14](https://github.com/agisilaos/homepodctl/pull/14))

### Fixed

- `plan` cannot be switched out of dry-run JSON mode by target flags. Previews preserve literal arguments after `--` and flag-looking shortcut or playlist values. JSON errors respect explicit false values, repeated flags, and argument boundaries. ([#2](https://github.com/agisilaos/homepodctl/pull/2), [#13](https://github.com/agisilaos/homepodctl/pull/13))
- Configuration-derived completion values are escaped for each shell so special characters cannot corrupt or execute generated scripts. Malformed config and values a shell cannot represent now produce errors instead of incomplete scripts. ([#3](https://github.com/agisilaos/homepodctl/pull/3))
- Multi-room native playlist and volume actions check all required Shortcut mappings before running any of them, preventing missing mappings from causing partial execution. Runtime Shortcut failures still stop subsequent actions without rolling back earlier ones. ([#5](https://github.com/agisilaos/homepodctl/pull/5))
- Playback previews and execution share request validation, including AirPlay's requirement for rooms when setting an explicit volume. Native execution retains the persistent playlist ID alongside its resolved name. ([#7](https://github.com/agisilaos/homepodctl/pull/7))
- Automation previews and execution share one resolved plan. Run results retain resolved details for successful, failed, and skipped steps and report actual execution timing; live playlist and output lookups remain execution-time operations. Help and documentation now describe the matching JSON and exit-code contracts. ([#6](https://github.com/agisilaos/homepodctl/pull/6), [#12](https://github.com/agisilaos/homepodctl/pull/12))
- Invalid setup options fail before reading or creating config. `setup`, `config set`, and `config-init` share configuration persistence, and write failures consistently return config error code 3. ([#9](https://github.com/agisilaos/homepodctl/pull/9), [#10](https://github.com/agisilaos/homepodctl/pull/10))
- Interrupted release checks restore module files. Publication failures report the stopped step and uncertain command outcomes, retain recovery artifacts, and provide safe manual recovery instructions. Automatic resume remains intentionally unsupported. ([#11](https://github.com/agisilaos/homepodctl/pull/11), [#15](https://github.com/agisilaos/homepodctl/pull/15))

### Development

- Continuous verification no longer depends on a publishable version. CLI subprocess tests isolate configuration and verify stdout and stderr separately; release fixtures cover interrupted checks, partial publication, and ambiguous remote outcomes. ([#4](https://github.com/agisilaos/homepodctl/pull/4), [#8](https://github.com/agisilaos/homepodctl/pull/8), [#15](https://github.com/agisilaos/homepodctl/pull/15))

## [v0.2.0] - 2026-02-23

### Added

- Added `homepodctl setup` for first-run onboarding.

### Changed

- Added global `--quiet` mode to suppress non-essential success output in automation flows.
- Preferred `--room` for `out set` while retaining positional argument compatibility.
- Improved `status --watch` readability with timestamped snapshots.
- Improved CLI/global `--version` behavior for consistent non-interactive usage.
- Expanded integration and command-surface coverage while continuing command handler refactors.
- Standardized release-check/help-snapshot/docs workflow contracts and repository docs structure.

### Fixed

- Fixed `out set` backend defaulting so it resolves to `airplay` when unset.
- Removed non-portable `rg` dependency from release checks.

## [v0.1.4] - 2026-02-14

- feat: improve automation docs, completion UX, and backend resilience (2705777)
- feat(plan): support automation run previews (ed9e981)
- feat(cli): add schema and plan commands for agent-friendly automation (695430e)
- refactor(cli): split playback and doctor/completion handlers from main (ec02ccc)

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
