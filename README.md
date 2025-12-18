# homepodctl

<p align="center">
  <img src="assets/logo-lockup.png" width="1600" height="705" alt="homepodctl logo">
</p>

<p align="center">
  <a href="https://github.com/agisilaos/homepodctl/releases">
    <img src="https://img.shields.io/github/v/release/agisilaos/homepodctl?display_name=tag&sort=semver" alt="release">
  </a>
  <img src="https://img.shields.io/badge/platform-macOS-000000" alt="macOS">
  <img src="https://img.shields.io/badge/arch-arm64%20%7C%20amd64-informational" alt="arm64 and amd64">
</p>

macOS CLI that controls Apple Music playback and routes audio to HomePods.

## Requirements

- macOS with the Music app
- `osascript` (built-in)
- `shortcuts` (built-in, optional for the `native` backend)
- Go toolchain to build (if building from source)

## Permissions

On first use, macOS may prompt you to allow your terminal (or the built binary) to control:

- Music (via Apple Events)
- Shortcuts (if you use the `native` backend)

## Two playback backends

- `--backend airplay`: selects Music.app AirPlay output device(s) and plays a playlist (the Mac is the sender).
- `--backend native`: runs a Shortcuts automation you map in `config.json` (can be set up so HomePod plays natively).

## Mental model (important)

- **“Rooms” = AirPlay device names** as seen by Music.app (HomePods, Apple TVs, speakers, etc).
- `out set` changes **Music.app’s current AirPlay outputs** (it does not edit your config).
- `play` plays a **Music.app user playlist** (by fuzzy search or by ID).
- `config.json` is only for **defaults and aliases** (so you don’t have to type `--room` every time).

## Quick start (AirPlay)

List available AirPlay outputs (these names are what you pass as “rooms”):

```sh
homepodctl devices
```

Pick outputs to play through (sets Music.app’s current outputs):

```sh
homepodctl out set "Bedroom"
```

Play a playlist by fuzzy query:

```sh
homepodctl play chill
```

If the playlist name has spaces, quote it:

```sh
homepodctl play "Songs I've been obsessed recently pt. 2"
```

If multiple playlists match, auto-picks the best match; to pick interactively:

```sh
homepodctl play autumn --choose
```

See what’s playing (track/album/playlist + outputs):

```sh
homepodctl status
```

Shortcut for `status`:

```sh
homepodctl now
```

Watch changes:

```sh
homepodctl status --watch 1s
```

Search playlists (for IDs / debugging):

```sh
homepodctl playlists --query chill
```

If a playlist name is ambiguous or tricky to match (emoji/whitespace), use IDs:

```sh
homepodctl playlists --query autumn
homepodctl play --playlist-id <PERSISTENT_ID>
```

Set volume (if rooms are omitted, uses `defaults.rooms`; if that’s empty, uses the currently selected outputs in Music.app):

```sh
homepodctl vol 50
homepodctl volume 35 "Living Room"
```

## Config (defaults + aliases)

Create a starter config:

```sh
homepodctl config-init
```

This writes `config.json` under your macOS user config dir (typically `~/Library/Application Support/homepodctl/config.json`).

Defaults are used when flags are omitted. For example, if you set:

- `defaults.backend = "airplay"`
- `defaults.rooms = ["Bedroom"]`

…then you can just run:

```sh
homepodctl play chill
```

List configured aliases:

```sh
homepodctl aliases
```

Run an alias from your config:

```sh
homepodctl run bed-example
```

## Native backend (optional)

Edit `config.json`, map `room -> playlist -> shortcut name`, and run:

```sh
homepodctl play --backend native --room "Bedroom" --playlist "Example Playlist"
```

## Help

CLI help:

```sh
homepodctl --help
homepodctl help play
```

## Command cheat sheet

- `homepodctl devices` / `homepodctl out list`: list AirPlay devices
- `homepodctl out set <room> ...`: select Music.app outputs
- `homepodctl play <query>` / `homepodctl play --playlist-id <id>`: play a playlist
- `homepodctl playlists --query <text>`: search playlists
- `homepodctl status` / `homepodctl now` / `homepodctl status --watch 1s`: now playing
- `homepodctl pause|stop|next|prev`: transport controls
- `homepodctl volume <0-100> [room ...]` / `homepodctl vol ...`: output volume
- `homepodctl aliases` / `homepodctl run <alias>`: config shortcuts
- `homepodctl native-run --shortcut <name>`: run a Shortcut directly
- `homepodctl config-init`: create starter config
- `homepodctl version`: version info

## Common gotchas

- **You built it but it still behaves “old”:** if you run `make build`, the binary is `./homepodctl`. Running `homepodctl ...` might be a different binary on your PATH.
- **Rooms are not flags:** use `--room "Bedroom"` (repeatable), not `--bedroom` / `--Bedroom`.
- **`out set` doesn’t edit config:** it only changes Music.app’s current outputs. Use `config-init` + edit `defaults.rooms` if you want persistent defaults.

## Distribution

This tool is macOS-only (it relies on `osascript` + Music.app, and optionally `shortcuts`).

- **Homebrew (recommended):**
  - `brew tap agisilaos/tap`
  - `brew install homepodctl`
- **From source (recommended while iterating):** `make build`
- **Prebuilt binaries:** `make release VERSION=vX.Y.Z` publishes a GitHub Release and updates the Homebrew formula in `agisilaos/homebrew-tap`.
- **`go install` (after publishing):** `go install github.com/agisilaos/homepodctl/cmd/homepodctl@latest`

## Disclaimer

This project is not affiliated with Apple.
