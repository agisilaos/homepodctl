package main

import (
	"bufio"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

type setupSelection struct {
	Rooms        []string
	RoomsChanged bool
	Playlist     *music.UserPlaylist
}

// chooseSetup collects preferences without saving configuration or changing
// playback. The caller saves only after every optional selection completes.
func chooseSetup(in io.Reader, out io.Writer, cfg *native.Config, devices []music.AirPlayDevice, playlists []music.UserPlaylist, skipRooms, skipPlaylist bool) (setupSelection, error) {
	reader := bufio.NewReader(in)
	selection := setupSelection{Rooms: append([]string(nil), cfg.Defaults.Rooms...)}
	if !skipRooms {
		rooms, err := chooseSetupRooms(reader, out, cfg.Defaults.Rooms, devices)
		if err != nil {
			return setupSelection{}, err
		}
		selection.Rooms = rooms
		selection.RoomsChanged = !slices.Equal(rooms, cfg.Defaults.Rooms)
	}
	if skipPlaylist {
		return selection, nil
	}
	if cfg.Defaults.PlaylistID != "" {
		fmt.Fprintf(out, "Saved default playlist ID: %q. Skip to keep it, or select a replacement.\n", cfg.Defaults.PlaylistID)
	}
	playlist, err := chooseSetupPlaylist(reader, out, playlists)
	if err != nil {
		return setupSelection{}, err
	}
	selection.Playlist = playlist
	return selection, nil
}

func chooseSetupRooms(reader *bufio.Reader, out io.Writer, defaults []string, devices []music.AirPlayDevice) ([]string, error) {
	ordered := append([]music.AirPlayDevice(nil), devices...)
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if strings.ToLower(a.Name) != strings.ToLower(b.Name) {
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.PersistentID < b.PersistentID
	})
	counts := make(map[string]int, len(devices))
	for _, device := range devices {
		counts[device.Name]++
	}
	var choices []string
	fmt.Fprintln(out, "Discovered destinations (saved defaults are preferences):")
	for _, device := range ordered {
		status := "unavailable"
		if device.Available {
			status = "available"
		}
		if slices.Contains(defaults, device.Name) {
			status += ", saved default"
		}
		eligible := device.Available && strings.TrimSpace(device.Name) != "" && counts[device.Name] == 1
		if counts[device.Name] > 1 {
			status += ", duplicate name: cannot select by name"
		} else if strings.TrimSpace(device.Name) == "" {
			status += ", missing name: cannot select"
		}
		if eligible {
			choices = append(choices, device.Name)
			fmt.Fprintf(out, "  %d) %q (%s)\n", len(choices), device.Name, status)
		} else {
			fmt.Fprintf(out, "  -  %q (%s)\n", device.Name, status)
		}
	}
	for _, room := range defaults {
		if counts[room] == 0 {
			fmt.Fprintf(out, "  -  %q (saved default, not discovered)\n", room)
		}
	}
	if len(choices) == 0 {
		fmt.Fprintln(out, "No available destinations can be selected by name. Keeping saved default rooms.")
		return append([]string(nil), defaults...), nil
	}
	for {
		fmt.Fprint(out, "Default rooms: enter numbers separated by commas or spaces, Enter to keep current defaults, or q to cancel: ")
		line, err := readSetupChoice(reader)
		if err != nil {
			return nil, err
		}
		if line == "" {
			return append([]string(nil), defaults...), nil
		}
		fields := strings.FieldsFunc(line, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
		var rooms []string
		valid := len(fields) > 0
		for _, field := range fields {
			n, ok := setupChoiceNumber(field, len(choices))
			if !ok {
				valid = false
				break
			}
			if !slices.Contains(rooms, choices[n-1]) {
				rooms = append(rooms, choices[n-1])
			}
		}
		if valid {
			return rooms, nil
		}
		fmt.Fprintf(out, "Invalid room selection. Use numbers from 1 to %d; Enter keeps current defaults.\n", len(choices))
	}
}

func chooseSetupPlaylist(reader *bufio.Reader, out io.Writer, playlists []music.UserPlaylist) (*music.UserPlaylist, error) {
	ordered := make([]music.UserPlaylist, 0, len(playlists))
	for _, playlist := range playlists {
		if strings.TrimSpace(playlist.PersistentID) != "" && strings.TrimSpace(playlist.Name) != "" {
			ordered = append(ordered, playlist)
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if strings.ToLower(a.Name) != strings.ToLower(b.Name) {
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.PersistentID < b.PersistentID
	})
	if len(ordered) == 0 {
		fmt.Fprintln(out, "No library playlists with persistent IDs were found. Keeping the saved playlist preference.")
		return nil, nil
	}
	fmt.Fprintln(out, "Choose a default library playlist for homepodctl play (optional; skipping preserves your saved playlist).")
	for {
		fmt.Fprint(out, "Playlist search: enter text, Enter to skip, or q to cancel: ")
		query, err := readSetupChoice(reader)
		if err != nil {
			return nil, err
		}
		if query == "" {
			return nil, nil
		}
		var matches []music.UserPlaylist
		for _, playlist := range ordered {
			if strings.Contains(strings.ToLower(playlist.Name), strings.ToLower(query)) {
				matches = append(matches, playlist)
			}
		}
		if len(matches) == 0 {
			fmt.Fprintf(out, "No library playlists match %q. Try another search or press Enter to skip.\n", query)
			continue
		}
		if len(matches) > 20 {
			fmt.Fprintf(out, "Showing the first 20 of %d matches; search again to narrow the list.\n", len(matches))
			matches = matches[:20]
		}
		for i, playlist := range matches {
			fmt.Fprintf(out, "  %d) %q (ID %q)\n", i+1, playlist.Name, playlist.PersistentID)
		}
		for {
			fmt.Fprint(out, "Playlist: enter a number, s to skip, Enter to search again, or q to cancel: ")
			line, err := readSetupChoice(reader)
			if err != nil {
				return nil, err
			}
			if line == "" {
				break
			}
			if strings.EqualFold(line, "s") {
				return nil, nil
			}
			n, ok := setupChoiceNumber(line, len(matches))
			if !ok {
				fmt.Fprintf(out, "Invalid playlist selection. Use one number from 1 to %d, s to skip, or Enter to search again.\n", len(matches))
				continue
			}
			playlist := matches[n-1]
			return &playlist, nil
		}
	}
}

func readSetupChoice(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err == io.EOF {
		return "", fmt.Errorf("setup cancelled: input ended")
	}
	if err != nil {
		return "", fmt.Errorf("read setup selection: %w", err)
	}
	line = strings.TrimSpace(line)
	if strings.EqualFold(line, "q") {
		return "", fmt.Errorf("setup cancelled")
	}
	return line, nil
}

func setupChoiceNumber(value string, count int) (int, bool) {
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(value)
	return n, err == nil && n >= 1 && n <= count
}
