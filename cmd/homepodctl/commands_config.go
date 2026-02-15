package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/agisilaos/homepodctl/internal/native"
)

type configValidateResult struct {
	OK     bool     `json:"ok"`
	Path   string   `json:"path"`
	Errors []string `json:"errors,omitempty"`
}

func cmdConfig(args []string) {
	if len(args) == 0 {
		die(usageErrf("usage: homepodctl config <validate|get|set> [args]"))
	}
	switch args[0] {
	case "validate":
		cmdConfigValidate(args[1:])
	case "get":
		cmdConfigGet(args[1:])
	case "set":
		cmdConfigSet(args[1:])
	default:
		die(usageErrf("unknown config subcommand: %q", args[0]))
	}
}

func cmdConfigValidate(args []string) {
	fs := flag.NewFlagSet("config validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		die(usageErrf("usage: homepodctl config validate [--json]"))
	}
	cfg, err := loadConfigOptional()
	if err != nil {
		die(err)
	}
	path, _ := configPath()
	issues := validateConfigValues(cfg)
	res := configValidateResult{
		OK:     len(issues) == 0,
		Path:   path,
		Errors: issues,
	}
	if *jsonOut {
		writeJSON(res)
		return
	}
	if res.OK {
		fmt.Printf("config ok: %s\n", res.Path)
		return
	}
	fmt.Printf("config invalid: %s\n", res.Path)
	for _, issue := range res.Errors {
		fmt.Printf("- %s\n", issue)
	}
	exitCode(exitUsage)
}

func cmdConfigGet(args []string) {
	flags, pos, err := parseArgs(args)
	if err != nil {
		die(err)
	}
	jsonOut, _, err := parseOutputFlags(flags)
	if err != nil {
		die(err)
	}
	if len(pos) != 1 {
		die(usageErrf("usage: homepodctl config get <path> [--json]"))
	}
	key := strings.TrimSpace(pos[0])
	cfg, err := loadConfigOptional()
	if err != nil {
		die(err)
	}
	value, err := getConfigPathValue(cfg, key)
	if err != nil {
		die(err)
	}
	if jsonOut {
		writeJSON(map[string]any{"path": key, "value": value})
		return
	}
	switch v := value.(type) {
	case []string:
		fmt.Println(strings.Join(v, "\t"))
	default:
		fmt.Printf("%v\n", v)
	}
}

