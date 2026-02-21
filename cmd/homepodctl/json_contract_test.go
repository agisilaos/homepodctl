package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func TestGoldenSchemaActionResultJSON(t *testing.T) {
	got := captureStdout(t, func() {
		cmdSchema([]string{"action-result", "--json"})
	})
	assertGolden(t, "schema_action_result_json.txt", got)
}

func TestGoldenAutomationDryRunJSON(t *testing.T) {
	f := filepath.Join(t.TempDir(), "routine.yaml")
	yaml := `version: "1"
name: test-routine
steps:
  - type: out.set
    rooms: ["Bedroom"]
  - type: play
    query: "Focus"
`
	if err := os.WriteFile(f, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write routine: %v", err)
	}
	cfg := &native.Config{
		Defaults: native.DefaultsConfig{
			Backend: "airplay",
			Rooms:   []string{"Bedroom"},
		},
	}
	got := captureStdout(t, func() {
		cmdAutomationRun(context.Background(), cfg, []string{"-f", f, "--dry-run", "--json"})
	})
	got = normalizeJSONFields(t, got, map[string]any{
		"startedAt":  "<timestamp>",
		"endedAt":    "<timestamp>",
		"durationMs": float64(0),
	})
	assertGolden(t, "automation_dry_run_json.txt", got)
}

func TestGoldenDoctorReportJSON(t *testing.T) {
	origLookPath := lookPath
	origConfigPath := configPath
	origLoadConfig := loadConfigOptional
	origGetNowPlaying := getNowPlaying
	t.Cleanup(func() {
		lookPath = origLookPath
		configPath = origConfigPath
		loadConfigOptional = origLoadConfig
		getNowPlaying = origGetNowPlaying
	})

	lookPath = func(string) (string, error) { return "/usr/bin/fake", nil }
	configPath = func() (string, error) { return "/tmp/homepodctl/config.json", nil }
	loadConfigOptional = func() (*native.Config, error) {
		return &native.Config{Aliases: map[string]native.Alias{"bed": {}}}, nil
	}
	getNowPlaying = func(context.Context) (music.NowPlaying, error) {
		return music.NowPlaying{PlayerState: "playing"}, nil
	}

	report := runDoctorChecks(context.Background())
	report.CheckedAt = "<timestamp>"
	got := captureStdout(t, func() { writeJSON(report) })
	assertGolden(t, "doctor_report_json.txt", got)
}

func TestGoldenPlanNativeRunJSON(t *testing.T) {
	bin := buildCLIBinary(t)
	code, out := runCLI(t, bin, t.TempDir(), "plan", "native-run", "--shortcut", "Example", "--json")
	if code != 0 {
		t.Fatalf("plan native-run exit=%d out=%s", code, out)
	}
	assertGolden(t, "plan_native_run_json.txt", out)
}

func TestCLIExitCodeContracts(t *testing.T) {
	bin := buildCLIBinary(t)
	home := t.TempDir()
	bad := filepath.Join(home, "bad.yaml")
	if err := os.WriteFile(bad, []byte("version: \"2\"\nname: bad\nsteps:\n  - type: wait\n    state: playing\n    timeout: 20s\n"), 0o644); err != nil {
		t.Fatalf("write bad routine: %v", err)
	}

	cases := []struct {
		name string
		args []string
		want int
	}{
		{name: "config usage", args: []string{"config", "set", "defaults.backend", "invalid"}, want: exitUsage},
		{name: "automation validation", args: []string{"automation", "validate", "-f", bad}, want: exitConfig},
		{name: "schema unknown", args: []string{"schema", "not-real"}, want: exitUsage},
		{name: "plan unsupported", args: []string{"plan", "pause"}, want: exitUsage},
		{name: "native backend failure", args: []string{"native-run", "--shortcut", "__definitely_missing_shortcut__"}, want: exitBackend},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, out := runCLI(t, bin, home, tc.args...)
			if code != tc.want {
				t.Fatalf("args=%v exit=%d want=%d out=%s", tc.args, code, tc.want, out)
			}
		})
	}
}

func runCLI(t *testing.T, bin, home string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), string(out)
	}
	t.Fatalf("run %v: %v", args, err)
	return 1, ""
}

func normalizeJSONFields(t *testing.T, raw string, fields map[string]any) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal json: %v raw=%s", err, raw)
	}
	for k, v := range fields {
		if _, ok := payload[k]; ok {
			payload[k] = v
		}
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(b) + "\n"
}
