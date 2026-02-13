package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
	"gopkg.in/yaml.v3"
)

type automationFile struct {
	Version  string             `json:"version" yaml:"version"`
	Name     string             `json:"name" yaml:"name"`
	Defaults automationDefaults `json:"defaults" yaml:"defaults"`
	Steps    []automationStep   `json:"steps" yaml:"steps"`
}

type automationDefaults struct {
	Backend string   `json:"backend,omitempty" yaml:"backend,omitempty"`
	Rooms   []string `json:"rooms,omitempty" yaml:"rooms,omitempty"`
	Volume  *int     `json:"volume,omitempty" yaml:"volume,omitempty"`
	Shuffle *bool    `json:"shuffle,omitempty" yaml:"shuffle,omitempty"`
}

type automationStep struct {
	Type       string   `json:"type" yaml:"type"`
	Rooms      []string `json:"rooms,omitempty" yaml:"rooms,omitempty"`
	Query      string   `json:"query,omitempty" yaml:"query,omitempty"`
	PlaylistID string   `json:"playlistId,omitempty" yaml:"playlistId,omitempty"`
	Value      *int     `json:"value,omitempty" yaml:"value,omitempty"`
	State      string   `json:"state,omitempty" yaml:"state,omitempty"`
	Timeout    string   `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Action     string   `json:"action,omitempty" yaml:"action,omitempty"`
}

type automationStepResult struct {
	Index      int            `json:"index"`
	Type       string         `json:"type"`
	Input      automationStep `json:"input"`
	Resolved   any            `json:"resolved,omitempty"`
	OK         bool           `json:"ok"`
	Skipped    bool           `json:"skipped"`
	Error      string         `json:"error,omitempty"`
	DurationMS int64          `json:"durationMs"`
}

type automationCommandResult struct {
	Name       string                 `json:"name"`
	Version    string                 `json:"version"`
	Mode       string                 `json:"mode"`
	OK         bool                   `json:"ok"`
	StartedAt  string                 `json:"startedAt"`
	EndedAt    string                 `json:"endedAt"`
	DurationMS int64                  `json:"durationMs"`
	Steps      []automationStepResult `json:"steps"`
}

type automationInitResult struct {
	Preset  string `json:"preset"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

func cmdAutomation(ctx context.Context, cfg *native.Config, args []string) {
	if len(args) == 0 {
		die(usageErrf("usage: homepodctl automation <run|validate|plan|init> [args]"))
	}
	switch args[0] {
	case "run":
		cmdAutomationRun(ctx, cfg, args[1:])
	case "validate":
		cmdAutomationValidate(cfg, args[1:])
	case "plan":
		cmdAutomationPlan(cfg, args[1:])
	case "init":
		cmdAutomationInit(args[1:])
	default:
		die(usageErrf("unknown automation subcommand: %q", args[0]))
	}
}

func cmdAutomationRun(ctx context.Context, cfg *native.Config, args []string) {
	fs := flag.NewFlagSet("automation run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	filePath := fs.String("file", "", "automation file path or - for stdin")
	fs.StringVar(filePath, "f", "", "automation file path or - for stdin")
	dryRun := fs.Bool("dry-run", false, "resolve and print without executing")
	jsonOut := fs.Bool("json", false, "output JSON")
	noInput := fs.Bool("no-input", false, "disable prompts (no-op: automation is non-interactive by default)")
	if err := fs.Parse(args); err != nil {
		die(usageErrf("usage: homepodctl automation run -f <file|-> [--dry-run] [--json] [--no-input]"))
	}
	if strings.TrimSpace(*filePath) == "" {
		die(usageErrf("--file is required"))
	}
	doc, err := loadAutomationFile(*filePath)
	if err != nil {
		die(err)
	}
	if err := validateAutomation(doc); err != nil {
		die(err)
	}

	mode := "run"
	steps := resolveAutomationSteps(cfg, doc)
	if *dryRun {
		mode = "dry-run"
		result := buildAutomationResult(mode, doc, steps)
		emitAutomationResult(result, *jsonOut)
		return
	}
	_ = noInput // accepted for compatibility; automation runs are non-interactive.
	// automation runs can include waits; use a longer timeout than one-off commands.
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	executed, ok := executeAutomationSteps(runCtx, cfg, doc)
	result := buildAutomationResult(mode, doc, executed)
	result.OK = ok
	emitAutomationResult(result, *jsonOut)
	if !result.OK {
		os.Exit(exitGeneric)
	}
}

func cmdAutomationValidate(_ *native.Config, args []string) {
	fs := flag.NewFlagSet("automation validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	filePath := fs.String("file", "", "automation file path or - for stdin")
	fs.StringVar(filePath, "f", "", "automation file path or - for stdin")
	jsonOut := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		die(usageErrf("usage: homepodctl automation validate -f <file|-> [--json]"))
	}
	if strings.TrimSpace(*filePath) == "" {
		die(usageErrf("--file is required"))
	}
	doc, err := loadAutomationFile(*filePath)
	if err != nil {
		die(err)
	}
	if err := validateAutomation(doc); err != nil {
		die(err)
	}
	result := buildAutomationResult("validate", doc, resolveAutomationSteps(nil, doc))
	emitAutomationResult(result, *jsonOut)
}

func cmdAutomationPlan(cfg *native.Config, args []string) {
	fs := flag.NewFlagSet("automation plan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	filePath := fs.String("file", "", "automation file path or - for stdin")
	fs.StringVar(filePath, "f", "", "automation file path or - for stdin")
	jsonOut := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		die(usageErrf("usage: homepodctl automation plan -f <file|-> [--json]"))
	}
	if strings.TrimSpace(*filePath) == "" {
		die(usageErrf("--file is required"))
	}
	doc, err := loadAutomationFile(*filePath)
	if err != nil {
		die(err)
	}
	if err := validateAutomation(doc); err != nil {
		die(err)
	}
	result := buildAutomationResult("plan", doc, resolveAutomationSteps(cfg, doc))
	emitAutomationResult(result, *jsonOut)
}

func cmdAutomationInit(args []string) {
	fs := flag.NewFlagSet("automation init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	preset := fs.String("preset", "", "preset name: morning|focus|winddown|party|reset")
	name := fs.String("name", "", "override routine name")
	jsonOut := fs.Bool("json", false, "output JSON metadata")
	if err := fs.Parse(args); err != nil {
		die(usageErrf("usage: homepodctl automation init --preset <name> [--name <string>] [--json]"))
	}
	if strings.TrimSpace(*preset) == "" {
		die(usageErrf("--preset is required"))
	}
	doc, err := automationPreset(*preset)
	if err != nil {
		die(err)
	}
	if strings.TrimSpace(*name) != "" {
		doc.Name = strings.TrimSpace(*name)
	}
	b, err := yaml.Marshal(doc)
	if err != nil {
		die(fmt.Errorf("encode preset: %w", err))
	}
	if *jsonOut {
		writeJSON(automationInitResult{Preset: strings.TrimSpace(*preset), Name: doc.Name, Content: string(b)})
		return
	}
	fmt.Print(string(b))
}

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
	cfg, err := native.LoadConfigOptional()
	if err != nil {
		die(err)
	}
	path, _ := native.ConfigPath()
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
	os.Exit(exitUsage)
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
	cfg, err := native.LoadConfigOptional()
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

	cfg, err := native.LoadConfigOptional()
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
	path, err := native.ConfigPath()
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

func loadAutomationFile(path string) (*automationFile, error) {
	b, err := readAutomationInput(path)
	if err != nil {
		return nil, err
	}
	doc, err := parseAutomationBytes(b)
	if err != nil {
		return nil, err
	}
	return doc, nil
}

func readAutomationInput(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return b, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read automation file %q: %w", path, err)
	}
	return b, nil
}

func parseAutomationBytes(b []byte) (*automationFile, error) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil, automationValidationErrf("automation file is empty")
	}
	var doc automationFile
	if b[0] == '{' {
		if err := json.Unmarshal(b, &doc); err != nil {
			return nil, automationValidationErrf("invalid automation JSON: %v", err)
		}
		return &doc, nil
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, automationValidationErrf("invalid automation YAML: %v", err)
	}
	return &doc, nil
}

