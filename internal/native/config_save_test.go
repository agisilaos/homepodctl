package native

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func isolatedConfigPath(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	path, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSaveConfigRoundTrip(t *testing.T) {
	path := isolatedConfigPath(t)
	volume, shuffle := 0, false
	want := &Config{
		Defaults: DefaultsConfig{Backend: "native", Rooms: []string{"Bedroom", "Living Room"}, Shuffle: true, Volume: &volume},
		Aliases: map[string]Alias{
			"bed": {
				Backend: "airplay", Rooms: []string{"Bedroom"}, Playlist: "Sleep",
				PlaylistID: "ABC123", Shuffle: &shuffle, Volume: &volume, Shortcut: "Bedtime",
			},
		},
		Native: NativeConfig{
			Playlists:       map[string]map[string]string{"Bedroom": {"Sleep": "Play Sleep"}},
			VolumeShortcuts: map[string]map[string]string{"Bedroom": {"0": "Mute Bedroom"}},
		},
	}
	if err := SaveConfig(want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip: got %#v, want %#v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("new file mode=%#o, want 0600", got)
	}

	// A shorter replacement must not retain trailing bytes from the old config.
	want.Aliases = map[string]Alias{}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(want); err != nil {
		t.Fatal(err)
	}
	got, err = LoadConfig()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("replacement round trip: got %#v, err=%v", got, err)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("existing file mode=%#o, want 0644", got)
	}
}

func TestSaveConfigPreservesEncoding(t *testing.T) {
	path := isolatedConfigPath(t)
	if err := SaveConfig(&Config{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Saving does not apply load-time defaults or normalize nil collections.
	const want = `{
  "defaults": {
    "backend": "",
    "rooms": null,
    "shuffle": false,
    "volume": null
  },
  "aliases": null,
  "native": {
    "playlists": null,
    "volumeShortcuts": null
  }
}`
	if string(data) != want {
		t.Fatalf("encoded config=%s, want %s", data, want)
	}
}

func TestSaveConfigRejectsNilWithoutFilesystemChanges(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "missing"
		if existing {
			name = "existing"
		}
		t.Run(name, func(t *testing.T) {
			path := isolatedConfigPath(t)
			const original = `{"defaults":{"backend":"native"}}`
			if existing {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err := SaveConfig(nil)
			var cfgErr *ConfigError
			if !errors.As(err, &cfgErr) || cfgErr.Op != "encode" || cfgErr.Path != path {
				t.Fatalf("expected encode ConfigError for %s, got %v", path, err)
			}
			if existing {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != original {
					t.Fatalf("nil save changed existing config: data=%q err=%v", data, err)
				}
			} else if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("nil save created config directory: %v", err)
			}
		})
	}
}

func TestSaveConfigErrors(t *testing.T) {
	for _, op := range []string{"resolve", "mkdir", "write"} {
		t.Run(op, func(t *testing.T) {
			path := isolatedConfigPath(t)
			wantPath := path
			switch op {
			case "resolve":
				t.Setenv("HOME", "")
				t.Setenv("XDG_CONFIG_HOME", "")
				wantPath = ""
			case "mkdir":
				wantPath = filepath.Dir(path)
				if err := os.MkdirAll(filepath.Dir(wantPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(wantPath, []byte("not a directory"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "write":
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			err := SaveConfig(&Config{})
			var cfgErr *ConfigError
			if !errors.As(err, &cfgErr) || cfgErr.Op != op || cfgErr.Path != wantPath {
				t.Fatalf("expected %s ConfigError for %q, got %v", op, wantPath, err)
			}
			if cfgErr.Err == nil || !errors.Is(err, cfgErr.Err) {
				t.Fatalf("underlying error was not preserved: %v", err)
			}
			if op != "resolve" {
				var pathErr *os.PathError
				if !errors.As(err, &pathErr) {
					t.Fatalf("filesystem error was not preserved: %v", err)
				}
			}
		})
	}
}

func TestInitConfigCreatesDefaults(t *testing.T) {
	path := isolatedConfigPath(t)
	gotPath, err := InitConfig()
	if err != nil || gotPath != path {
		t.Fatalf("InitConfig path=%q, err=%v", gotPath, err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.Backend != "airplay" || !reflect.DeepEqual(cfg.Defaults.Rooms, []string{"Living Room"}) || cfg.Defaults.Volume == nil || *cfg.Defaults.Volume != 50 {
		t.Fatalf("unexpected starter defaults: %+v", cfg.Defaults)
	}
	if len(cfg.Aliases) != 3 || len(cfg.Native.Playlists) != 2 || len(cfg.Native.VolumeShortcuts) != 2 {
		t.Fatalf("missing starter mappings: %+v", cfg)
	}
}

func TestInitConfigLeavesExistingConfigUntouched(t *testing.T) {
	path := isolatedConfigPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Initialization must leave even invalid JSON alone, without changing its mode.
	const original = "{not valid JSON\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	gotPath, err := InitConfig()
	if err != nil || gotPath != path {
		t.Fatalf("InitConfig path=%q, err=%v", gotPath, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != original {
		t.Fatalf("existing config changed: data=%q err=%v", data, err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("initialization changed existing config metadata")
	}
}
