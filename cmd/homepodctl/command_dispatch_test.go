package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agisilaos/homepodctl/internal/native"
)

func TestCmdConfigDispatch_ValidateJSON(t *testing.T) {
	origLoad := loadConfigOptional
	origPath := configPath
	t.Cleanup(func() {
		loadConfigOptional = origLoad
		configPath = origPath
	})

	loadConfigOptional = func() (*native.Config, error) { return &native.Config{}, nil }
	configPath = func() (string, error) { return filepath.Join(t.TempDir(), "config.json"), nil }

	out, recovered := captureStdoutAndRecover(t, func() {
		cmdConfig([]string{"validate", "--json"})
	})
	if recovered != nil {
		t.Fatalf("unexpected panic: %v", recovered)
	}
	if !strings.Contains(out, `"ok": true`) {
		t.Fatalf("validate output=%q", out)
	}
}

func TestCmdConfigDispatch_SetAndGet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	path, err := native.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}

	out, recovered := captureStdoutAndRecover(t, func() {
		cmdConfig([]string{"set", "defaults.backend", "native"})
	})
	if recovered != nil {
		t.Fatalf("unexpected panic from set: %v", recovered)
	}
	if !strings.Contains(out, "Updated "+path) {
		t.Fatalf("set output=%q", out)
	}
	if b, err := os.ReadFile(path); err != nil || !strings.Contains(string(b), `"backend": "native"`) {
		t.Fatalf("written config invalid err=%v body=%q", err, string(b))
	}

	out, recovered = captureStdoutAndRecover(t, func() {
		cmdConfig([]string{"get", "defaults.backend"})
	})
	if recovered != nil {
		t.Fatalf("unexpected panic from get: %v", recovered)
	}
	if strings.TrimSpace(out) != "native" {
		t.Fatalf("get output=%q", out)
	}
}

func TestConfigCommandsReportSaveErrors(t *testing.T) {
	for _, command := range []string{"setup", "config set"} {
		t.Run(command, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			path, err := native.ConfigPath()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
			// Bypass loading to exercise the save failure, not a read failure.
			origLoad := loadConfigOptional
			t.Cleanup(func() { loadConfigOptional = origLoad })
			loadConfigOptional = func() (*native.Config, error) { return &native.Config{}, nil }
			out, recovered := captureStdoutAndRecover(t, func() {
				if command == "setup" {
					cmdSetup(context.Background(), []string{"--backend", "native"})
				} else {
					cmdConfig([]string{"set", "defaults.backend", "native"})
				}
			})
			fatal, ok := recovered.(cliFatal)
			if !ok {
				t.Fatalf("expected cliFatal, got %v", recovered)
			}
			var cfgErr *native.ConfigError
			if !errors.As(fatal.err, &cfgErr) || cfgErr.Op != "write" || cfgErr.Path != path {
				t.Fatalf("expected config write error for %s, got %v", path, fatal.err)
			}
			if classifyExitCode(fatal.err) != exitConfig || classifyErrorCode(fatal.err) != "CONFIG_ERROR" {
				t.Fatalf("incorrect save failure classification: %v", fatal.err)
			}
			if out != "" {
				t.Fatalf("unexpected success output: %q", out)
			}
		})
	}
}

func TestCmdAutomationDispatch_Direct(t *testing.T) {
	cfg := &native.Config{}

	out, recovered := captureStdoutAndRecover(t, func() {
		cmdAutomation(context.Background(), cfg, []string{"init", "--preset", "focus", "--json"})
	})
	if recovered != nil {
		t.Fatalf("unexpected panic from init: %v", recovered)
	}
	if !strings.Contains(out, `"preset": "focus"`) {
		t.Fatalf("init output=%q", out)
	}

	routinePath := filepath.Join(t.TempDir(), "routine.yaml")
	routine := `version: "1"
name: smoke
steps:
  - type: transport
    action: stop
`
	if err := os.WriteFile(routinePath, []byte(routine), 0o644); err != nil {
		t.Fatalf("write routine: %v", err)
	}

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "validate", args: []string{"validate", "-f", routinePath, "--json"}, want: `"mode": "validate"`},
		{name: "plan", args: []string{"plan", "-f", routinePath, "--json"}, want: `"mode": "plan"`},
		{name: "run dry-run", args: []string{"run", "-f", routinePath, "--dry-run", "--json"}, want: `"mode": "dry-run"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, recovered := captureStdoutAndRecover(t, func() {
				cmdAutomation(context.Background(), cfg, tc.args)
			})
			if recovered != nil {
				t.Fatalf("unexpected panic: %v", recovered)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("output=%q want contains %q", out, tc.want)
			}
		})
	}
}

func TestCmdCompletionDispatch_Direct(t *testing.T) {
	out, recovered := captureStdoutAndRecover(t, func() {
		cmdCompletion([]string{"bash"})
	})
	if recovered != nil {
		t.Fatalf("unexpected panic: %v", recovered)
	}
	if !strings.Contains(out, "homepodctl") {
		t.Fatalf("completion output=%q", out)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	targetDir := filepath.Join(home, "completions")
	out, recovered = captureStdoutAndRecover(t, func() {
		cmdCompletion([]string{"install", "bash", "--path", targetDir})
	})
	if recovered != nil {
		t.Fatalf("unexpected panic from completion install: %v", recovered)
	}
	if !strings.Contains(out, "Installed bash completion:") {
		t.Fatalf("completion install output=%q", out)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "homepodctl")); err != nil {
		t.Fatalf("completion file missing: %v", err)
	}
}

func TestUsageOutputContainsCoreCommands(t *testing.T) {
	out := captureStderr(t, usage)
	if !strings.Contains(out, "homepodctl [--verbose] [--quiet] <command> [args]") {
		t.Fatalf("usage output=%q", out)
	}
	if !strings.Contains(out, "automation") || !strings.Contains(out, "config") {
		t.Fatalf("usage output missing expected commands: %q", out)
	}
	if !strings.Contains(out, "homepodctl tui [--refresh <duration>]") {
		t.Fatalf("usage output missing tui command: %q", out)
	}
}

func captureStdoutAndRecover(t *testing.T, fn func()) (string, any) {
	t.Helper()
	var recovered any
	out := captureStdout(t, func() {
		defer func() {
			recovered = recover()
		}()
		fn()
	})
	return out, recovered
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close write pipe: %v", err)
	}
	buf, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured output: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close read pipe: %v", err)
	}
	return string(buf)
}
