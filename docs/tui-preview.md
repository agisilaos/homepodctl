# TUI Preview

`homepodctl tui` is a keyboard-driven view of Music.app playback and the AirPlay Rooms that Music.app reports. It is a Preview feature: its keybindings and layout may evolve, while existing non-interactive CLI output contracts remain unchanged.

## Observation boundary

The interface observes one Music.app playback session and its AirPlay device list. It shows the current track, artist, album, playlist, position, duration, shuffle, repeat, and every available or unavailable Room with its selected, active, and volume state.

A selected output belongs to Music.app's playback route; selection alone is not proof that the device is audibly playing. The separate active marker reports Music.app's active flag. Native playback started on a HomePod through Siri, another source device, or a configured Shortcut is not observable through this interface. When `defaults.backend` is `native`, the TUI displays that limitation rather than presenting Music.app state as native state.

## Start and refresh

```sh
homepodctl tui
homepodctl tui --refresh 2s
```

The default refresh interval is two seconds and the minimum is 500 milliseconds. Each refresh and mutation has its own five-second timeout; the TUI session itself stays open until the user quits. Interactive stdin and stdout are required. Manual refreshes and post-action confirmation remain immediate.

If a refresh fails, the most recent successful snapshot remains visible and is marked stale. Mutating controls stay disabled until a current snapshot arrives. Starting while Music is unavailable opens a disconnected view and keeps retrying.

## Keys

| Key | Action |
| --- | --- |
| Space | Play or pause the current Music session |
| `n` / `b` | Next or previous track |
| `s` | Stop playback |
| Up/Down or `j`/`k` | Focus a Room |
| `x` | Stage the focused Room into or out of the pending route |
| Enter | Apply the pending route |
| `+` / `-` | Adjust focused Room volume by five |
| `r` | Refresh immediately |
| `?` | Toggle help |
| `q` or Ctrl-C | Quit |

Route edits are staged and applied together. The TUI rejects an empty route. Immediately before applying, it checks the route again; after requesting a change, it waits for Music to confirm the new route. If the observed route changes elsewhere while edits are pending, it resets those edits instead of overwriting the newer route.

Space only toggles the current Music session. When Music has no resumable track, the TUI directs the user to start one with `homepodctl play` rather than choosing content automatically.

## Presentation and diagnostics

The Midnight + Music palette reserves pink for playback, blue for focus and routing, yellow for pending changes, orange for errors or unavailable Rooms, and green for live connectivity. Text and symbols duplicate every color-coded state. Set `NO_COLOR` to disable color.

Global `--verbose` adds the most recent operation and duration to the status line without writing diagnostics over the alternate screen. Global `--quiet` suppresses routine success notices but never hides errors, stale-state warnings, or observation-boundary messages.

The layout condenses as the terminal narrows. Below 48 columns or 14 rows it shows a minimum-size message instead of rendering a misleading or clipped dashboard. The alternate screen is restored when the program exits.
