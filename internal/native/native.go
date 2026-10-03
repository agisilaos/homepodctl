package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Config struct {
	Defaults DefaultsConfig   `json:"defaults"`
	Aliases  map[string]Alias `json:"aliases"`
	Native   NativeConfig     `json:"native"`
}

type DefaultsConfig struct {
	Backend    string   `json:"backend"`
	Rooms      []string `json:"rooms"`
	PlaylistID string   `json:"playlistId,omitempty"` // optional default for direct playback
	Shuffle    bool     `json:"shuffle"`
	Volume     *int     `json:"volume"` // 0-100
}

type Alias struct {
	Backend    string   `json:"backend"`              // airplay|native
	Rooms      []string `json:"rooms"`                // optional
	Playlist   string   `json:"playlist,omitempty"`   // optional
	PlaylistID string   `json:"playlistId,omitempty"` // optional
	Shuffle    *bool    `json:"shuffle,omitempty"`    // optional
	Volume     *int     `json:"volume,omitempty"`     // optional
	Shortcut   string   `json:"shortcut,omitempty"`   // optional, runs shortcuts directly
}

type NativeConfig struct {
	Playlists       map[string]map[string]string `json:"playlists"`       // room -> playlist name -> shortcut name
	VolumeShortcuts map[string]map[string]string `json:"volumeShortcuts"` // room -> "0".."100" -> shortcut name (discrete)
}

type ConfigError struct {
	Op   string
	Path string
	Err  error
}

func (e *ConfigError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("%s config: %v", e.Op, e.Err)
	}
	return fmt.Sprintf("%s config %s: %v", e.Op, e.Path, e.Err)
}

func (e *ConfigError) Unwrap() error { return e.Err }

type ShortcutError struct {
	Uncertain bool
	Name      string
	Err       error
	Output    string
}

var (
	runShortcutExec = func(ctx context.Context, name string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "shortcuts", "run", name)
		return cmd.CombinedOutput()
	}
)

func (e *ShortcutError) Error() string {
	message := fmt.Sprintf("shortcuts run %q failed: %v: %s", e.Name, e.Err, e.Output)
	if e.Uncertain {
		message += "; Shortcut outcome is uncertain; inspect the room before retrying"
	}
	return message
}

func (e *ShortcutError) Unwrap() error { return e.Err }

func (e *ShortcutError) OutcomeUncertain() bool { return e.Uncertain }

func ConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "homepodctl", "config.json"), nil
}

func LoadConfigOptional() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, &ConfigError{Op: "resolve", Err: err}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := &Config{}
			normalizeConfig(cfg)
			return cfg, nil
		}
		return nil, &ConfigError{Op: "read", Path: path, Err: err}
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, &ConfigError{Op: "parse", Path: path, Err: err}
	}
	normalizeConfig(&cfg)
	return &cfg, nil
}

// SaveConfig writes cfg without validating or normalizing its values.
// New files are created with mode 0600; existing file permissions are unchanged.
func SaveConfig(cfg *Config) error {
	path, err := ConfigPath()
	if err != nil {
		return &ConfigError{Op: "resolve", Err: err}
	}
	if cfg == nil {
		return &ConfigError{Op: "encode", Path: path, Err: errors.New("cannot save nil config")}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return &ConfigError{Op: "mkdir", Path: filepath.Dir(path), Err: err}
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return &ConfigError{Op: "encode", Path: path, Err: err}
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return &ConfigError{Op: "write", Path: path, Err: err}
	}
	return nil
}

func InitConfig() (string, error) {
	path, err := ConfigPath()
	if err != nil {
		return "", &ConfigError{Op: "resolve", Err: err}
	}
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	cfg := Config{
		Defaults: DefaultsConfig{
			Backend: "airplay",
			Rooms:   []string{},
		},
	}
	normalizeConfig(&cfg)

	if err := SaveConfig(&cfg); err != nil {
		return "", err
	}
	return path, nil
}

func normalizeConfig(cfg *Config) {
	if cfg.Native.Playlists == nil {
		cfg.Native.Playlists = map[string]map[string]string{}
	}
	if cfg.Native.VolumeShortcuts == nil {
		cfg.Native.VolumeShortcuts = map[string]map[string]string{}
	}
	if cfg.Aliases == nil {
		cfg.Aliases = map[string]Alias{}
	}
	if cfg.Defaults.Backend == "" {
		cfg.Defaults.Backend = "airplay"
	}
}

// A configured Shortcut may mutate playback before returning an error.
func RunShortcut(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	out, err := runShortcutExec(ctx, name)
	if err == nil {
		return nil
	}
	var launchErr *exec.Error
	var pathErr *os.PathError
	return &ShortcutError{Name: name, Err: err, Output: strings.TrimSpace(string(out)), Uncertain: !errors.As(err, &launchErr) && !errors.As(err, &pathErr)}
}
