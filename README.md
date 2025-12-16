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
- Go toolchain to build

## Permissions

On first use, macOS may prompt you to allow your terminal (or the built binary) to control:

- Music (via Apple Events)
- Shortcuts (if you use the `native` backend)

## Two playback backends

- `--backend airplay`: selects Music.app AirPlay output device(s) and plays a playlist (the Mac is the sender).
- `--backend native`: runs a Shortcuts automation you map in `config.json` (can be set up so HomePod plays natively).

## Quick start

Initialize config (sets defaults like backend/rooms/volume):

```sh
homepodctl config-init
```

List available HomePods (AirPlay devices):

```sh
homepodctl devices
```

List outputs (alias of devices):

```sh
homepodctl out list
```

Select output(s) (uses `defaults.rooms` when omitted):

```sh
homepodctl out set "Living Room"
homepodctl out set Bedroom "Living Room"
```

Search playlists:

```sh
homepodctl playlists --query chill
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

Play a playlist (uses defaults from `config.json` when flags are omitted):

```sh
homepodctl play chill
```

If multiple playlists match, auto-pick the best match (prints what it picked). To pick interactively:

```sh
homepodctl play autumn --choose
```

If a playlist name is ambiguous or tricky to match (emoji/whitespace), use IDs:

```sh
homepodctl playlists --query autumn
homepodctl play --backend airplay --room "Bedroom" --playlist-id <PERSISTENT_ID>
```

Set volume (uses `defaults.rooms` when omitted):

```sh
homepodctl vol 50
homepodctl volume 35 "Living Room"
```

Native backend (optional): edit the file printed by `config-init`, map `room -> playlist -> shortcut name`, and run:

```sh
homepodctl play --backend native --room "Bedroom" --playlist "Example Playlist"
```

Run an alias from your config:

```sh
homepodctl run bed-example
```

List aliases:

```sh
homepodctl aliases
```

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
