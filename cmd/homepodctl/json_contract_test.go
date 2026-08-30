package main

import (
	"context"
	"encoding/json"
	"os"
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
	cli := newCLIHarness(t)
	result := cli.run(t, "plan", "native-run", "--shortcut", "Example", "--json")
	if result.ExitCode != 0 {
		t.Fatalf("plan native-run exit=%d out=%s", result.ExitCode, result.Stdout)
	}
	assertGolden(t, "plan_native_run_json.txt", result.Stdout)
}

func TestCLIExitCodeContracts(t *testing.T) {
	cli := newCLIHarness(t)
	bad := filepath.Join(cli.home, "bad.yaml")
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
			result := cli.run(t, tc.args...)
			if result.ExitCode != tc.want {
				t.Fatalf("args=%v exit=%d want=%d stderr=%s", tc.args, result.ExitCode, tc.want, result.Stderr)
			}
		})
	}
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
