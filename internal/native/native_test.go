package native

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigOptional_MissingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := LoadConfigOptional()
	if err != nil {
		t.Fatalf("LoadConfigOptional: %v", err)
	}
	if cfg.Defaults.Backend != "airplay" {
		t.Fatalf("defaults.backend=%q, want airplay", cfg.Defaults.Backend)
	}
	if cfg.Aliases == nil {
		t.Fatalf("aliases should be initialized")
	}
	if cfg.Native.Playlists == nil {
		t.Fatalf("native.playlists should be initialized")
	}
	if cfg.Native.VolumeShortcuts == nil {
		t.Fatalf("native.volumeShortcuts should be initialized")
	}
}

func TestLoadConfigOptional_ParseError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{bad json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err = LoadConfigOptional()
	if err == nil {
		t.Fatalf("expected parse error")
	}
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected ConfigError, got %T", err)
	}
	if cfgErr.Op != "parse" {
		t.Fatalf("ConfigError.Op=%q, want parse", cfgErr.Op)
	}
}

func TestLoadConfigOptional_ValidConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data := []byte(`{
  "defaults": { "backend": "native", "rooms": ["Bedroom"], "shuffle": true, "volume": 30 },
  "aliases": { "bed": { "backend": "airplay", "rooms": ["Bedroom"] } },
  "native": { "playlists": {}, "volumeShortcuts": {} }
}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadConfigOptional()
	if err != nil {
		t.Fatalf("LoadConfigOptional: %v", err)
	}
	if cfg.Defaults.Backend != "native" {
		t.Fatalf("defaults.backend=%q, want native", cfg.Defaults.Backend)
	}
	if len(cfg.Aliases) != 1 {
		t.Fatalf("len(aliases)=%d, want 1", len(cfg.Aliases))
	}
}

func TestShortcutFailureNeverRetries(t *testing.T) {
	old := runShortcutExec
	defer func() { runShortcutExec = old }()
	for _, message := range []string{"The operation timed out. Please try again.", "No shortcut named Bedroom Play"} {
		calls := 0
		runShortcutExec = func(context.Context, string) ([]byte, error) { calls++; return []byte(message), errors.New("exit 1") }
		err := RunShortcut(context.Background(), "Demo")
		var se *ShortcutError
		if !errors.As(err, &se) || !se.Uncertain || calls != 1 {
			t.Fatalf("calls=%d error=%v", calls, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runShortcutExec = func(context.Context, string) ([]byte, error) { t.Fatal("cancelled call dispatched"); return nil, nil }
	if err := RunShortcut(ctx, "Demo"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
