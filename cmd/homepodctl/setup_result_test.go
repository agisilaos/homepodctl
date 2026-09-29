package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func TestSetupDiagnosticExitStatus(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		doctorFail, deviceFail, warnings bool
	}{
		{name: "success"}, {name: "warnings", warnings: true}, {name: "doctor failure", doctorFail: true},
		{name: "discovery failure", deviceFail: true}, {name: "both fail", doctorFail: true, deviceFail: true},
	} {
		for _, mode := range []struct {
			name        string
			json, quiet bool
		}{
			{name: "text"}, {name: "JSON", json: true}, {name: "quiet", quiet: true}, {name: "quiet JSON", json: true, quiet: true},
		} {
			t.Run(tc.name+"/"+mode.name, func(t *testing.T) {
				oldInit, oldLoad, oldPath := initConfig, loadConfigOptional, configPath
				oldLook, oldNow, oldDevices, oldQuiet := lookPath, getNowPlaying, listAirPlayDevices, quiet
				t.Cleanup(func() {
					initConfig, loadConfigOptional, configPath = oldInit, oldLoad, oldPath
					lookPath, getNowPlaying, listAirPlayDevices, quiet = oldLook, oldNow, oldDevices, oldQuiet
				})
				quiet = mode.quiet
				initConfig = func() (string, error) { return "/test/config.json", nil }
				configPath = initConfig
				loadConfigOptional = func() (*native.Config, error) { return &native.Config{}, nil }
				lookPath = func(name string) (string, error) {
					if (tc.doctorFail && name == "osascript") || (tc.warnings && name == "shortcuts") {
						return "", errors.New("missing")
					}
					return "/test/" + name, nil
				}
				getNowPlaying = func(context.Context) (music.NowPlaying, error) {
					if tc.warnings {
						return music.NowPlaying{}, errors.New("unreachable")
					}
					return music.NowPlaying{}, nil
				}
				discoveries := 0
				listAirPlayDevices = func(context.Context) ([]music.AirPlayDevice, error) {
					discoveries++
					if tc.deviceFail {
						return nil, &music.ScriptError{Output: "discovery failed"}
					}
					return nil, nil // Empty discovery is successful.
				}
				out, recovered := captureStdoutAndRecover(t, func() { cmdSetup(context.Background(), []string{fmt.Sprintf("--json=%t", mode.json)}) })
				wantOK := !tc.doctorFail && !tc.deviceFail
				if wantOK {
					if recovered != nil {
						t.Fatalf("unexpected exit: %v", recovered)
					}
				} else if exit, ok := recovered.(cliExit); !ok || exit.code != exitGeneric {
					t.Fatalf("exit=%v, want cliExit{1}", recovered)
				}
				if discoveries != 1 {
					t.Fatalf("discoveries=%d, want 1 even after doctor failure", discoveries)
				}
				if mode.json {
					var result setupResult
					if err := json.Unmarshal([]byte(out), &result); err != nil {
						t.Fatal(err)
					}
					if result.OK != wantOK || result.Doctor.OK == tc.doctorFail || (result.DeviceError != "") != tc.deviceFail || len(result.Next) == 0 {
						t.Fatalf("unexpected report: %+v", result)
					}
				} else if mode.quiet {
					if out != "" {
						t.Fatalf("quiet output=%q", out)
					}
				} else if !strings.Contains(out, fmt.Sprintf("setup ok=%t", wantOK)) || !strings.Contains(out, "next:") {
					t.Fatalf("incomplete report: %s", out)
				}
			})
		}
	}
}

// Exercise the real process exit boundary with a deterministic backend, without
// contacting Music.app. An empty successful discovery is a valid setup result.
func setupCLIHarness(t *testing.T, backendFails bool) *cliHarness {
	t.Helper()
	cli := newCLIHarness(t)
	dir := t.TempDir()
	script := "#!/bin/sh\nexit 0\n"
	if backendFails {
		script = "#!/bin/sh\necho 'backend unavailable' >&2\nexit 1\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "osascript"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	cli.env = append(cli.env, "PATH="+dir, "XDG_CONFIG_HOME="+filepath.Join(cli.home, "config"))
	return cli
}

func TestCLISetupDiagnosticExitModes(t *testing.T) {
	for _, fail := range []bool{false, true} {
		for _, mode := range []struct {
			name        string
			json, quiet bool
		}{
			{name: "text"}, {name: "JSON", json: true}, {name: "quiet", quiet: true}, {name: "quiet JSON", json: true, quiet: true},
		} {
			t.Run(fmt.Sprintf("failure=%t/%s", fail, mode.name), func(t *testing.T) {
				cli := setupCLIHarness(t, fail)
				args := []string{"setup", fmt.Sprintf("--json=%t", mode.json)}
				if mode.quiet {
					args = append([]string{"--quiet"}, args...)
				}
				result := cli.run(t, args...)
				wantExit := 0
				if fail {
					wantExit = exitGeneric
				}
				if result.ExitCode != wantExit || result.Stderr != "" {
					t.Fatalf("result=%+v, want exit=%d and no extra error", result, wantExit)
				}
				if mode.json {
					var report setupResult
					if err := json.Unmarshal([]byte(result.Stdout), &report); err != nil {
						t.Fatal(err)
					}
					if report.OK == fail {
						t.Fatalf("unexpected report: %+v", report)
					}
				} else if mode.quiet {
					if result.Stdout != "" {
						t.Fatalf("quiet output=%q", result.Stdout)
					}
				} else if !strings.Contains(result.Stdout, fmt.Sprintf("setup ok=%t", !fail)) {
					t.Fatalf("missing report: %s", result.Stdout)
				}
			})
		}
	}
}
