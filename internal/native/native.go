package native

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type Config struct {
	Defaults DefaultsConfig   `json:"defaults"`
	Aliases  map[string]Alias `json:"aliases"`
	Native   NativeConfig     `json:"native"`
}

type DefaultsConfig struct {
	Backend string   `json:"backend"`
	Rooms   []string `json:"rooms"`
	Shuffle bool     `json:"shuffle"`
	Volume  *int     `json:"volume"` // 0-100
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
	Name   string
	Err    error
	Output string
}

func (e *ShortcutError) Error() string {
	return fmt.Sprintf("shortcuts run %q failed: %v: %s", e.Name, e.Err, e.Output)
}

func (e *ShortcutError) Unwrap() error { return e.Err }

func ConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "homepodctl", "config.json"), nil
}

func LoadConfig() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, &ConfigError{Op: "resolve", Err: err}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, &ConfigError{Op: "read", Path: path, Err: fmt.Errorf("%w (run `homepodctl config-init`)", err)}
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, &ConfigError{Op: "parse", Path: path, Err: err}
	}
	normalizeConfig(&cfg)
	if cfg.Native.Playlists == nil {
		cfg.Native.Playlists = map[string]map[string]string{}
	}
	if cfg.Native.VolumeShortcuts == nil {
		cfg.Native.VolumeShortcuts = map[string]map[string]string{}
	}
	if cfg.Aliases == nil {
		cfg.Aliases = map[string]Alias{}
	}
	return &cfg, nil
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

func InitConfig() (string, error) {
	path, err := ConfigPath()
	if err != nil {
		return "", &ConfigError{Op: "resolve", Err: err}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", &ConfigError{Op: "mkdir", Path: filepath.Dir(path), Err: err}
	}
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	defaultVolume := 50
	cfg := Config{
		Defaults: DefaultsConfig{
			Backend: "airplay",
			Rooms:   []string{"Living Room"},
			Shuffle: false,
			Volume:  &defaultVolume,
		},
		Aliases: map[string]Alias{
			"bed": {
				Backend: "airplay",
				Rooms:   []string{"Bedroom"},
			},
			"lr": {
				Backend: "airplay",
				Rooms:   []string{"Living Room"},
			},
			"bed-example": {
				Backend:  "airplay",
				Rooms:    []string{"Bedroom"},
				Playlist: "Example Playlist",
				Volume:   &defaultVolume,
			},
		},
		Native: NativeConfig{
			Playlists: map[string]map[string]string{
				"Bedroom": {
					"Example Playlist": "BR Play Example Playlist",
				},
				"Living Room": {
					"Example Playlist": "LR Play Example Playlist",
				},
			},
			VolumeShortcuts: map[string]map[string]string{
				"Bedroom": {
					"30": "BR Volume 30",
				},
				"Living Room": {
					"30": "LR Volume 30",
				},
			},
		},
	}

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", &ConfigError{Op: "encode", Path: path, Err: err}
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", &ConfigError{Op: "write", Path: path, Err: err}
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

func RunShortcut(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "shortcuts", "run", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &ShortcutError{
			Name:   name,
			Err:    err,
			Output: string(out),
		}
	}
	return nil
}
