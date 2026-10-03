package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

type setupResult struct {
	OK            bool                  `json:"ok"`
	ConfigPath    string                `json:"configPath"`
	ConfigUpdated bool                  `json:"configUpdated"`
	Defaults      native.DefaultsConfig `json:"defaults"`
	Doctor        doctorReport          `json:"doctor"`
	Devices       []music.AirPlayDevice `json:"devices,omitempty"`
	DeviceError   string                `json:"deviceError,omitempty"`
	Playlists     []music.UserPlaylist  `json:"playlists,omitempty"`
	PlaylistError string                `json:"playlistError,omitempty"`
	Warnings      []string              `json:"warnings,omitempty"`
	Next          []string              `json:"next"`
}

type setupOptions struct {
	backend    string
	rooms      []string
	playlistID string
	jsonOut    bool
	choose     bool
	noInput    bool
}

func parseSetupOptions(args []string) (setupOptions, error) {
	flags, positionals, err := parseArgs("setup", args)
	if err != nil {
		return setupOptions{}, err
	}
	if len(positionals) != 0 {
		return setupOptions{}, usageErrf("usage: homepodctl setup [--backend airplay|native] [--room <name> ...] [--playlist-id <id>] [--choose] [--json] [--no-input]")
	}
	jsonOut, _, err := flags.boolStrict("json")
	if err != nil {
		return setupOptions{}, err
	}
	noInput, _, err := flags.boolStrict("no-input")
	if err != nil {
		return setupOptions{}, err
	}
	choose, _, err := flags.boolStrict("choose")
	if err != nil {
		return setupOptions{}, err
	}
	opts := setupOptions{
		backend:    strings.TrimSpace(flags.string("backend")),
		rooms:      flags.strings("room"),
		playlistID: strings.TrimSpace(flags.string("playlist-id")),
		jsonOut:    jsonOut,
		choose:     choose,
		noInput:    noInput,
	}
	if flags.has("playlist-id") && opts.playlistID == "" {
		return setupOptions{}, usageErrf("setup --playlist-id must be non-empty; use config set defaults.playlistId \"\" to clear a saved playlist")
	}
	if opts.choose && (opts.noInput || opts.jsonOut || quiet) {
		return setupOptions{}, usageErrf("setup --choose cannot be combined with --no-input, --json, or --quiet; omit --choose for noninteractive setup")
	}
	if opts.choose && opts.backend == "native" {
		return setupOptions{}, usageErrf("setup --choose supports AirPlay only; use --backend native --room <name> without --choose and configure Shortcut mappings")
	}
	if opts.backend != "" && opts.backend != "airplay" && opts.backend != "native" {
		return setupOptions{}, usageErrf("unknown backend: %q", opts.backend)
	}
	var issues []string
	for i, room := range opts.rooms {
		if strings.TrimSpace(room) == "" {
			issues = append(issues, fmt.Sprintf("defaults.rooms[%d] must be non-empty", i))
		}
	}
	if len(issues) > 0 {
		return setupOptions{}, usageErrf("setup produced invalid config: %s", strings.Join(issues, "; "))
	}
	return opts, nil
}