func validateAutomation(doc *automationFile) error {
	if doc == nil {
		return automationValidationErrf("automation file is required")
	}
	if strings.TrimSpace(doc.Version) != "1" {
		return automationValidationErrf("version: expected \"1\"")
	}
	if strings.TrimSpace(doc.Name) == "" {
		return automationValidationErrf("name: required")
	}
	if err := validateAutomationDefaults("defaults", doc.Defaults); err != nil {
		return err
	}
	if len(doc.Steps) == 0 {
		return automationValidationErrf("steps: must contain at least one step")
	}
	for i, st := range doc.Steps {
		if err := validateAutomationStep(i, st); err != nil {
			return err
		}
	}
	return nil
}

func validateAutomationDefaults(path string, d automationDefaults) error {
	if d.Backend != "" && d.Backend != "airplay" && d.Backend != "native" {
		return automationValidationErrf("%s.backend: expected airplay or native", path)
	}
	if d.Volume != nil && (*d.Volume < 0 || *d.Volume > 100) {
		return automationValidationErrf("%s.volume: expected 0..100", path)
	}
	for i, r := range d.Rooms {
		if strings.TrimSpace(r) == "" {
			return automationValidationErrf("%s.rooms[%d]: must be non-empty", path, i)
		}
	}
	return nil
}

