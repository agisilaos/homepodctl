package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
		t.Fatalf("plan native-run exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	assertGolden(t, "plan_native_run_json.txt", result.Stdout)
}

func TestCLIExitCodeContracts(t *testing.T) {
	cli := newCLIHarness(t)
	writeRoutine := func(name, content string) string {
		t.Helper()
		path := filepath.Join(cli.home, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	bad := writeRoutine("bad.yaml", "version: [\n")
	invalid := writeRoutine("invalid.json", `{"version":"2","name":"invalid","steps":[{"type":"transport","action":"stop"}]}`)
	stop := writeRoutine("stop.json", `{"version":"1","name":"stop","steps":[{"type":"transport","action":"stop"},{"type":"transport","action":"stop"}]}`)
	nativePlay := writeRoutine("native.json", `{"version":"1","name":"native","defaults":{"backend":"native","rooms":["Office"]},"steps":[{"type":"play","query":"Unmapped"},{"type":"transport","action":"stop"}]}`)
	nativeMapped := writeRoutine("native-mapped.json", `{"version":"1","name":"native-mapped","defaults":{"backend":"native","rooms":["Office"]},"steps":[{"type":"play","query":"Focus"},{"type":"transport","action":"stop"}]}`)
	wait := writeRoutine("wait.json", `{"version":"1","name":"wait","steps":[{"type":"wait","state":"playing","timeout":"1s"},{"type":"transport","action":"stop"}]}`)
	if result := cli.run(t, "config", "set", "native.playlists.Office.Focus", "Example"); result.ExitCode != 0 {
		t.Fatalf("configure native mapping: %+v", result)
	}

	// Exercise real process exits and output streams without controlling Music or
	// Shortcuts. Both backends resolve through this isolated PATH.
	backendDir := t.TempDir()
	for _, name := range []string{"osascript", "shortcuts"} {
		if err := os.WriteFile(filepath.Join(backendDir, name), []byte(`#!/bin/sh
echo called >> "$HOMEPODCTL_TEST_BACKEND_LOG"
if [ "$HOMEPODCTL_TEST_BACKEND_FAIL" = 1 ]; then
  echo 'synthetic backend failure' >&2
  exit 1
fi
echo stopped
`), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name        string
		args        []string
		want        int
		errorCode   string
		stepError   string
		backendFail bool
		backendCall bool
	}{
		{name: "config usage", args: []string{"config", "get", "defaults.backend", "unexpected"}, want: 2, errorCode: "USAGE_ERROR"},
		{name: "schema unknown", args: []string{"schema", "not-real"}, want: 2, errorCode: "USAGE_ERROR"},
		{name: "plan unsupported", args: []string{"plan", "pause"}, want: 2, errorCode: "USAGE_ERROR"},
		{name: "standalone backend failure", args: []string{"native-run", "--shortcut", "Example"}, want: 4, errorCode: "BACKEND_ERROR", backendFail: true, backendCall: true},
		{name: "missing file flag", args: []string{"automation", "run"}, want: 2, errorCode: "USAGE_ERROR"},
		{name: "unsupported dry-run alias", args: []string{"automation", "run", "-f", stop, "-n"}, want: 2, errorCode: "USAGE_ERROR"},
		{name: "invalid dry-run value", args: []string{"automation", "run", "-f", stop, "--dry-run=maybe"}, want: 2, errorCode: "USAGE_ERROR"},
		{name: "file read failure", args: []string{"automation", "run", "-f", filepath.Join(cli.home, "missing.yaml")}, want: 1, errorCode: "GENERIC_ERROR"},
		{name: "malformed file", args: []string{"automation", "validate", "-f", bad}, want: 3, errorCode: "AUTOMATION_VALIDATION_ERROR"},
		{name: "invalid routine", args: []string{"automation", "run", "-f", invalid}, want: 3, errorCode: "AUTOMATION_VALIDATION_ERROR"},
		{name: "plan wrapper child failure", args: []string{"plan", "automation", "run", "-f", invalid}, want: 1, errorCode: "GENERIC_ERROR"},
		{name: "validate offline", args: []string{"automation", "validate", "-f", nativePlay}, want: 0},
		{name: "plan offline", args: []string{"automation", "plan", "--file", nativePlay}, want: 0},
		{name: "dry-run offline", args: []string{"automation", "run", "-f", nativePlay, "--dry-run"}, want: 0},
		{name: "successful run", args: []string{"automation", "run", "-f", stop}, want: 0, backendCall: true},
		{name: "backend execution failure", args: []string{"automation", "run", "-f", stop}, want: 1, stepError: "synthetic backend failure", backendFail: true, backendCall: true},
		{name: "native backend execution failure", args: []string{"automation", "run", "-f", nativeMapped}, want: 1, stepError: "synthetic backend failure", backendFail: true, backendCall: true},
		{name: "missing native mapping", args: []string{"automation", "run", "-f", nativePlay}, want: 1, stepError: "no native mapping"},
		{name: "wait timeout", args: []string{"automation", "run", "-f", wait}, want: 1, stepError: "wait timeout after 1s", backendCall: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, format := range []string{"human", "json"} {
				t.Run(format, func(t *testing.T) {
					backendLog := filepath.Join(t.TempDir(), "backend.log")
					child := *cli
					child.env = append([]string(nil), cli.env...)
					child.env = append(child.env, "PATH="+backendDir+string(os.PathListSeparator)+os.Getenv("PATH"), "HOMEPODCTL_TEST_BACKEND_LOG="+backendLog)
					fail := "0"
					if tc.backendFail {
						fail = "1"
					}
					child.env = append(child.env, "HOMEPODCTL_TEST_BACKEND_FAIL="+fail)
					args := append([]string(nil), tc.args...)
					if format == "json" {
						args = append(args, "--json")
					}
					result := child.run(t, args...)
					if result.ExitCode != tc.want {
						t.Fatalf("exit=%d want=%d stdout=%s stderr=%s", result.ExitCode, tc.want, result.Stdout, result.Stderr)
					}
					log, err := os.ReadFile(backendLog)
					if err != nil && !os.IsNotExist(err) {
						t.Fatal(err)
					}
					if (len(log) > 0) != tc.backendCall {
						t.Fatalf("backend calls=%q want called=%t", log, tc.backendCall)
					}
					if tc.errorCode != "" {
						if result.Stdout != "" || result.Stderr == "" {
							t.Fatalf("command error must use stderr only: %+v", result)
						}
						if format == "json" {
							var payload jsonErrorResponse
							if err := json.Unmarshal([]byte(result.Stderr), &payload); err != nil {
								t.Fatal(err)
							}
							if payload.OK || payload.Error.Code != tc.errorCode || payload.Error.ExitCode != tc.want || payload.Error.Message == "" {
								t.Fatalf("unexpected error envelope: %+v", payload)
							}
						}
						return
					}
					if result.Stderr != "" {
						t.Fatalf("automation result should not emit a separate error: %s", result.Stderr)
					}
					if format == "human" {
						wantOK := "ok=true"
						if tc.want != 0 {
							wantOK = "ok=false"
						}
						if !strings.Contains(result.Stdout, wantOK) {
							t.Fatalf("missing %s: %s", wantOK, result.Stdout)
						}
						return
					}
					var payload struct {
						OK    bool `json:"ok"`
						Steps []struct {
							OK       bool            `json:"ok"`
							Skipped  bool            `json:"skipped"`
							Error    string          `json:"error"`
							Resolved json.RawMessage `json:"resolved"`
						} `json:"steps"`
					}
					if err := json.Unmarshal([]byte(result.Stdout), &payload); err != nil {
						t.Fatal(err)
					}
					if payload.OK != (tc.want == 0) || len(payload.Steps) != 2 {
						t.Fatalf("unexpected automation result: %s", result.Stdout)
					}
					if tc.stepError != "" {
						failed, skipped := payload.Steps[0], payload.Steps[1]
						if failed.OK || failed.Skipped || !strings.Contains(failed.Error, tc.stepError) || skipped.OK || !skipped.Skipped || len(failed.Resolved) == 0 || len(skipped.Resolved) == 0 {
							t.Fatalf("unexpected failed/skipped steps: %s", result.Stdout)
						}
					}
				})
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
