package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func setupWorkflowEnvironment(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	oldLook, oldNow, oldDevices, oldPlaylists := lookPath, getNowPlaying, listAirPlayDevices, listUserPlaylists
	oldTerminal, oldQuiet := setupInputIsTerminal, quiet
	t.Cleanup(func() {
		lookPath, getNowPlaying, listAirPlayDevices, listUserPlaylists = oldLook, oldNow, oldDevices, oldPlaylists
		setupInputIsTerminal, quiet = oldTerminal, oldQuiet
	})
	quiet = false
	setupInputIsTerminal = func() bool { return true }
	lookPath = func(name string) (string, error) { return "/test/" + name, nil }
	getNowPlaying = func(context.Context) (music.NowPlaying, error) { return music.NowPlaying{}, nil }
	listAirPlayDevices = func(ctx context.Context) ([]music.AirPlayDevice, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("device discovery has no deadline")
		}
		return []music.AirPlayDevice{{Name: "Kitchen", Available: true, NetworkAddress: "private"}}, nil
	}
	listUserPlaylists = func(ctx context.Context, query string, limit int) ([]music.UserPlaylist, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("playlist discovery has no deadline")
		}
		if query != "" || limit != 0 {
			t.Errorf("setup enumeration query=%q limit=%d", query, limit)
		}
		return []music.UserPlaylist{{Name: "Zebra", PersistentID: "Z"}, {Name: "Focus", PersistentID: "F"}}, nil
	}
	path, err := native.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func setupWorkflowInput(t *testing.T, text string) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = old; _ = f.Close() })
}

func TestSetupGuidedCancellationPreservesConfiguration(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, input := range []string{"1\nq\n", "1\n"} {
			t.Run(strings.ReplaceAll(input, "\n", "/")+stringBool(existing), func(t *testing.T) {
				path := setupWorkflowEnvironment(t)
				var before []byte
				if existing {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					before = []byte(`{"defaults":{"backend":"airplay","rooms":["Offline"],"playlistId":"Z","volume":17},"aliases":{"night":{"rooms":["Offline"]}}}`)
					if err := os.WriteFile(path, before, 0o640); err != nil {
						t.Fatal(err)
					}
				}
				setupWorkflowInput(t, input)
				_, recovered := captureStdoutAndRecover(t, func() { cmdSetup(context.Background(), []string{"--choose"}) })
				fatal, ok := recovered.(cliFatal)
				if !ok || !strings.Contains(fatal.err.Error(), "cancelled") {
					t.Fatalf("expected cancellation, got %v", recovered)
				}
				after, err := os.ReadFile(path)
				if existing {
					if err != nil || string(after) != string(before) {
						t.Fatalf("cancel changed saved config: %s, err=%v", after, err)
					}
				} else if !os.IsNotExist(err) {
					t.Fatalf("cancel created config: %s, err=%v", after, err)
				}
			})
		}
	}
}

func TestSetupGuidedCompletionSavesPlaybackDefaults(t *testing.T) {
	setupWorkflowEnvironment(t)
	setupWorkflowInput(t, "1\nfocus\n1\n")
	out, recovered := captureStdoutAndRecover(t, func() { cmdSetup(context.Background(), []string{"--choose"}) })
	if recovered != nil {
		t.Fatal(recovered)
	}
	cfg, err := native.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Defaults.Rooms, []string{"Kitchen"}) || cfg.Defaults.PlaylistID != "F" || cfg.Defaults.Volume != nil || len(cfg.Aliases) != 0 || len(cfg.Native.Playlists) != 0 {
		t.Fatalf("unexpected saved preferences: %+v", cfg)
	}
	if !strings.Contains(out, "- homepodctl play\n") || !strings.Contains(out, "- homepodctl status\n") {
		t.Fatalf("saved playback defaults have no simple next step: %s", out)
	}
}

func TestSetupExplicitPlaylistSavesDefaultsWithoutSelection(t *testing.T) {
	for _, choose := range []bool{false, true} {
		t.Run(stringBool(choose), func(t *testing.T) {
			setupWorkflowEnvironment(t)
			setupWorkflowInput(t, "")
			args := []string{"--room", "Kitchen", "--playlist-id", "F"}
			if choose {
				args = append(args, "--choose")
			} else {
				args = append(args, "--no-input", "--json")
			}
			out, recovered := captureStdoutAndRecover(t, func() { cmdSetup(context.Background(), args) })
			if recovered != nil {
				t.Fatal(recovered)
			}
			cfg, err := native.LoadConfig()
			if err != nil || cfg.Defaults.PlaylistID != "F" || !reflect.DeepEqual(cfg.Defaults.Rooms, []string{"Kitchen"}) {
				t.Fatalf("explicit defaults not saved: %+v %v", cfg, err)
			}
			if !choose {
				var report setupResult
				if err := json.Unmarshal([]byte(out), &report); err != nil {
					t.Fatal(err)
				}
				if report.Defaults.PlaylistID != "F" || !report.ConfigUpdated || len(report.Next) < 2 || report.Next[0] != "homepodctl play" || report.Next[1] != "homepodctl status" {
					t.Fatalf("unexpected next step: %+v", report)
				}
			}
		})
	}
}