func validateAutomationStep(i int, st automationStep) error {
	path := fmt.Sprintf("steps[%d]", i)
	t := strings.TrimSpace(st.Type)
	if t == "" {
		return automationValidationErrf("%s.type: required", path)
	}
	switch t {
	case "out.set":
		if len(st.Rooms) == 0 {
			return automationValidationErrf("%s.rooms: required for out.set", path)
		}
		for j, r := range st.Rooms {
			if strings.TrimSpace(r) == "" {
				return automationValidationErrf("%s.rooms[%d]: must be non-empty", path, j)
			}
		}
	case "play":
		hasQ := strings.TrimSpace(st.Query) != ""
		hasID := strings.TrimSpace(st.PlaylistID) != ""
		if hasQ == hasID {
			return automationValidationErrf("%s: play requires exactly one of query or playlistId", path)
		}
	case "volume.set":
		if st.Value == nil {
			return automationValidationErrf("%s.value: required for volume.set", path)
		}
		if *st.Value < 0 || *st.Value > 100 {
			return automationValidationErrf("%s.value: expected 0..100", path)
		}
	case "wait":
		s := strings.TrimSpace(st.State)
		if s != "playing" && s != "paused" && s != "stopped" {
			return automationValidationErrf("%s.state: expected playing|paused|stopped", path)
		}
		if strings.TrimSpace(st.Timeout) == "" {
			return automationValidationErrf("%s.timeout: required", path)
		}
		d, err := time.ParseDuration(st.Timeout)
		if err != nil {
			return automationValidationErrf("%s.timeout: invalid duration", path)
		}
		if d < time.Second || d > 10*time.Minute {
			return automationValidationErrf("%s.timeout: expected between 1s and 10m", path)
		}
	case "transport":
		if strings.TrimSpace(st.Action) != "stop" {
			return automationValidationErrf("%s.action: only \"stop\" is supported in v1", path)
		}
	default:
		return automationValidationErrf("%s.type: unsupported step type %q", path, st.Type)
	}
	return nil
}

