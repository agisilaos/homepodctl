package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCLIDryRunCommands(t *testing.T) {
	cli := newCLIHarness(t)

	if result := cli.run(t, "config-init"); result.ExitCode != 0 {
		t.Fatalf("config-init exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if result := cli.run(t, "config", "set", "defaults.backend", "native"); result.ExitCode != 0 {
		t.Fatalf("config set defaults.backend native exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}

	assertDryRun := func(args ...string) {
		t.Helper()
		result := cli.run(t, args...)
		if result.ExitCode != 0 {
			t.Fatalf("%v exit=%d stdout=%s", args, result.ExitCode, result.Stdout)
		}
		if !strings.Contains(result.Stdout, `"dryRun": true`) {
			t.Fatalf("%v output missing dryRun=true: %s", args, result.Stdout)
		}
	}

	assertDryRun("out", "set", "Bedroom", "--dry-run", "--json")
	assertDryRun("out", "set", "--room", "Bedroom", "--dry-run", "--json")
	assertDryRun("volume", "30", "--dry-run", "--json")
	assertDryRun("play", "chill", "--dry-run", "--json")
	assertDryRun("run", "bed", "--dry-run", "--json")
	assertDryRun("native-run", "--shortcut", "Example", "--dry-run", "--json")

	result := cli.run(t, "out", "set", "Bedroom", "--dry-run", "--json")
	if result.ExitCode != 0 {
		t.Fatalf("out set dry-run with defaults.backend=native exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if !strings.Contains(result.Stdout, `"backend": "airplay"`) {
		t.Fatalf("out set backend should be airplay, stdout=%s", result.Stdout)
	}
}

func TestCLIGlobalVersionFlag(t *testing.T) {
	cli := newCLIHarness(t)
	result := cli.run(t, "--version")
	if result.ExitCode != 0 {
		t.Fatalf("--version exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if !strings.Contains(result.Stdout, "homepodctl ") {
		t.Fatalf("unexpected --version output: %s", result.Stdout)
	}
}

func TestCLIQuietSuppressesDryRunOutput(t *testing.T) {
	cli := newCLIHarness(t)

	if result := cli.run(t, "config-init"); result.ExitCode != 0 {
		t.Fatalf("config-init exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	result := cli.run(t, "--quiet", "out", "set", "--room", "Bedroom", "--dry-run")
	if result.ExitCode != 0 {
		t.Fatalf("quiet out set dry-run exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if strings.TrimSpace(result.Stdout) != "" || strings.TrimSpace(result.Stderr) != "" {
		t.Fatalf("expected quiet output to be empty, stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
}

func TestCLISetupJSON(t *testing.T) {
	cli := newCLIHarness(t)

	result := cli.run(t, "setup", "--json", "--no-input")
	if result.ExitCode != 0 {
		t.Fatalf("setup --json exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Stdout), &payload); err != nil {
		t.Fatalf("setup json parse: %v stdout=%s", err, result.Stdout)
	}
	if _, ok := payload["configPath"]; !ok {
		t.Fatalf("setup payload missing configPath: %v", payload)
	}
	if _, ok := payload["doctor"]; !ok {
		t.Fatalf("setup payload missing doctor: %v", payload)
	}
}

func TestCLISetupPersistsDefaults(t *testing.T) {
	cli := newCLIHarness(t)

	result := cli.run(t, "setup", "--backend", "native", "--room", "Bedroom", "--json", "--no-input")
	if result.ExitCode != 0 {
		t.Fatalf("setup persist defaults exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}

	result = cli.run(t, "config", "get", "defaults.backend", "--json")
	if result.ExitCode != 0 || !strings.Contains(result.Stdout, `"value": "native"`) {
		t.Fatalf("defaults.backend not updated exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	result = cli.run(t, "config", "get", "defaults.rooms", "--json")
	if result.ExitCode != 0 || !strings.Contains(result.Stdout, `"Bedroom"`) {
		t.Fatalf("defaults.rooms not updated exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
}

func TestCLIDryRunErrorPaths(t *testing.T) {
	cli := newCLIHarness(t)

	if result := cli.run(t, "config-init"); result.ExitCode != 0 {
		t.Fatalf("config-init exit=%d stderr=%s", result.ExitCode, result.Stderr)
	}

	assertUsage := func(args []string, contains string) {
		t.Helper()
		result := cli.run(t, args...)
		if result.ExitCode != exitUsage {
			t.Fatalf("%v exit=%d want=%d stderr=%s", args, result.ExitCode, exitUsage, result.Stderr)
		}
		if !strings.Contains(strings.ToLower(result.Stderr), strings.ToLower(contains)) {
			t.Fatalf("%v output missing %q: %s", args, contains, result.Stderr)
		}
	}

	assertUsage([]string{"out", "set", "Bedroom", "--backend", "native", "--dry-run"}, "only supports backend=airplay")
	assertUsage([]string{"volume", "101", "--dry-run"}, "volume must be 0-100")
	assertUsage([]string{"play", "--dry-run", "--json"}, "playlist is required")
	assertUsage([]string{"run", "missing-alias", "--dry-run", "--json"}, "unknown alias")
	assertUsage([]string{"native-run", "--dry-run", "--json"}, "--shortcut is required")
}

func TestCLIExitBoundary_JSONAndUsagePaths(t *testing.T) {
	cli := newCLIHarness(t)

	result := cli.run(t)
	if result.ExitCode != exitUsage {
		t.Fatalf("empty args exit=%d want=%d stderr=%s", result.ExitCode, exitUsage, result.Stderr)
	}
	if !strings.Contains(strings.ToLower(result.Stderr), "usage:") {
		t.Fatalf("empty args output missing usage text: %s", result.Stderr)
	}

	result = cli.run(t, "unknown-command", "--json")
	if result.ExitCode != exitUsage {
		t.Fatalf("unknown command --json exit=%d want=%d stderr=%s", result.ExitCode, exitUsage, result.Stderr)
	}
	var usagePayload struct {
		OK    bool `json:"ok"`
		Error struct {
			Code     string `json:"code"`
			ExitCode int    `json:"exitCode"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(result.Stderr), &usagePayload); err != nil {
		t.Fatalf("parse usage json: %v: %s", err, result.Stderr)
	}
	if usagePayload.OK || usagePayload.Error.Code != "USAGE_ERROR" || usagePayload.Error.ExitCode != exitUsage {
		t.Fatalf("unexpected usage payload: %+v", usagePayload)
	}

	result = cli.run(t, "config", "validate", "--json")
	if result.ExitCode != 0 {
		t.Fatalf("initial config validate exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	var initialValidate struct {
		OK   bool   `json:"ok"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &initialValidate); err != nil {
		t.Fatalf("parse initial validate json: %v: %s", err, result.Stdout)
	}
	if !initialValidate.OK || strings.TrimSpace(initialValidate.Path) == "" {
		t.Fatalf("unexpected initial validate payload: %+v", initialValidate)
	}

	cfgPath := initialValidate.Path
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte(`{"defaults":{"backend":"broken"}}`), 0o600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	result = cli.run(t, "config", "validate")
	if result.ExitCode != exitUsage {
		t.Fatalf("config validate invalid exit=%d want=%d stdout=%s", result.ExitCode, exitUsage, result.Stdout)
	}
	if !strings.Contains(strings.ToLower(result.Stdout), "config invalid") || !strings.Contains(result.Stdout, "defaults.backend") {
		t.Fatalf("validate plain output missing expected diagnostics: %s", result.Stdout)
	}
}

func TestCLIAutomationCommands(t *testing.T) {
	cli := newCLIHarness(t)

	result := cli.run(t, "automation", "init", "--preset", "morning")
	if result.ExitCode != 0 {
		t.Fatalf("automation init exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if !strings.Contains(result.Stdout, `version: "1"`) || !strings.Contains(result.Stdout, "name: morning") {
		t.Fatalf("automation init output unexpected: %s", result.Stdout)
	}

	routinePath := filepath.Join(t.TempDir(), "morning.yaml")
	if err := os.WriteFile(routinePath, []byte(result.Stdout), 0o644); err != nil {
		t.Fatalf("write routine: %v", err)
	}

	result = cli.run(t, "automation", "validate", "-f", routinePath, "--json")
	if result.ExitCode != 0 {
		t.Fatalf("automation validate exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if !strings.Contains(result.Stdout, `"mode": "validate"`) || !strings.Contains(result.Stdout, `"ok": true`) {
		t.Fatalf("automation validate json unexpected: %s", result.Stdout)
	}

	result = cli.run(t, "automation", "plan", "-f", routinePath)
	if result.ExitCode != 0 {
		t.Fatalf("automation plan exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if !strings.Contains(result.Stdout, `mode=plan`) || !strings.Contains(result.Stdout, `1/4 out.set ok=true`) {
		t.Fatalf("automation plan output unexpected: %s", result.Stdout)
	}

	result = cli.run(t, "automation", "run", "-f", routinePath, "--dry-run", "--json")
	if result.ExitCode != 0 {
		t.Fatalf("automation run --dry-run exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if !strings.Contains(result.Stdout, `"mode": "dry-run"`) || !strings.Contains(result.Stdout, `"steps"`) {
		t.Fatalf("automation dry-run json unexpected: %s", result.Stdout)
	}

	result = cli.run(t, "automation", "run", "-f", routinePath, "--no-input")
	if result.ExitCode == exitUsage && strings.Contains(result.Stderr, "not implemented yet") {
		t.Fatalf("automation run should execute now, got old scaffold error: %s", result.Stderr)
	}
}

func TestCLIAutomationErrorPaths(t *testing.T) {
	cli := newCLIHarness(t)

	assertUsage := func(args []string, contains string) {
		t.Helper()
		result := cli.run(t, args...)
		if result.ExitCode != exitUsage {
			t.Fatalf("%v exit=%d want=%d stderr=%s", args, result.ExitCode, exitUsage, result.Stderr)
		}
		if !strings.Contains(strings.ToLower(result.Stderr), strings.ToLower(contains)) {
			t.Fatalf("%v output missing %q: %s", args, contains, result.Stderr)
		}
	}

	missingFile := filepath.Join(cli.home, "does-not-exist.yaml")
	result := cli.run(t, "automation", "validate", "-f", missingFile)
	if result.ExitCode != exitGeneric {
		t.Fatalf("missing file exit=%d want=%d stderr=%s", result.ExitCode, exitGeneric, result.Stderr)
	}
	if !strings.Contains(strings.ToLower(result.Stderr), "read automation file") {
		t.Fatalf("missing file output unexpected: %s", result.Stderr)
	}
	assertUsage([]string{"automation", "validate", "-f", ""}, "--file is required")
	assertValidation := func(args []string, contains string) {
		t.Helper()
		result := cli.run(t, args...)
		if result.ExitCode != exitConfig {
			t.Fatalf("%v exit=%d want=%d stderr=%s", args, result.ExitCode, exitConfig, result.Stderr)
		}
		if !strings.Contains(strings.ToLower(result.Stderr), strings.ToLower(contains)) {
			t.Fatalf("%v output missing %q: %s", args, contains, result.Stderr)
		}
	}

	assertValidation([]string{"automation", "run", "-f", "-", "--dry-run"}, "automation file is empty")

	badYAML := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(badYAML, []byte("version: [\n"), 0o644); err != nil {
		t.Fatalf("write bad yaml: %v", err)
	}
	assertValidation([]string{"automation", "validate", "-f", badYAML}, "invalid automation yaml")

	badSchema := filepath.Join(t.TempDir(), "bad-schema.yaml")
	if err := os.WriteFile(badSchema, []byte(`version: "2"
name: bad
steps:
  - type: wait
    state: playing
    timeout: 20s
`), 0o644); err != nil {
		t.Fatalf("write bad schema: %v", err)
	}
	assertValidation([]string{"automation", "validate", "-f", badSchema}, `version: expected "1"`)

	badStep := filepath.Join(t.TempDir(), "bad-step.yaml")
	if err := os.WriteFile(badStep, []byte(`version: "1"
name: bad-step
steps:
  - type: wait
    state: running
    timeout: 20s
`), 0o644); err != nil {
		t.Fatalf("write bad step: %v", err)
	}
	assertValidation([]string{"automation", "plan", "-f", badStep}, "expected playing|paused|stopped")
}

func TestCLIConfigCommands(t *testing.T) {
	cli := newCLIHarness(t)

	if result := cli.run(t, "config", "validate", "--json"); result.ExitCode != 0 || !strings.Contains(result.Stdout, `"ok": true`) {
		t.Fatalf("config validate exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if result := cli.run(t, "config", "set", "defaults.backend", "native"); result.ExitCode != 0 {
		t.Fatalf("config set backend exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if result := cli.run(t, "config", "get", "defaults.backend"); result.ExitCode != 0 || strings.TrimSpace(result.Stdout) != "native" {
		t.Fatalf("config get backend exit=%d stdout=%q", result.ExitCode, result.Stdout)
	}
	if result := cli.run(t, "config", "set", "defaults.rooms", "Bedroom", "Living Room"); result.ExitCode != 0 {
		t.Fatalf("config set rooms exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if result := cli.run(t, "config", "get", "defaults.rooms", "--json"); result.ExitCode != 0 || !strings.Contains(result.Stdout, "Living Room") {
		t.Fatalf("config get rooms exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if result := cli.run(t, "config", "set", "defaults.backend", "invalid"); result.ExitCode != exitUsage {
		t.Fatalf("invalid backend exit=%d want=%d stderr=%s", result.ExitCode, exitUsage, result.Stderr)
	}
	if result := cli.run(t, "config", "set", "aliases.night.backend", "native"); result.ExitCode != 0 {
		t.Fatalf("config set alias backend exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if result := cli.run(t, "config", "set", "aliases.night.rooms", "Bedroom"); result.ExitCode != 0 {
		t.Fatalf("config set alias rooms exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if result := cli.run(t, "config", "set", "native.playlists.Bedroom.Focus", "BR Focus Shortcut"); result.ExitCode != 0 {
		t.Fatalf("config set native playlist mapping exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if result := cli.run(t, "config", "set", "native.volumeShortcuts.Bedroom.30", "BR Volume 30"); result.ExitCode != 0 {
		t.Fatalf("config set native volume mapping exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if result := cli.run(t, "config", "get", "aliases.night.backend"); result.ExitCode != 0 || strings.TrimSpace(result.Stdout) != "native" {
		t.Fatalf("config get alias backend exit=%d stdout=%q", result.ExitCode, result.Stdout)
	}
	if result := cli.run(t, "config", "get", "native.playlists.Bedroom.Focus"); result.ExitCode != 0 || strings.TrimSpace(result.Stdout) != "BR Focus Shortcut" {
		t.Fatalf("config get native playlist mapping exit=%d stdout=%q", result.ExitCode, result.Stdout)
	}
	if result := cli.run(t, "config", "get", "native.volumeShortcuts.Bedroom.30"); result.ExitCode != 0 || strings.TrimSpace(result.Stdout) != "BR Volume 30" {
		t.Fatalf("config get native volume mapping exit=%d stdout=%q", result.ExitCode, result.Stdout)
	}

	result := cli.run(t, "config", "get", "does.not.exist", "--json")
	if result.ExitCode != exitUsage {
		t.Fatalf("json error exit=%d want=%d stderr=%s", result.ExitCode, exitUsage, result.Stderr)
	}
	var payload struct {
		OK    bool `json:"ok"`
		Error struct {
			Code     string `json:"code"`
			Message  string `json:"message"`
			ExitCode int    `json:"exitCode"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(result.Stderr), &payload); err != nil {
		t.Fatalf("json unmarshal error payload: %v; raw=%s", err, result.Stderr)
	}
	if payload.OK {
		t.Fatalf("payload.ok=true want=false")
	}
	if payload.Error.Code != "USAGE_ERROR" {
		t.Fatalf("payload.error.code=%q want=%q", payload.Error.Code, "USAGE_ERROR")
	}
	if payload.Error.ExitCode != exitUsage {
		t.Fatalf("payload.error.exitCode=%d want=%d", payload.Error.ExitCode, exitUsage)
	}
	if !strings.Contains(payload.Error.Message, "unsupported config path") {
		t.Fatalf("payload.error.message=%q", payload.Error.Message)
	}
}

func TestCLICompletionInstall(t *testing.T) {
	cli := newCLIHarness(t)

	targetDir := filepath.Join(cli.home, "custom-completions")
	result := cli.run(t, "completion", "install", "bash", "--path", targetDir)
	if result.ExitCode != 0 {
		t.Fatalf("completion install exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	targetFile := filepath.Join(targetDir, "homepodctl")
	b, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("read installed completion: %v", err)
	}
	if !strings.Contains(string(b), "homepodctl") {
		t.Fatalf("installed completion content unexpected: %s", string(b))
	}
}

func TestCLIPlanCommand(t *testing.T) {
	cli := newCLIHarness(t)

	result := cli.run(t, "plan", "native-run", "--shortcut", "Example", "--json")
	if result.ExitCode != 0 {
		t.Fatalf("plan native-run exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	var payload struct {
		OK      bool           `json:"ok"`
		Command string         `json:"command"`
		Plan    map[string]any `json:"plan"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &payload); err != nil {
		t.Fatalf("parse plan json: %v: %s", err, result.Stdout)
	}
	if !payload.OK || payload.Command != "native-run" {
		t.Fatalf("unexpected plan envelope: %+v", payload)
	}
	if payload.Plan["action"] != "native-run" {
		t.Fatalf("plan action=%v", payload.Plan["action"])
	}
	if payload.Plan["dryRun"] != true {
		t.Fatalf("plan dryRun=%v", payload.Plan["dryRun"])
	}

	missingShortcut := "__homepodctl_plan_safety_missing_7f5fd198__"
	result = cli.run(t, "plan", "native-run", "--shortcut", missingShortcut, "--dry-run=false", "--json")
	if result.ExitCode != 0 {
		t.Fatalf("plan canonical dry-run exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	var canonical struct {
		Args []string       `json:"args"`
		Plan map[string]any `json:"plan"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &canonical); err != nil {
		t.Fatalf("parse canonical plan json: %v: %s", err, result.Stdout)
	}
	wantArgs := []string{"--dry-run=true", "--json=true", "--shortcut", missingShortcut}
	if !slices.Equal(canonical.Args, wantArgs) {
		t.Fatalf("canonical args=%v want=%v", canonical.Args, wantArgs)
	}
	if canonical.Plan["dryRun"] != true {
		t.Fatalf("canonical plan dryRun=%v", canonical.Plan["dryRun"])
	}

	routinePath := filepath.Join(t.TempDir(), "plan-routine.yaml")
	routine := `version: "1"
name: plan-routine
steps:
  - type: out.set
    rooms: ["Bedroom"]
  - type: play
    query: "Focus"
  - type: wait
    state: playing
    timeout: 5s
`
	if err := os.WriteFile(routinePath, []byte(routine), 0o644); err != nil {
		t.Fatalf("write routine: %v", err)
	}

	result = cli.run(t, "plan", "automation", "run", "-f", routinePath, "--json")
	if result.ExitCode != 0 {
		t.Fatalf("plan automation run exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	var auto struct {
		OK      bool           `json:"ok"`
		Command string         `json:"command"`
		Plan    map[string]any `json:"plan"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &auto); err != nil {
		t.Fatalf("parse automation plan json: %v: %s", err, result.Stdout)
	}
	if !auto.OK || auto.Command != "automation" {
		t.Fatalf("unexpected automation plan envelope: %+v", auto)
	}
	if auto.Plan["mode"] != "dry-run" {
		t.Fatalf("automation plan mode=%v", auto.Plan["mode"])
	}
	if _, ok := auto.Plan["steps"]; !ok {
		t.Fatalf("automation plan missing steps: %+v", auto.Plan)
	}

	result = cli.run(t, "plan", "automation", "validate", "-f", routinePath)
	if result.ExitCode != exitUsage {
		t.Fatalf("plan automation validate exit=%d want=%d stderr=%s", result.ExitCode, exitUsage, result.Stderr)
	}
	if !strings.Contains(strings.ToLower(result.Stderr), "automation run") {
		t.Fatalf("unexpected automation non-run output: %s", result.Stderr)
	}

	result = cli.run(t, "plan", "pause")
	if result.ExitCode != exitUsage {
		t.Fatalf("plan unsupported exit=%d want=%d stderr=%s", result.ExitCode, exitUsage, result.Stderr)
	}
	if !strings.Contains(strings.ToLower(result.Stderr), "only supports") {
		t.Fatalf("unexpected unsupported output: %s", result.Stderr)
	}
}

func TestCLISchemaCommand(t *testing.T) {
	cli := newCLIHarness(t)

	result := cli.run(t, "schema", "--json")
	if result.ExitCode != 0 {
		t.Fatalf("schema list exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if !strings.Contains(result.Stdout, "action-result") || !strings.Contains(result.Stdout, "plan-response") {
		t.Fatalf("schema list missing expected names: %s", result.Stdout)
	}

	result = cli.run(t, "schema", "action-result", "--json")
	if result.ExitCode != 0 {
		t.Fatalf("schema action-result exit=%d stdout=%s", result.ExitCode, result.Stdout)
	}
	if !strings.Contains(result.Stdout, `"$schema"`) || !strings.Contains(result.Stdout, `"action"`) {
		t.Fatalf("schema action-result output unexpected: %s", result.Stdout)
	}

	result = cli.run(t, "schema", "does-not-exist")
	if result.ExitCode != exitUsage {
		t.Fatalf("unknown schema exit=%d want=%d stderr=%s", result.ExitCode, exitUsage, result.Stderr)
	}
}
