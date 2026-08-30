package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agisilaos/homepodctl/internal/native"
)

func TestParseSetupOptionsPreservesAcceptedInputs(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want setupOptions
	}{
		{"defaults", nil, setupOptions{}},
		{"empty backend", []string{"--backend", ""}, setupOptions{}},
		{"blank backend", []string{"--backend", " \t "}, setupOptions{}},
		{"trimmed backend", []string{"--backend", " native "}, setupOptions{backend: "native"}},
		{"last backend", []string{"--backend", "invalid", "--backend", "airplay"}, setupOptions{backend: "airplay"}},
		{"rooms verbatim", []string{"--room", " Bedroom ", "--room", "Kitchen", "--room", " Bedroom "}, setupOptions{rooms: []string{" Bedroom ", "Kitchen", " Bedroom "}}},
		{"flag-like room", []string{"--room", "--json"}, setupOptions{rooms: []string{"--json"}}},
		{"empty equals room", []string{"--room=", "Bedroom"}, setupOptions{rooms: []string{"Bedroom"}}},
		{"bare booleans", []string{"--json", "--no-input"}, setupOptions{jsonOut: true}},
		{"boolean aliases", []string{"--json=YES", "--no-input=off"}, setupOptions{jsonOut: true}},
		{"last boolean", []string{"--json=invalid", "--json=false", "--no-input=0"}, setupOptions{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSetupOptions(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if got.backend != tc.want.backend || got.jsonOut != tc.want.jsonOut || !slices.Equal(got.rooms, tc.want.rooms) {
				t.Errorf("options=%#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestSetupRejectsInvalidInputBeforeConfigAccess(t *testing.T) {
	bin := buildCLIBinary(t)

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"backend", []string{"--backend", "invalid"}, `unknown backend: "invalid"`},
		{"backend case", []string{"--backend", "AirPlay"}, `unknown backend: "AirPlay"`},
		{"json", []string{"--json=invalid"}, "invalid --json"},
		{"no-input", []string{"--no-input=invalid"}, "invalid --no-input"},
		{"empty room", []string{"--room", ""}, "defaults.rooms[0] must be non-empty"},
		{"blank room", []string{"--room", " \t "}, "defaults.rooms[0] must be non-empty"},
		{"later room", []string{"--room", "Bedroom", "--room", ""}, "defaults.rooms[1] must be non-empty"},
		{"missing value", []string{"--room"}, "--room requires a value"},
		{"positional", []string{"Bedroom"}, "usage: homepodctl setup"},
		{"unknown flag", []string{"--unknown"}, "unknown flag: --unknown"},
		{"formerly ignored flags", []string{"--volume", "invalid", "--shuffle=invalid"}, "unknown flag: --volume"},
		{"unrelated boolean", []string{"--shuffle=invalid"}, "unknown flag: --shuffle"},
		{"unsupported output", []string{"--plain"}, "unknown flag: --plain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origInit, origLoad := initConfig, loadConfigOptional
			t.Cleanup(func() {
				initConfig, loadConfigOptional = origInit, origLoad
			})
			initConfig = func() (string, error) {
				t.Error("invalid setup input called initConfig")
				return "", errors.New("unexpected config initialization")
			}
			loadConfigOptional = func() (*native.Config, error) {
				t.Error("invalid setup input called loadConfigOptional")
				return nil, errors.New("unexpected config load")
			}

			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			path, err := native.ConfigPath()
			if err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command(bin, append([]string{"setup"}, tc.args...)...)
			out, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != exitUsage {
				t.Fatalf("setup %v: err=%v output=%s", tc.args, err, out)
			}
			if !strings.Contains(string(out), tc.want) {
				t.Errorf("output=%s, want %q", out, tc.want)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("invalid input created config at %s: %v", path, err)
			}
			entries, err := os.ReadDir(home)
			if err != nil || len(entries) != 0 {
				t.Errorf("invalid input modified isolated home: entries=%v err=%v", entries, err)
			}

			_, recovered := captureStdoutAndRecover(t, func() {
				cmdSetup(context.Background(), tc.args)
			})
			fatal, ok := recovered.(cliFatal)
			if !ok || classifyExitCode(fatal.err) != exitUsage {
				t.Fatalf("expected usage error before config access, got %v", recovered)
			}
		})
	}
}