func cmdConfigSet(args []string) {
	fs := flag.NewFlagSet("config set", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		die(usageErrf("usage: homepodctl config set <path> <value...>"))
	}
	if fs.NArg() < 2 {
		die(usageErrf("usage: homepodctl config set <path> <value...>"))
	}
	key := strings.TrimSpace(fs.Arg(0))
	values := fs.Args()[1:]

	cfg, err := loadConfigOptional()
	if err != nil {
		die(err)
	}
	if err := setConfigPathValue(cfg, key, values); err != nil {
		die(err)
	}
	issues := validateConfigValues(cfg)
	if len(issues) > 0 {
		die(usageErrf("updated config is invalid: %s", strings.Join(issues, "; ")))
	}
	path, err := configPath()
	if err != nil {
		die(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		die(err)
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		die(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		die(err)
	}
	fmt.Printf("Updated %s (%s)\n", path, key)
}

func validateConfigValues(cfg *native.Config) []string {
	var issues []string
	switch cfg.Defaults.Backend {
	case "", "airplay", "native":
	default:
		issues = append(issues, fmt.Sprintf("defaults.backend must be airplay|native, got %q", cfg.Defaults.Backend))
	}
	if cfg.Defaults.Volume != nil && (*cfg.Defaults.Volume < 0 || *cfg.Defaults.Volume > 100) {
		issues = append(issues, fmt.Sprintf("defaults.volume must be 0..100, got %d", *cfg.Defaults.Volume))
	}
	for i, room := range cfg.Defaults.Rooms {
		if strings.TrimSpace(room) == "" {
			issues = append(issues, fmt.Sprintf("defaults.rooms[%d] must be non-empty", i))
		}
	}
	for name, a := range cfg.Aliases {
		if strings.TrimSpace(name) == "" {
			issues = append(issues, "aliases key must be non-empty")
		}
		if a.Backend != "" && a.Backend != "airplay" && a.Backend != "native" {
			issues = append(issues, fmt.Sprintf("aliases.%s.backend must be airplay|native, got %q", name, a.Backend))
		}
		for i, room := range a.Rooms {
			if strings.TrimSpace(room) == "" {
				issues = append(issues, fmt.Sprintf("aliases.%s.rooms[%d] must be non-empty", name, i))
			}
		}
		if a.Volume != nil && (*a.Volume < 0 || *a.Volume > 100) {
			issues = append(issues, fmt.Sprintf("aliases.%s.volume must be 0..100, got %d", name, *a.Volume))
		}
	}
	for room, mappings := range cfg.Native.Playlists {
		if strings.TrimSpace(room) == "" {
			issues = append(issues, "native.playlists room key must be non-empty")
		}
		for playlist, shortcut := range mappings {
			if strings.TrimSpace(playlist) == "" {
				issues = append(issues, fmt.Sprintf("native.playlists.%s playlist key must be non-empty", room))
			}
			if strings.TrimSpace(shortcut) == "" {
				issues = append(issues, fmt.Sprintf("native.playlists.%s.%s shortcut must be non-empty", room, playlist))
			}
		}
	}
	for room, mappings := range cfg.Native.VolumeShortcuts {
		if strings.TrimSpace(room) == "" {
			issues = append(issues, "native.volumeShortcuts room key must be non-empty")
		}
		for volStr, shortcut := range mappings {
			n, err := strconv.Atoi(volStr)
			if err != nil || n < 0 || n > 100 {
				issues = append(issues, fmt.Sprintf("native.volumeShortcuts.%s.%s key must be 0..100", room, volStr))
			}
			if strings.TrimSpace(shortcut) == "" {
				issues = append(issues, fmt.Sprintf("native.volumeShortcuts.%s.%s shortcut must be non-empty", room, volStr))
			}
		}
	}
	return issues
}

func getConfigPathValue(cfg *native.Config, key string) (any, error) {
	switch key {
	case "defaults.backend":
		return cfg.Defaults.Backend, nil
	case "defaults.shuffle":
		return cfg.Defaults.Shuffle, nil
	case "defaults.volume":
		if cfg.Defaults.Volume == nil {
			return nil, nil
		}
		return *cfg.Defaults.Volume, nil
	case "defaults.rooms":
		return append([]string(nil), cfg.Defaults.Rooms...), nil
	}

	parts := strings.Split(key, ".")
	if len(parts) >= 3 && parts[0] == "aliases" {
		aliasName := strings.TrimSpace(parts[1])
		if aliasName == "" {
			return nil, usageErrf("alias name must be non-empty in path %q", key)
		}
		a, ok := cfg.Aliases[aliasName]
		if !ok {
			return nil, usageErrf("unknown alias %q", aliasName)
		}
		if len(parts) != 3 {
			return nil, usageErrf("unsupported config path %q", key)
		}
		switch parts[2] {
		case "backend":
			return a.Backend, nil
		case "rooms":
			return append([]string(nil), a.Rooms...), nil
		case "playlist":
			return a.Playlist, nil
		case "playlistId":
			return a.PlaylistID, nil
		case "shuffle":
			if a.Shuffle == nil {
				return nil, nil
			}
			return *a.Shuffle, nil
		case "volume":
			if a.Volume == nil {
				return nil, nil
			}
			return *a.Volume, nil
		case "shortcut":
			return a.Shortcut, nil
		default:
			return nil, usageErrf("unsupported config path %q", key)
		}
	}
	if len(parts) >= 4 && parts[0] == "native" && parts[1] == "playlists" {
		if len(parts) != 4 {
			return nil, usageErrf("unsupported config path %q", key)
		}
		room := strings.TrimSpace(parts[2])
		playlist := strings.TrimSpace(parts[3])
		if room == "" || playlist == "" {
			return nil, usageErrf("native playlists path must include non-empty room and playlist: %q", key)
		}
		return cfg.Native.Playlists[room][playlist], nil
	}
	if len(parts) >= 4 && parts[0] == "native" && parts[1] == "volumeShortcuts" {
		if len(parts) != 4 {
			return nil, usageErrf("unsupported config path %q", key)
		}
		room := strings.TrimSpace(parts[2])
		volumeKey := strings.TrimSpace(parts[3])
		if room == "" || volumeKey == "" {
			return nil, usageErrf("native volumeShortcuts path must include non-empty room and volume: %q", key)
		}
		return cfg.Native.VolumeShortcuts[room][volumeKey], nil
	}
	return nil, usageErrf("unsupported config path %q", key)
}

func setConfigPathValue(cfg *native.Config, key string, values []string) error {
	switch key {
	case "defaults.backend":
		if len(values) != 1 {
			return usageErrf("%s expects exactly 1 value", key)
		}
		v := strings.TrimSpace(values[0])
		if v != "airplay" && v != "native" {
			return usageErrf("%s must be airplay|native", key)
		}
		cfg.Defaults.Backend = v
		return nil
	case "defaults.shuffle":
		if len(values) != 1 {
			return usageErrf("%s expects exactly 1 value", key)
		}
		switch strings.ToLower(strings.TrimSpace(values[0])) {
		case "true", "1", "yes", "on":
			cfg.Defaults.Shuffle = true
		case "false", "0", "no", "off":
			cfg.Defaults.Shuffle = false
		default:
			return usageErrf("%s expects boolean true|false", key)
		}
		return nil
	case "defaults.volume":
		if len(values) != 1 {
			return usageErrf("%s expects exactly 1 value", key)
		}
		v := strings.TrimSpace(values[0])
		if v == "null" {
			cfg.Defaults.Volume = nil
			return nil
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 100 {
			return usageErrf("%s expects 0..100 or null", key)
		}
		cfg.Defaults.Volume = &n
		return nil
	case "defaults.rooms":
		rooms := make([]string, 0, len(values))
		for _, v := range values {
			r := strings.TrimSpace(v)
			if r == "" {
				return usageErrf("%s values must be non-empty", key)
			}
			rooms = append(rooms, r)
		}
		cfg.Defaults.Rooms = rooms
		return nil
	}

	parts := strings.Split(key, ".")
	if len(parts) >= 3 && parts[0] == "aliases" {
		if len(parts) != 3 {
			return usageErrf("unsupported config path %q", key)
		}
		aliasName := strings.TrimSpace(parts[1])
		field := parts[2]
		if aliasName == "" {
			return usageErrf("alias name must be non-empty in path %q", key)
		}
		if cfg.Aliases == nil {
			cfg.Aliases = map[string]native.Alias{}
		}
		a := cfg.Aliases[aliasName]
		switch field {
		case "backend":
			if len(values) != 1 {
				return usageErrf("%s expects exactly 1 value", key)
			}
			v := strings.TrimSpace(values[0])
			if v != "airplay" && v != "native" {
				return usageErrf("%s must be airplay|native", key)
			}
			a.Backend = v
		case "rooms":
			rooms := make([]string, 0, len(values))
			for _, v := range values {
				r := strings.TrimSpace(v)
				if r == "" {
					return usageErrf("%s values must be non-empty", key)
				}
				rooms = append(rooms, r)
			}
			a.Rooms = rooms
		case "playlist":
			if len(values) != 1 {
				return usageErrf("%s expects exactly 1 value", key)
			}
			a.Playlist = strings.TrimSpace(values[0])
		case "playlistId":
			if len(values) != 1 {
				return usageErrf("%s expects exactly 1 value", key)
			}
			a.PlaylistID = strings.TrimSpace(values[0])
		case "shuffle":
			if len(values) != 1 {
				return usageErrf("%s expects exactly 1 value", key)
			}
			v := strings.ToLower(strings.TrimSpace(values[0]))
			if v == "null" {
				a.Shuffle = nil
				cfg.Aliases[aliasName] = a
				return nil
			}
			var b bool
			switch v {
			case "true", "1", "yes", "on":
				b = true
			case "false", "0", "no", "off":
				b = false
			default:
				return usageErrf("%s expects boolean true|false or null", key)
			}
			a.Shuffle = &b
		case "volume":
			if len(values) != 1 {
				return usageErrf("%s expects exactly 1 value", key)
			}
			v := strings.TrimSpace(values[0])
			if v == "null" {
				a.Volume = nil
				cfg.Aliases[aliasName] = a
				return nil
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 || n > 100 {
				return usageErrf("%s expects 0..100 or null", key)
			}
			a.Volume = &n
		case "shortcut":
			if len(values) != 1 {
				return usageErrf("%s expects exactly 1 value", key)
			}
			a.Shortcut = strings.TrimSpace(values[0])
		default:
			return usageErrf("unsupported config path %q", key)
		}
		cfg.Aliases[aliasName] = a
		return nil
	}
	if len(parts) >= 4 && parts[0] == "native" && parts[1] == "playlists" {
		if len(parts) != 4 {
			return usageErrf("unsupported config path %q", key)
		}
		if len(values) != 1 {
			return usageErrf("%s expects exactly 1 value", key)
		}
		room := strings.TrimSpace(parts[2])
		playlist := strings.TrimSpace(parts[3])
		shortcut := strings.TrimSpace(values[0])
		if room == "" || playlist == "" || shortcut == "" {
			return usageErrf("%s expects non-empty room, playlist, and shortcut", key)
		}
		if cfg.Native.Playlists == nil {
			cfg.Native.Playlists = map[string]map[string]string{}
		}
		if cfg.Native.Playlists[room] == nil {
			cfg.Native.Playlists[room] = map[string]string{}
		}
		cfg.Native.Playlists[room][playlist] = shortcut
		return nil
	}
	if len(parts) >= 4 && parts[0] == "native" && parts[1] == "volumeShortcuts" {
		if len(parts) != 4 {
			return usageErrf("unsupported config path %q", key)
		}
		if len(values) != 1 {
			return usageErrf("%s expects exactly 1 value", key)
		}
		room := strings.TrimSpace(parts[2])
		volumeKey := strings.TrimSpace(parts[3])
		shortcut := strings.TrimSpace(values[0])
		n, err := strconv.Atoi(volumeKey)
		if err != nil || n < 0 || n > 100 {
			return usageErrf("%s volume key must be 0..100", key)
		}
		if room == "" || shortcut == "" {
			return usageErrf("%s expects non-empty room and shortcut", key)
		}
		if cfg.Native.VolumeShortcuts == nil {
			cfg.Native.VolumeShortcuts = map[string]map[string]string{}
		}
		if cfg.Native.VolumeShortcuts[room] == nil {
			cfg.Native.VolumeShortcuts[room] = map[string]string{}
		}
		cfg.Native.VolumeShortcuts[room][volumeKey] = shortcut
		return nil
	}
	return usageErrf("unsupported config path %q", key)
}