func resolveAutomationSteps(cfg *native.Config, doc *automationFile) []automationStepResult {
	resolvedDefaults := resolveAutomationDefaults(cfg, doc.Defaults)

	out := make([]automationStepResult, 0, len(doc.Steps))
	for i, st := range doc.Steps {
		resolved := map[string]any{"backend": resolvedDefaults.Backend}
		switch st.Type {
		case "out.set":
			resolved["rooms"] = st.Rooms
		case "play":
			if strings.TrimSpace(st.Query) != "" {
				resolved["query"] = st.Query
			}
			if strings.TrimSpace(st.PlaylistID) != "" {
				resolved["playlistId"] = st.PlaylistID
			}
			if resolvedDefaults.Shuffle != nil {
				resolved["shuffle"] = *resolvedDefaults.Shuffle
			}
			if resolvedDefaults.Volume != nil {
				resolved["volume"] = *resolvedDefaults.Volume
			}
			if len(resolvedDefaults.Rooms) > 0 {
				resolved["rooms"] = resolvedDefaults.Rooms
			}
		case "volume.set":
			if st.Value != nil {
				resolved["value"] = *st.Value
			}
			if len(st.Rooms) > 0 {
				resolved["rooms"] = st.Rooms
			} else if len(resolvedDefaults.Rooms) > 0 {
				resolved["rooms"] = resolvedDefaults.Rooms
			}
		case "wait":
			resolved["state"] = st.State
			resolved["timeout"] = st.Timeout
		case "transport":
			resolved["action"] = st.Action
		}
		out = append(out, automationStepResult{
			Index:      i,
			Type:       st.Type,
			Input:      st,
			Resolved:   resolved,
			OK:         true,
			Skipped:    false,
			DurationMS: 0,
		})
	}
	return out
}

func resolveAutomationDefaults(cfg *native.Config, in automationDefaults) automationDefaults {
	out := in
	if cfg == nil {
		return out
	}
	if strings.TrimSpace(out.Backend) == "" {
		out.Backend = cfg.Defaults.Backend
	}
	if len(out.Rooms) == 0 {
		out.Rooms = append([]string(nil), cfg.Defaults.Rooms...)
	}
	if out.Volume == nil && cfg.Defaults.Volume != nil {
		v := *cfg.Defaults.Volume
		out.Volume = &v
	}
	if out.Shuffle == nil {
		v := cfg.Defaults.Shuffle
		out.Shuffle = &v
	}
	return out
}

func buildAutomationResult(mode string, doc *automationFile, steps []automationStepResult) automationCommandResult {
	started := time.Now().UTC()
	ended := started
	return automationCommandResult{
		Name:       doc.Name,
		Version:    doc.Version,
		Mode:       mode,
		OK:         true,
		StartedAt:  started.Format(time.RFC3339),
		EndedAt:    ended.Format(time.RFC3339),
		DurationMS: ended.Sub(started).Milliseconds(),
		Steps:      steps,
	}
}

func executeAutomationSteps(ctx context.Context, cfg *native.Config, doc *automationFile) ([]automationStepResult, bool) {
	defaults := resolveAutomationDefaults(cfg, doc.Defaults)
	results := make([]automationStepResult, 0, len(doc.Steps))
	ok := true

	for i, st := range doc.Steps {
		stepStart := time.Now()
		res := automationStepResult{
			Index: i,
			Type:  st.Type,
			Input: st,
		}
		err := executeAutomationStep(ctx, cfg, defaults, st)
		res.DurationMS = time.Since(stepStart).Milliseconds()
		if err != nil {
			res.OK = false
			res.Error = err.Error()
			ok = false
			results = append(results, res)
			// mark remaining steps as skipped so callers can inspect full plan shape.
			for j := i + 1; j < len(doc.Steps); j++ {
				results = append(results, automationStepResult{
					Index:   j,
					Type:    doc.Steps[j].Type,
					Input:   doc.Steps[j],
					OK:      false,
					Skipped: true,
					Error:   "skipped due to previous step failure",
				})
			}
			break
		}
		res.OK = true
		results = append(results, res)
	}
	return results, ok
}