func TestSetupSkippingPlaylistPreservesSavedDefault(t *testing.T) {
	path := setupWorkflowEnvironment(t)
	if err := native.SaveConfig(&native.Config{Defaults: native.DefaultsConfig{Backend: "airplay", Rooms: []string{"Kitchen"}, PlaylistID: "Z"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	setupWorkflowInput(t, "\n\n")
	_, recovered := captureStdoutAndRecover(t, func() { cmdSetup(context.Background(), []string{"--choose"}) })
	if recovered != nil {
		t.Fatal(recovered)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("skipping preferences changed saved default playlist")
	}
}

func TestSetupGuidedSkippingReportsSavedDefaults(t *testing.T) {
	setupWorkflowEnvironment(t)
	setupWorkflowInput(t, "\n\n")
	out, recovered := captureStdoutAndRecover(t, func() { cmdSetup(context.Background(), []string{"--choose"}) })
	if recovered != nil {
		t.Fatal(recovered)
	}
	cfg, err := native.LoadConfig()
	if err != nil || cfg.Defaults.Rooms == nil || len(cfg.Defaults.Rooms) != 0 {
		t.Fatalf("unexpected fresh defaults: %+v %v", cfg, err)
	}
	if !strings.Contains(out, "updated=false") || !strings.Contains(out, "--room='Kitchen'") {
		t.Fatalf("unexpected skipped setup report: %s", out)
	}
}

func TestSetupPlaylistFailureIsNonfatalAndPreservesExplicitRooms(t *testing.T) {
	setupWorkflowEnvironment(t)
	listUserPlaylists = func(context.Context, string, int) ([]music.UserPlaylist, error) {
		return nil, errors.New("library denied")
	}
	out, recovered := captureStdoutAndRecover(t, func() {
		cmdSetup(context.Background(), []string{"--room", "Offline", "--no-input", "--json"})
	})
	if recovered != nil {
		t.Fatal(recovered)
	}
	var report setupResult
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.PlaylistError != "library denied" || len(report.Warnings) < 2 || len(report.Playlists) != 0 || len(report.Devices) != 1 || report.Devices[0].NetworkAddress != "" {
		t.Fatalf("unexpected report: %+v", report)
	}
	for _, command := range report.Next {
		if strings.Contains(command, "homepodctl play ") {
			t.Fatalf("suggested playback despite missing room and playlists: %s", command)
		}
	}
	cfg, err := native.LoadConfig()
	if err != nil || !reflect.DeepEqual(cfg.Defaults.Rooms, []string{"Offline"}) {
		t.Fatalf("requested default not saved: %+v %v", cfg, err)
	}
}

func TestSetupMinimalConfigurationNeedsNoAliasRecovery(t *testing.T) {
	setupWorkflowEnvironment(t)
	out, recovered := captureStdoutAndRecover(t, func() {
		cmdSetup(context.Background(), []string{"--no-input", "--json"})
	})
	if recovered != nil {
		t.Fatal(recovered)
	}
	var report setupResult
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	for _, check := range report.Doctor.Checks {
		if check.Name == "config" {
			if check.Status != "pass" || check.Tip != "" {
				t.Fatalf("minimal config suggests unnecessary recovery: %+v", check)
			}
			return
		}
	}
	t.Fatal("missing config diagnostic")
}

func TestSetupRejectsChooseForSavedNativeBackend(t *testing.T) {
	path := setupWorkflowEnvironment(t)
	if err := native.SaveConfig(&native.Config{Defaults: native.DefaultsConfig{Backend: "native"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	_, recovered := captureStdoutAndRecover(t, func() { cmdSetup(context.Background(), []string{"--choose"}) })
	fatal, ok := recovered.(cliFatal)
	if !ok || classifyExitCode(fatal.err) != exitUsage {
		t.Fatalf("expected usage error, got %v", recovered)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("native chooser rejection modified config")
	}
}

func stringBool(value bool) string {
	if value {
		return "existing"
	}
	return "new"
}