func cmdSetup(ctx context.Context, args []string) {
	opts, err := parseSetupOptions(args)
	if err != nil {
		die(err)
	}

	if opts.choose && !setupInputIsTerminal() {
		die(usageErrf("setup --choose requires interactive stdin; omit --choose and use --room <name> for noninteractive setup"))
	}
	var path string
	if opts.choose {
		path, err = configPath()
	} else {
		path, err = initConfig()
	}
	if err != nil {
		die(err)
	}
	cfg, err := loadConfigOptional()
	if err != nil {
		die(err)
	}
	original := *cfg
	working := *cfg
	cfg = &working

	configUpdated := false
	if opts.backend != "" {
		cfg.Defaults.Backend = opts.backend
		configUpdated = true
	}
	if len(opts.rooms) > 0 {
		cfg.Defaults.Rooms = append([]string(nil), opts.rooms...)
		configUpdated = true
	}
	if opts.playlistID != "" {
		cfg.Defaults.PlaylistID = opts.playlistID
		configUpdated = true
	}
	if issues := validateConfigValues(cfg); len(issues) > 0 {
		die(usageErrf("setup produced invalid config: %s", strings.Join(issues, "; ")))
	}
	if opts.choose && cfg.Defaults.Backend == "native" {
		die(usageErrf("setup --choose supports AirPlay only; use --backend airplay to change the default, or omit --choose for native setup"))
	}

	var devices []music.AirPlayDevice
	var playlists []music.UserPlaylist
	var devErr, playlistErr error
	if opts.choose {
		fmt.Fprintln(os.Stderr, "Discovering destinations and playlists; playback will stay unchanged. Enter q at a prompt to cancel.")
		devices, devErr = discoverSetupDevices(ctx)
		playlists, playlistErr = discoverSetupPlaylists(ctx)
		if devErr == nil {
			if playlistErr != nil {
				fmt.Fprintf(os.Stderr, "Playlist discovery failed: %s. You can still choose rooms.\n", formatError(playlistErr))
			}
			selection, err := chooseSetup(os.Stdin, os.Stderr, cfg, devices, playlists, len(opts.rooms) > 0, opts.playlistID != "")
			if err != nil {
				die(err)
			}
			if selection.RoomsChanged {
				cfg.Defaults.Rooms = selection.Rooms
				configUpdated = true
			}
			if selection.Playlist != nil {
				if cfg.Defaults.PlaylistID != selection.Playlist.PersistentID {
					cfg.Defaults.PlaylistID = selection.Playlist.PersistentID
					configUpdated = true
				}
				playlists = []music.UserPlaylist{*selection.Playlist}
			}
			// Create or update configuration only after every prompt completed.
			if configUpdated {
				if cfg.Defaults.Rooms == nil {
					cfg.Defaults.Rooms = []string{}
				}
				if err := native.SaveConfig(cfg); err != nil {
					die(err)
				}
			} else {
				if _, err := initConfig(); err != nil {
					die(err)
				}
				cfg, err = loadConfigOptional()
				if err != nil {
					die(err)
				}
			}
		} else {
			cfg = &original
			configUpdated = false
		}
	} else if configUpdated {
		if err := native.SaveConfig(cfg); err != nil {
			die(err)
		}
	}

	doctor := runDoctorChecks(ctx)
	if !opts.choose {
		devices, devErr = discoverSetupDevices(ctx)
		if cfg.Defaults.Backend != "native" {
			playlists, playlistErr = discoverSetupPlaylists(ctx)
		}
	}
	next, warnings := setupGuidance(cfg, devices, playlists, devErr == nil, playlistErr == nil)
	suggestions := setupPlaylistSuggestions(playlists)
	for _, playlist := range playlists {
		if playlist.PersistentID == cfg.Defaults.PlaylistID {
			suggestions = []music.UserPlaylist{playlist}
			break
		}
	}

	res := setupResult{
		OK:            doctor.OK && devErr == nil,
		ConfigPath:    path,
		ConfigUpdated: configUpdated,
		Defaults:      cfg.Defaults,
		Doctor:        doctor,
		Devices:       devices,
		Playlists:     suggestions,
		Warnings:      warnings,
		Next:          next,
	}
	if devErr != nil {
		res.DeviceError = formatError(devErr)
	}
	if playlistErr != nil {
		res.PlaylistError = formatError(playlistErr)
		res.Warnings = append(res.Warnings, "Could not read library playlists: "+res.PlaylistError+". Open Music.app, check Automation permissions, and run homepodctl playlists.")
	}

	if opts.jsonOut {
		writeJSON(res)
	} else if !quiet {
		fmt.Fprintf(checkedOutput{os.Stdout}, "setup ok=%t config=%s updated=%t\n", res.OK, res.ConfigPath, res.ConfigUpdated)
		fmt.Fprintf(checkedOutput{os.Stdout}, "defaults backend=%s rooms=%q playlist_id=%q\n", cfg.Defaults.Backend, cfg.Defaults.Rooms, cfg.Defaults.PlaylistID)
		printDoctorReport(doctor, false)
		if devErr != nil {
			fmt.Fprintf(checkedOutput{os.Stdout}, "devices error=%q\n", res.DeviceError)
		} else {
			if err := printDevicesTable(os.Stdout, devices, false); err != nil {
				die(&stdoutError{cause: err, code: exitGeneric})
			}
		}
		for _, warning := range res.Warnings {
			fmt.Fprintf(checkedOutput{os.Stdout}, "warning: %s\n", warning)
		}
		for _, playlist := range res.Playlists {
			fmt.Fprintf(checkedOutput{os.Stdout}, "playlist: %q (%s)\n", playlist.Name, playlist.PersistentID)
		}
		fmt.Fprintln(checkedOutput{os.Stdout}, "next:")
		for _, step := range res.Next {
			fmt.Fprintf(checkedOutput{os.Stdout}, "- %s\n", step)
		}
	}
	if !res.OK {
		exitCode(exitGeneric)
	}
}

func discoverSetupDevices(ctx context.Context) ([]music.AirPlayDevice, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	devices, err := listAirPlayDevices(ctx)
	if err != nil {
		return nil, err
	}
	devices = append([]music.AirPlayDevice(nil), devices...)
	for i := range devices {
		devices[i].NetworkAddress = ""
	}
	return devices, nil
}

func discoverSetupPlaylists(ctx context.Context) ([]music.UserPlaylist, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return listUserPlaylists(ctx, "", 0)
}