func executeAutomationStep(ctx context.Context, cfg *native.Config, defaults automationDefaults, st automationStep) error {
	backend := strings.TrimSpace(defaults.Backend)
	if backend == "" {
		backend = "airplay"
	}

	switch st.Type {
	case "out.set":
		if backend != "airplay" {
			return fmt.Errorf("out.set only supports backend=airplay")
		}
		return setCurrentOutputs(ctx, st.Rooms)
	case "play":
		return executeAutomationPlay(ctx, cfg, backend, defaults, st)
	case "volume.set":
		if st.Value == nil {
			return fmt.Errorf("volume.set requires value")
		}
		return executeAutomationVolume(ctx, cfg, backend, defaults, *st.Value, st.Rooms)
	case "wait":
		return executeAutomationWait(ctx, st.State, st.Timeout)
	case "transport":
		if strings.TrimSpace(st.Action) != "stop" {
			return fmt.Errorf("unsupported transport action %q", st.Action)
		}
		return stopPlayback(ctx)
	default:
		return fmt.Errorf("unsupported step type %q", st.Type)
	}
}

func executeAutomationPlay(ctx context.Context, cfg *native.Config, backend string, defaults automationDefaults, st automationStep) error {
	switch backend {
	case "airplay":
		rooms := append([]string(nil), defaults.Rooms...)
		if len(rooms) > 0 {
			if err := setCurrentOutputs(ctx, rooms); err != nil {
				return err
			}
		}
		if defaults.Volume != nil && len(rooms) > 0 {
			for _, room := range rooms {
				if err := setDeviceVolume(ctx, room, *defaults.Volume); err != nil {
					return err
				}
			}
		}
		if defaults.Shuffle != nil {
			if err := setShuffle(ctx, *defaults.Shuffle); err != nil {
				return err
			}
		}
		id := strings.TrimSpace(st.PlaylistID)
		if id == "" {
			matches, err := searchPlaylists(ctx, st.Query)
			if err != nil {
				return err
			}
			best, ok := music.PickBestPlaylist(st.Query, matches)
			if !ok {
				return fmt.Errorf("no playlists match %q", st.Query)
			}
			id = best.PersistentID
		}
		return playPlaylistByID(ctx, id)
	case "native":
		if cfg == nil {
			return fmt.Errorf("native backend requires config")
		}
		rooms := append([]string(nil), defaults.Rooms...)
		if len(rooms) == 0 {
			return fmt.Errorf("native play requires rooms")
		}
		name := strings.TrimSpace(st.Query)
		if name == "" {
			var err error
			name, err = findPlaylistNameByID(ctx, st.PlaylistID)
			if err != nil {
				return err
			}
		}
		for _, room := range rooms {
			shortcutName, ok := cfg.Native.Playlists[room][name]
			if !ok || strings.TrimSpace(shortcutName) == "" {
				return fmt.Errorf("no native mapping for room=%q playlist=%q", room, name)
			}
			if err := runNativeShortcut(ctx, shortcutName); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown backend %q", backend)
	}
}

func executeAutomationVolume(ctx context.Context, cfg *native.Config, backend string, defaults automationDefaults, value int, overrideRooms []string) error {
	rooms := append([]string(nil), overrideRooms...)
	if len(rooms) == 0 {
		rooms = append(rooms, defaults.Rooms...)
	}
	switch backend {
	case "airplay":
		if len(rooms) == 0 {
			rooms = inferSelectedOutputs(ctx)
		}
		if len(rooms) == 0 {
			return fmt.Errorf("no rooms available for volume.set")
		}
		for _, room := range rooms {
			if err := setDeviceVolume(ctx, room, value); err != nil {
				return err
			}
		}
		return nil
	case "native":
		if cfg == nil {
			return fmt.Errorf("native backend requires config")
		}
		if len(rooms) == 0 {
			return fmt.Errorf("native volume.set requires rooms")
		}
		for _, room := range rooms {
			m := cfg.Native.VolumeShortcuts[room]
			shortcutName := ""
			if m != nil {
				shortcutName = m[fmt.Sprint(value)]
			}
			if strings.TrimSpace(shortcutName) == "" {
				return fmt.Errorf("no native volume mapping for room=%q value=%d", room, value)
			}
			if err := runNativeShortcut(ctx, shortcutName); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown backend %q", backend)
	}
}

func executeAutomationWait(ctx context.Context, wantState string, timeoutRaw string) error {
	timeout, err := time.ParseDuration(timeoutRaw)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	want := strings.ToLower(strings.TrimSpace(wantState))
	for {
		np, err := getNowPlaying(ctx)
		if err != nil {
			return err
		}
		if strings.ToLower(strings.TrimSpace(np.PlayerState)) == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait timeout after %s for state=%s", timeout.String(), want)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		sleepFn(1 * time.Second)
	}
}

func emitAutomationResult(result automationCommandResult, jsonOut bool) {
	if jsonOut {
		writeJSON(result)
		return
	}
	fmt.Printf("automation name=%q mode=%s ok=%t steps=%d\n", result.Name, result.Mode, result.OK, len(result.Steps))
	for _, st := range result.Steps {
		fmt.Printf("%d/%d %s ok=%t\n", st.Index+1, len(result.Steps), st.Type, st.OK)
	}
}

func automationPreset(name string) (automationFile, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "morning":
		return automationFile{
			Version:  "1",
			Name:     "morning",
			Defaults: automationDefaults{Backend: "airplay", Rooms: []string{"Bedroom"}, Volume: intPtr(30), Shuffle: boolPtr(false)},
			Steps:    []automationStep{{Type: "out.set", Rooms: []string{"Bedroom"}}, {Type: "play", Query: "Morning Mix"}, {Type: "volume.set", Value: intPtr(30)}, {Type: "wait", State: "playing", Timeout: "20s"}},
		}, nil
	case "focus":
		return automationFile{
			Version:  "1",
			Name:     "focus",
			Defaults: automationDefaults{Backend: "airplay", Rooms: []string{"Office"}, Volume: intPtr(25), Shuffle: boolPtr(false)},
			Steps:    []automationStep{{Type: "out.set", Rooms: []string{"Office"}}, {Type: "play", Query: "Deep Focus"}, {Type: "volume.set", Value: intPtr(25)}, {Type: "wait", State: "playing", Timeout: "20s"}},
		}, nil
	case "winddown":
		return automationFile{
			Version:  "1",
			Name:     "winddown",
			Defaults: automationDefaults{Backend: "airplay", Rooms: []string{"Bedroom"}, Volume: intPtr(20), Shuffle: boolPtr(false)},
			Steps:    []automationStep{{Type: "out.set", Rooms: []string{"Bedroom"}}, {Type: "play", Query: "Evening Ambient"}, {Type: "volume.set", Value: intPtr(20)}, {Type: "wait", State: "playing", Timeout: "20s"}},
		}, nil
	case "party":
		return automationFile{
			Version:  "1",
			Name:     "party",
			Defaults: automationDefaults{Backend: "airplay", Rooms: []string{"Living Room", "Kitchen"}, Volume: intPtr(55), Shuffle: boolPtr(true)},
			Steps:    []automationStep{{Type: "out.set", Rooms: []string{"Living Room", "Kitchen"}}, {Type: "play", Query: "Party Mix"}, {Type: "volume.set", Value: intPtr(55)}, {Type: "wait", State: "playing", Timeout: "30s"}},
		}, nil
	case "reset":
		return automationFile{
			Version:  "1",
			Name:     "reset",
			Defaults: automationDefaults{Backend: "airplay", Rooms: []string{"Bedroom"}, Volume: intPtr(25)},
			Steps:    []automationStep{{Type: "transport", Action: "stop"}, {Type: "out.set", Rooms: []string{"Bedroom"}}, {Type: "volume.set", Value: intPtr(25)}},
		}, nil
	default:
		return automationFile{}, usageErrf("unknown preset %q (expected morning, focus, winddown, party, reset)", name)
	}
}

func intPtr(v int) *int { return &v }

func boolPtr(v bool) *bool { return &v }
