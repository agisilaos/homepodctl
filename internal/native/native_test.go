package native

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestShouldRetryShortcut(t *testing.T) {
	t.Parallel()

	if !shouldRetryShortcut(errors.New("exit"), "The operation timed out. Please try again.") {
		t.Fatalf("expected timeout output to be retryable")
	}
	if shouldRetryShortcut(context.Canceled, "timed out") {
		t.Fatalf("context cancellation should not be retried")
	}
	if shouldRetryShortcut(errors.New("exit"), "No shortcut named Bedroom Play") {
		t.Fatalf("missing shortcut should not be retried")
	}
}

func TestRunShortcut_RetriesTransientThenSucceeds(t *testing.T) {
	origExec := runShortcutExec
	origSleep := sleepWithContextFn
	t.Cleanup(func() {
		runShortcutExec = origExec
		sleepWithContextFn = origSleep
	})

	attempts := 0
	runShortcutExec = func(context.Context, string) ([]byte, error) {
		attempts++
		if attempts < 3 {
			return []byte("The operation timed out. Please try again."), errors.New("boom")
		}
		return []byte("ok"), nil
	}
	sleepWithContextFn = func(context.Context, time.Duration) error { return nil }

	if err := RunShortcut(context.Background(), "Demo"); err != nil {
		t.Fatalf("RunShortcut: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts=%d, want 3", attempts)
	}
}

func TestRunShortcut_FailFastOnPermanentError(t *testing.T) {
	origExec := runShortcutExec
	origSleep := sleepWithContextFn
	t.Cleanup(func() {
		runShortcutExec = origExec
		sleepWithContextFn = origSleep
	})

	attempts := 0
	runShortcutExec = func(context.Context, string) ([]byte, error) {
		attempts++
		return []byte("No shortcut named Bedroom Play"), errors.New("boom")
	}
	sleepWithContextFn = func(context.Context, time.Duration) error { return nil }

	if err := RunShortcut(context.Background(), "Missing"); err == nil {
		t.Fatalf("expected error")
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d, want 1", attempts)
	}
}
