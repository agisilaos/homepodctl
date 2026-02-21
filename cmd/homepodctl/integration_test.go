package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIDryRunCommands(t *testing.T) {
	bin := buildCLIBinary(t)

	home := t.TempDir()
	run := func(args ...string) (int, string) {
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

	if code, out := run("config-init"); code != 0 {
		t.Fatalf("config-init exit=%d out=%s", code, out)
	}
	if code, out := run("config", "set", "defaults.backend", "native"); code != 0 {
		t.Fatalf("config set defaults.backend native exit=%d out=%s", code, out)
	}

	assertDryRun := func(args ...string) {
		t.Helper()
		code, out := run(args...)
		if code != 0 {
			t.Fatalf("%v exit=%d out=%s", args, code, out)
		}
		if !strings.Contains(out, `"dryRun": true`) {
			t.Fatalf("%v output missing dryRun=true: %s", args, out)
		}
	}

	assertDryRun("out", "set", "Bedroom", "--dry-run", "--json")
	assertDryRun("volume", "30", "--dry-run", "--json")
	assertDryRun("play", "chill", "--dry-run", "--json")
	assertDryRun("run", "bed", "--dry-run", "--json")
	assertDryRun("native-run", "--shortcut", "Example", "--dry-run", "--json")

	code, out := run("out", "set", "Bedroom", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("out set dry-run with defaults.backend=native exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, `"backend": "airplay"`) {
		t.Fatalf("out set backend should be airplay, output=%s", out)
	}
}

func TestCLIDryRunErrorPaths(t *testing.T) {
	bin := buildCLIBinary(t)

	home := t.TempDir()
	run := func(args ...string) (int, string) {
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

	if code, out := run("config-init"); code != 0 {
		t.Fatalf("config-init exit=%d out=%s", code, out)
	}

	assertUsage := func(args []string, contains string) {
		t.Helper()
		code, out := run(args...)
		if code != exitUsage {
			t.Fatalf("%v exit=%d want=%d out=%s", args, code, exitUsage, out)
		}
		if !strings.Contains(strings.ToLower(out), strings.ToLower(contains)) {
			t.Fatalf("%v output missing %q: %s", args, contains, out)
		}
	}

	assertUsage([]string{"out", "set", "Bedroom", "--backend", "native", "--dry-run"}, "only supports backend=airplay")
	assertUsage([]string{"volume", "101", "--dry-run"}, "volume must be 0-100")
	assertUsage([]string{"play", "--dry-run", "--json"}, "playlist is required")
	assertUsage([]string{"run", "missing-alias", "--dry-run", "--json"}, "unknown alias")
	assertUsage([]string{"native-run", "--dry-run", "--json"}, "--shortcut is required")
}

func TestCLIExitBoundary_JSONAndUsagePaths(t *testing.T) {
	bin := buildCLIBinary(t)

	home := t.TempDir()
	run := func(args ...string) (int, string) {
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

	code, out := run()
	if code != exitUsage {
		t.Fatalf("empty args exit=%d want=%d out=%s", code, exitUsage, out)
	}
	if !strings.Contains(strings.ToLower(out), "usage:") {
		t.Fatalf("empty args output missing usage text: %s", out)
	}

	code, out = run("unknown-command", "--json")
	if code != exitUsage {
		t.Fatalf("unknown command --json exit=%d want=%d out=%s", code, exitUsage, out)
	}
	var usagePayload struct {
		OK    bool `json:"ok"`
		Error struct {
			Code     string `json:"code"`
			ExitCode int    `json:"exitCode"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &usagePayload); err != nil {
		t.Fatalf("parse usage json: %v: %s", err, out)
	}
	if usagePayload.OK || usagePayload.Error.Code != "USAGE_ERROR" || usagePayload.Error.ExitCode != exitUsage {
		t.Fatalf("unexpected usage payload: %+v", usagePayload)
	}

	code, out = run("config", "validate", "--json")
	if code != 0 {
		t.Fatalf("initial config validate exit=%d out=%s", code, out)
	}
	var initialValidate struct {
		OK   bool   `json:"ok"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(out), &initialValidate); err != nil {
		t.Fatalf("parse initial validate json: %v: %s", err, out)
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

	code, out = run("config", "validate")
	if code != exitUsage {
		t.Fatalf("config validate invalid exit=%d want=%d out=%s", code, exitUsage, out)
	}
	if !strings.Contains(strings.ToLower(out), "config invalid") || !strings.Contains(out, "defaults.backend") {
		t.Fatalf("validate plain output missing expected diagnostics: %s", out)
	}
}
func TestCLIAutomationCommands(t *testing.T) {
	bin := buildCLIBinary(t)

	home := t.TempDir()
	run := func(args ...string) (int, string) {
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

	code, out := run("automation", "init", "--preset", "morning")
	if code != 0 {
		t.Fatalf("automation init exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, `version: "1"`) || !strings.Contains(out, "name: morning") {
		t.Fatalf("automation init output unexpected: %s", out)
	}

	routinePath := filepath.Join(t.TempDir(), "morning.yaml")
	if err := os.WriteFile(routinePath, []byte(out), 0o644); err != nil {
		t.Fatalf("write routine: %v", err)
	}

	code, out = run("automation", "validate", "-f", routinePath, "--json")
	if code != 0 {
		t.Fatalf("automation validate exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, `"mode": "validate"`) || !strings.Contains(out, `"ok": true`) {
		t.Fatalf("automation validate json unexpected: %s", out)
	}

	code, out = run("automation", "plan", "-f", routinePath)
	if code != 0 {
		t.Fatalf("automation plan exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, `mode=plan`) || !strings.Contains(out, `1/4 out.set ok=true`) {
		t.Fatalf("automation plan output unexpected: %s", out)
	}

	code, out = run("automation", "run", "-f", routinePath, "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("automation run --dry-run exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, `"mode": "dry-run"`) || !strings.Contains(out, `"steps"`) {
		t.Fatalf("automation dry-run json unexpected: %s", out)
	}

	code, out = run("automation", "run", "-f", routinePath, "--no-input")
	if code == exitUsage && strings.Contains(out, "not implemented yet") {
		t.Fatalf("automation run should execute now, got old scaffold error: %s", out)
	}
}

func TestCLIAutomationErrorPaths(t *testing.T) {
	bin := buildCLIBinary(t)

	home := t.TempDir()
	run := func(args ...string) (int, string) {
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

	assertUsage := func(args []string, contains string) {
		t.Helper()
		code, out := run(args...)
		if code != exitUsage {
			t.Fatalf("%v exit=%d want=%d out=%s", args, code, exitUsage, out)
		}
		if !strings.Contains(strings.ToLower(out), strings.ToLower(contains)) {
			t.Fatalf("%v output missing %q: %s", args, contains, out)
		}
	}

	code, out := run("automation", "validate", "-f", "/tmp/does-not-exist.yaml")
	if code != exitGeneric {
		t.Fatalf("missing file exit=%d want=%d out=%s", code, exitGeneric, out)
	}
	if !strings.Contains(strings.ToLower(out), "read automation file") {
		t.Fatalf("missing file output unexpected: %s", out)
	}
	assertUsage([]string{"automation", "validate", "-f", ""}, "--file is required")
	assertValidation := func(args []string, contains string) {
		t.Helper()
		code, out := run(args...)
		if code != exitConfig {
			t.Fatalf("%v exit=%d want=%d out=%s", args, code, exitConfig, out)
		}
		if !strings.Contains(strings.ToLower(out), strings.ToLower(contains)) {
			t.Fatalf("%v output missing %q: %s", args, contains, out)
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
	bin := buildCLIBinary(t)

	home := t.TempDir()
	run := func(args ...string) (int, string) {
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

	if code, out := run("config", "validate", "--json"); code != 0 || !strings.Contains(out, `"ok": true`) {
		t.Fatalf("config validate exit=%d out=%s", code, out)
	}
	if code, out := run("config", "set", "defaults.backend", "native"); code != 0 {
		t.Fatalf("config set backend exit=%d out=%s", code, out)
	}
	if code, out := run("config", "get", "defaults.backend"); code != 0 || strings.TrimSpace(out) != "native" {
		t.Fatalf("config get backend exit=%d out=%q", code, out)
	}
	if code, out := run("config", "set", "defaults.rooms", "Bedroom", "Living Room"); code != 0 {
		t.Fatalf("config set rooms exit=%d out=%s", code, out)
	}
	if code, out := run("config", "get", "defaults.rooms", "--json"); code != 0 || !strings.Contains(out, "Living Room") {
		t.Fatalf("config get rooms exit=%d out=%s", code, out)
	}
	if code, out := run("config", "set", "defaults.backend", "invalid"); code != exitUsage {
		t.Fatalf("invalid backend exit=%d want=%d out=%s", code, exitUsage, out)
	}
	if code, out := run("config", "set", "aliases.night.backend", "native"); code != 0 {
		t.Fatalf("config set alias backend exit=%d out=%s", code, out)
	}
	if code, out := run("config", "set", "aliases.night.rooms", "Bedroom"); code != 0 {
		t.Fatalf("config set alias rooms exit=%d out=%s", code, out)
	}
	if code, out := run("config", "set", "native.playlists.Bedroom.Focus", "BR Focus Shortcut"); code != 0 {
		t.Fatalf("config set native playlist mapping exit=%d out=%s", code, out)
	}
	if code, out := run("config", "set", "native.volumeShortcuts.Bedroom.30", "BR Volume 30"); code != 0 {
		t.Fatalf("config set native volume mapping exit=%d out=%s", code, out)
	}
	if code, out := run("config", "get", "aliases.night.backend"); code != 0 || strings.TrimSpace(out) != "native" {
		t.Fatalf("config get alias backend exit=%d out=%q", code, out)
	}
	if code, out := run("config", "get", "native.playlists.Bedroom.Focus"); code != 0 || strings.TrimSpace(out) != "BR Focus Shortcut" {
		t.Fatalf("config get native playlist mapping exit=%d out=%q", code, out)
	}
	if code, out := run("config", "get", "native.volumeShortcuts.Bedroom.30"); code != 0 || strings.TrimSpace(out) != "BR Volume 30" {
		t.Fatalf("config get native volume mapping exit=%d out=%q", code, out)
	}

	code, out := run("config", "get", "does.not.exist", "--json")
	if code != exitUsage {
		t.Fatalf("json error exit=%d want=%d out=%s", code, exitUsage, out)
	}
	var payload struct {
		OK    bool `json:"ok"`
		Error struct {
			Code     string `json:"code"`
			Message  string `json:"message"`
			ExitCode int    `json:"exitCode"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("json unmarshal error payload: %v; raw=%s", err, out)
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
	bin := buildCLIBinary(t)

	home := t.TempDir()
	run := func(args ...string) (int, string) {
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

	targetDir := filepath.Join(home, "custom-completions")
	code, out := run("completion", "install", "bash", "--path", targetDir)
	if code != 0 {
		t.Fatalf("completion install exit=%d out=%s", code, out)
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
	bin := buildCLIBinary(t)

	run := func(args ...string) (int, string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
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

	code, out := run("plan", "native-run", "--shortcut", "Example", "--json")
	if code != 0 {
		t.Fatalf("plan native-run exit=%d out=%s", code, out)
	}
	var payload struct {
		OK      bool           `json:"ok"`
		Command string         `json:"command"`
		Plan    map[string]any `json:"plan"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse plan json: %v: %s", err, out)
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

	code, out = run("plan", "automation", "run", "-f", routinePath, "--json")
	if code != 0 {
		t.Fatalf("plan automation run exit=%d out=%s", code, out)
	}
	var auto struct {
		OK      bool           `json:"ok"`
		Command string         `json:"command"`
		Plan    map[string]any `json:"plan"`
	}
	if err := json.Unmarshal([]byte(out), &auto); err != nil {
		t.Fatalf("parse automation plan json: %v: %s", err, out)
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

	code, out = run("plan", "automation", "validate", "-f", routinePath)
	if code != exitUsage {
		t.Fatalf("plan automation validate exit=%d want=%d out=%s", code, exitUsage, out)
	}
	if !strings.Contains(strings.ToLower(out), "automation run") {
		t.Fatalf("unexpected automation non-run output: %s", out)
	}

	code, out = run("plan", "pause")
	if code != exitUsage {
		t.Fatalf("plan unsupported exit=%d want=%d out=%s", code, exitUsage, out)
	}
	if !strings.Contains(strings.ToLower(out), "only supports") {
		t.Fatalf("unexpected unsupported output: %s", out)
	}
}

func TestCLISchemaCommand(t *testing.T) {
	bin := buildCLIBinary(t)

	run := func(args ...string) (int, string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
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

	code, out := run("schema", "--json")
	if code != 0 {
		t.Fatalf("schema list exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "action-result") || !strings.Contains(out, "plan-response") {
		t.Fatalf("schema list missing expected names: %s", out)
	}

	code, out = run("schema", "action-result", "--json")
	if code != 0 {
		t.Fatalf("schema action-result exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, `"$schema"`) || !strings.Contains(out, `"action"`) {
		t.Fatalf("schema action-result output unexpected: %s", out)
	}

	code, out = run("schema", "does-not-exist")
	if code != exitUsage {
		t.Fatalf("unknown schema exit=%d want=%d out=%s", code, exitUsage, out)
	}
}
