package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

// setupPlaylistSuggestions returns a bounded, stable selection without changing
// the discovery result. A playlist needs both its display name and persistent ID
// to be useful in setup's suggested commands.
func setupPlaylistSuggestions(playlists []music.UserPlaylist) []music.UserPlaylist {
	valid := make([]music.UserPlaylist, 0, len(playlists))
	for _, playlist := range playlists {
		if strings.TrimSpace(playlist.Name) != "" && strings.TrimSpace(playlist.PersistentID) != "" {
			valid = append(valid, playlist)
		}
	}
	sort.SliceStable(valid, func(i, j int) bool {
		left, right := strings.ToLower(valid[i].Name), strings.ToLower(valid[j].Name)
		if left != right {
			return left < right
		}
		if valid[i].Name != valid[j].Name {
			return valid[i].Name < valid[j].Name
		}
		return valid[i].PersistentID < valid[j].PersistentID
	})
	if len(valid) > 3 {
		valid = valid[:3]
	}
	return valid
}

func setupGuidance(cfg *native.Config, devices []music.AirPlayDevice, playlists []music.UserPlaylist, discoveryOK, playlistDiscoveryOK bool) (next []string, warnings []string) {
	next = []string{"homepodctl status"}
	nativeBackend := cfg.Defaults.Backend == "native"
	if nativeBackend {
		next = append(next, "homepodctl help config", "homepodctl config validate")
		warnings = append(warnings, "Native playback requires a Shortcut mapping for each room and exact playlist name. Configure native.playlists as described in the README's Native backend section before playing.")
	}
	defaultPlaylistReady := cfg.Defaults.PlaylistID == ""
	if !nativeBackend && cfg.Defaults.PlaylistID != "" {
		if playlistDiscoveryOK {
			for _, playlist := range playlists {
				if playlist.PersistentID == cfg.Defaults.PlaylistID {
					defaultPlaylistReady = true
					break
				}
			}
			if !defaultPlaylistReady {
				warnings = append(warnings, fmt.Sprintf("Default playlist ID %q was not found in the library; its saved preference is preserved. List playlists or rerun setup --choose to select another playlist.", cfg.Defaults.PlaylistID))
			}
		} else {
			warnings = append(warnings, fmt.Sprintf("Could not verify default playlist ID %q because playlist discovery failed; its saved preference is preserved. List playlists and rerun setup --choose after restoring library access.", cfg.Defaults.PlaylistID))
		}
	}

	if !discoveryOK {
		next = append(next, "homepodctl devices", "homepodctl doctor --json")
		if !nativeBackend && (!playlistDiscoveryOK || !defaultPlaylistReady || len(setupPlaylistSuggestions(playlists)) == 0) {
			next = append(next, "homepodctl playlists")
		}
		if !nativeBackend && !defaultPlaylistReady {
			next = append(next, "homepodctl setup --choose")
		}
		return next, warnings
	}

	roomCounts := make(map[string]int)
	available := make(map[string]bool)
	destinationRank := make(map[string]int)
	for _, device := range devices {
		roomCounts[device.Name]++
		available[device.Name] = device.Available
		kind := strings.ToLower(strings.TrimSpace(device.Kind))
		switch {
		case strings.Contains(kind, "homepod"):
			destinationRank[device.Name] = 0
		case kind == "computer":
			destinationRank[device.Name] = 2
		default:
			destinationRank[device.Name] = 1
		}
	}
	roomsReady := true
	roomRecovery := "Check devices or rerun setup --choose to select available destinations."
	if nativeBackend {
		roomRecovery = "Check devices and update default rooms with setup --backend native --room <name>."
	}
	for _, room := range cfg.Defaults.Rooms {
		switch {
		case roomCounts[room] == 0:
			warnings = append(warnings, fmt.Sprintf("Default room %q was not discovered; its saved preference is preserved. %s", room, roomRecovery))
			roomsReady = false
		case roomCounts[room] > 1:
			warnings = append(warnings, fmt.Sprintf("Default room %q matches multiple devices; its saved preference is preserved. Rename the devices to distinct names before using this destination.", room))
			roomsReady = false
		case !available[room]:
			warnings = append(warnings, fmt.Sprintf("Default room %q is unavailable; its saved preference is preserved. %s", room, roomRecovery))
			roomsReady = false
		}
	}
	if !roomsReady {
		next = append(next, "homepodctl devices")
		if !nativeBackend {
			next = append(next, "homepodctl setup --choose")
		}
	}
	if nativeBackend {
		return next, warnings
	}

	// With no saved defaults, name an available destination explicitly so the
	// playback command also works before the suggested setup command is run.
	playRoom := ""
	if len(cfg.Defaults.Rooms) == 0 {
		var candidates []string
		for name, count := range roomCounts {
			if count == 1 && available[name] && strings.TrimSpace(name) != "" {
				candidates = append(candidates, name)
			}
		}
		sort.Slice(candidates, func(i, j int) bool {
			// Prefer HomePods for an onboarding example, with other outputs as fallback.
			// This only ranks suggestions; it never saves a default room.
			if destinationRank[candidates[i]] != destinationRank[candidates[j]] {
				return destinationRank[candidates[i]] < destinationRank[candidates[j]]
			}
			left, right := strings.ToLower(candidates[i]), strings.ToLower(candidates[j])
			if left != right {
				return left < right
			}
			return candidates[i] < candidates[j]
		})
		if len(candidates) > 0 {
			playRoom = candidates[0]
			next = append(next, "homepodctl setup --room="+bashArrayLiteral([]string{playRoom}))
		} else {
			roomsReady = false
			warnings = append(warnings, "No available destination with a unique name was discovered. Check device availability and give destinations distinct names before selecting default rooms.")
			next = append(next, "homepodctl devices", "homepodctl doctor --json")
		}
	}

	if !playlistDiscoveryOK || !defaultPlaylistReady {
		next = append(next, "homepodctl playlists")
		if cfg.Defaults.PlaylistID != "" && !slices.Contains(next, "homepodctl setup --choose") {
			next = append(next, "homepodctl setup --choose")
		}
		return next, warnings
	}
	if cfg.Defaults.PlaylistID != "" {
		if roomsReady {
			command := "homepodctl play"
			if playRoom != "" {
				command += " --room=" + bashArrayLiteral([]string{playRoom})
			}
			next = append([]string{command}, next...)
		}
		return next, warnings
	}

	suggestions := setupPlaylistSuggestions(playlists)
	if len(suggestions) == 0 {
		next = append(next, "homepodctl playlists")
		return next, warnings
	}
	if roomsReady {
		for _, playlist := range suggestions {
			command := "homepodctl play --backend airplay --playlist-id=" + bashArrayLiteral([]string{playlist.PersistentID})
			if playRoom != "" {
				command += " --room=" + bashArrayLiteral([]string{playRoom})
			}
			next = append(next, command)
		}
	}
	return next, warnings
}
