package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func TestParseArgs(t *testing.T) {
	t.Parallel()

	flags, pos, err := parseArgs([]string{
		"chill",
		"--backend", "airplay",
		"--room", "Living Room",
		"--room=Bedroom",
		"--shuffle", "false",
		"--choose=true",
		"--dry-run",
		"--playlist-id", "ABC123",
	})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if got := flags.string("backend"); got != "airplay" {
		t.Fatalf("backend=%q, want %q", got, "airplay")
	}
	if got := flags.strings("room"); len(got) != 2 || got[0] != "Living Room" || got[1] != "Bedroom" {
		t.Fatalf("room=%v, want %v", got, []string{"Living Room", "Bedroom"})
	}
	if got := flags.string("playlist-id"); got != "ABC123" {
		t.Fatalf("playlist-id=%q, want %q", got, "ABC123")
	}
	if got, ok, err := flags.boolStrict("shuffle"); err != nil || !ok || got != false {
		t.Fatalf("shuffle=%v ok=%v err=%v, want false true nil", got, ok, err)
	}
	if got, ok, err := flags.boolStrict("choose"); err != nil || !ok || got != true {
		t.Fatalf("choose=%v ok=%v err=%v, want true true nil", got, ok, err)
	}
	if got, ok, err := flags.boolStrict("dry-run"); err != nil || !ok || got != true {
		t.Fatalf("dry-run=%v ok=%v err=%v, want true true nil", got, ok, err)
	}
	if len(pos) != 1 || pos[0] != "chill" {
		t.Fatalf("pos=%v, want %v", pos, []string{"chill"})
	}
}

func TestParseArgs_UnknownFlag(t *testing.T) {
	t.Parallel()

	_, _, err := parseArgs([]string{"--nope"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := classifyExitCode(err); got != exitUsage {
		t.Fatalf("classifyExitCode=%d, want %d", got, exitUsage)
	}
}

func TestParseGlobalOptions(t *testing.T) {
	t.Parallel()

	opts, cmd, args, err := parseGlobalOptions([]string{"--verbose", "status", "--json"})
	if err != nil {
		t.Fatalf("parseGlobalOptions: %v", err)
	}
	if !opts.verbose {
		t.Fatalf("verbose=false, want true")
	}
	if cmd != "status" {
		t.Fatalf("cmd=%q, want %q", cmd, "status")
	}
	if len(args) != 1 || args[0] != "--json" {
		t.Fatalf("args=%v, want [--json]", args)
	}
}

func TestParseGlobalOptions_UnknownFlag(t *testing.T) {
	t.Parallel()

	_, _, _, err := parseGlobalOptions([]string{"--wat"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := classifyExitCode(err); got != exitUsage {
		t.Fatalf("classifyExitCode=%d, want %d", got, exitUsage)
	}
}

func TestClassifyExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "usage", err: usageErrf("bad flag"), want: exitUsage},
		{name: "config", err: &native.ConfigError{Op: "read", Err: errors.New("boom")}, want: exitConfig},
		{name: "automation validation", err: automationValidationErrf("bad automation"), want: exitConfig},
		{name: "script", err: &music.ScriptError{Err: errors.New("boom"), Output: "x"}, want: exitBackend},
		{name: "shortcut", err: &native.ShortcutError{Name: "x", Err: errors.New("boom")}, want: exitBackend},
		{name: "generic", err: exec.ErrNotFound, want: exitGeneric},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyExitCode(tc.err); got != tc.want {
				t.Fatalf("classifyExitCode=%d, want %d", got, tc.want)
			}
		})
	}
}

func TestBuildAliasRows(t *testing.T) {
	t.Parallel()

	v := 30
	cfg := &native.Config{
		Defaults: native.DefaultsConfig{
			Backend: "airplay",
			Rooms:   []string{"Bedroom"},
		},
		Aliases: map[string]native.Alias{
			"zeta": {PlaylistID: "ABC123"},
			"alpha": {
				Shortcut: "Wake HomePod",
				Rooms:    []string{"Living Room"},
				Backend:  "native",
				Volume:   &v,
			},
		},
	}

	rows := buildAliasRows(cfg)
	if len(rows) != 2 {
		t.Fatalf("len(rows)=%d, want 2", len(rows))
	}
	if rows[0].Name != "alpha" || rows[1].Name != "zeta" {
		t.Fatalf("row order=%v, want [alpha zeta]", []string{rows[0].Name, rows[1].Name})
	}
	if rows[0].Target != "shortcut:Wake HomePod" {
		t.Fatalf("alpha target=%q, want shortcut target", rows[0].Target)
	}
	if rows[1].Backend != "airplay" {
		t.Fatalf("zeta backend=%q, want default backend", rows[1].Backend)
	}
	if len(rows[1].Rooms) != 1 || rows[1].Rooms[0] != "Bedroom" {
		t.Fatalf("zeta rooms=%v, want default rooms", rows[1].Rooms)
	}
}

func TestBuildAliasRows_Empty(t *testing.T) {
	t.Parallel()

	cfg := &native.Config{
		Defaults: native.DefaultsConfig{Backend: "airplay"},
		Aliases:  map[string]native.Alias{},
	}
	rows := buildAliasRows(cfg)
	if len(rows) != 0 {
		t.Fatalf("len(rows)=%d, want 0", len(rows))
	}
}

func TestParsedArgs_IntStrict(t *testing.T) {
	t.Parallel()

	flags, _, err := parseArgs([]string{"--volume", "50"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	v, ok, err := flags.intStrict("volume")
	if err != nil || !ok || v != 50 {
		t.Fatalf("volume=%v ok=%v err=%v, want 50 true nil", v, ok, err)
	}
}

func TestParseOutputFlags(t *testing.T) {
	t.Parallel()

	flags, _, err := parseArgs([]string{"--json", "--plain=false"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	jsonOut, plainOut, err := parseOutputFlags(flags)
	if err != nil {
		t.Fatalf("parseOutputFlags: %v", err)
	}
	if !jsonOut {
		t.Fatalf("jsonOut=false, want true")
	}
	if plainOut {
		t.Fatalf("plainOut=true, want false")
	}
}

func TestFriendlyScriptError(t *testing.T) {
	t.Parallel()

	if got := friendlyScriptError("Not authorised to send Apple events"); !strings.Contains(strings.ToLower(got), "automation") {
		t.Fatalf("friendlyScriptError auth=%q", got)
	}
	if got := friendlyScriptError("Connection Invalid error for service"); !strings.Contains(strings.ToLower(got), "music") {
		t.Fatalf("friendlyScriptError connection=%q", got)
	}
	if got := friendlyScriptError("can't get AirPlay device \"Bedroom\""); !strings.Contains(strings.ToLower(got), "airplay") {
		t.Fatalf("friendlyScriptError airplay=%q", got)
	}
	if got := friendlyScriptError("unmapped backend noise"); got != "" {
		t.Fatalf("friendlyScriptError default=%q, want empty", got)
	}
}

func TestCompletionScript(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	for _, shell := range []string{"bash", "zsh", "fish"} {
		s, err := completionScript(shell)
		if err != nil {
			t.Fatalf("completionScript(%q): %v", shell, err)
		}
		if !strings.Contains(s, "homepodctl") {
			t.Fatalf("completionScript(%q) missing command name", shell)
		}
		if !strings.Contains(s, "dry-run") {
			t.Fatalf("completionScript(%q) missing dry-run flag", shell)
		}
		if !strings.Contains(s, "automation") {
			t.Fatalf("completionScript(%q) missing automation command", shell)
		}
	}
	if _, err := completionScript("pwsh"); err == nil {
		t.Fatalf("expected error for unknown shell")
	}
}

func TestCompletionData(t *testing.T) {
	t.Parallel()

	cfg := &native.Config{
		Defaults: native.DefaultsConfig{
			Rooms: []string{"Bedroom"},
		},
		Aliases: map[string]native.Alias{
			"bed": {Rooms: []string{"Bedroom"}},
			"lr":  {Rooms: []string{"Living Room"}},
		},
		Native: native.NativeConfig{
			Playlists: map[string]map[string]string{
				"Kitchen": {"X": "Y"},
			},
		},
	}
	aliases, rooms := completionData(cfg)
	if len(aliases) != 2 || aliases[0] != "bed" || aliases[1] != "lr" {
		t.Fatalf("aliases=%v", aliases)
	}
	if len(rooms) != 3 {
		t.Fatalf("rooms=%v", rooms)
	}
}

func TestCompletionInstallPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := completionInstallPath("bash", "")
	if err != nil {
		t.Fatalf("completionInstallPath(bash): %v", err)
	}
	want := filepath.Join(home, ".local", "share", "bash-completion", "completions", "homepodctl")
	if got != want {
		t.Fatalf("bash path=%q want=%q", got, want)
	}

	customDir := filepath.Join(home, "custom")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom dir: %v", err)
	}
	got, err = completionInstallPath("zsh", customDir)
	if err != nil {
		t.Fatalf("completionInstallPath(zsh custom dir): %v", err)
	}
	want = filepath.Join(customDir, "_homepodctl")
	if got != want {
		t.Fatalf("zsh custom path=%q want=%q", got, want)
	}

	got, err = completionInstallPath("fish", "~/dotfiles/homepodctl.fish")
	if err != nil {
		t.Fatalf("completionInstallPath(fish tilde): %v", err)
	}
	want = filepath.Join(home, "dotfiles", "homepodctl.fish")
	if got != want {
		t.Fatalf("fish tilde path=%q want=%q", got, want)
	}
}

func TestInstallCompletionWritesFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	targetDir := filepath.Join(home, "completions")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	path, err := installCompletion("fish", targetDir)
	if err != nil {
		t.Fatalf("installCompletion: %v", err)
	}
	if path != filepath.Join(targetDir, "homepodctl.fish") {
		t.Fatalf("path=%q", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read completion file: %v", err)
	}
	if !strings.Contains(string(b), "complete -c homepodctl") {
		t.Fatalf("completion file content unexpected: %s", string(b))
	}
}

func TestWriteActionOutput_DryRunJSON(t *testing.T) {
	out := captureStdout(t, func() {
		writeActionOutput("play", true, false, actionOutput{
			DryRun:   true,
			Backend:  "airplay",
			Rooms:    []string{"Bedroom"},
			Playlist: "chill",
		})
	})
	if !strings.Contains(out, `"dryRun": true`) {
		t.Fatalf("dry-run json missing: %q", out)
	}
	if !strings.Contains(out, `"action": "play"`) {
		t.Fatalf("action json missing: %q", out)
	}
}

func TestWriteActionOutput_DryRunText(t *testing.T) {
	out := captureStdout(t, func() {
		writeActionOutput("volume", false, false, actionOutput{
			DryRun:  true,
			Backend: "airplay",
			Rooms:   []string{"Bedroom"},
		})
	})
	if !strings.Contains(out, "dry-run action=volume") {
		t.Fatalf("dry-run text missing: %q", out)
	}
}

func TestCLIDryRunCommands(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	bin := filepath.Join(t.TempDir(), "homepodctl")
	build := exec.Command("go", "build", "-o", bin, "./cmd/homepodctl")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build cli: %v: %s", err, string(out))
	}

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
}

func TestCLIDryRunErrorPaths(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	bin := filepath.Join(t.TempDir(), "homepodctl")
	build := exec.Command("go", "build", "-o", bin, "./cmd/homepodctl")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build cli: %v: %s", err, string(out))
	}

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
func TestCLIAutomationCommands(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	bin := filepath.Join(t.TempDir(), "homepodctl")
	build := exec.Command("go", "build", "-o", bin, "./cmd/homepodctl")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build cli: %v: %s", err, string(out))
	}

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
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	bin := filepath.Join(t.TempDir(), "homepodctl")
	build := exec.Command("go", "build", "-o", bin, "./cmd/homepodctl")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build cli: %v: %s", err, string(out))
	}

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
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	bin := filepath.Join(t.TempDir(), "homepodctl")
	build := exec.Command("go", "build", "-o", bin, "./cmd/homepodctl")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build cli: %v: %s", err, string(out))
	}

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
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	bin := filepath.Join(t.TempDir(), "homepodctl")
	build := exec.Command("go", "build", "-o", bin, "./cmd/homepodctl")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build cli: %v: %s", err, string(out))
	}

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
func TestCmdHelp_PlayExamplesUseQuotes(t *testing.T) {
	out := captureStdout(t, func() {
		cmdHelp([]string{"play"})
	})
	if !strings.Contains(out, `homepodctl play "Songs I've been obsessed recently pt. 2"`) {
		t.Fatalf("help output missing quoted example: %q", out)
	}
	if strings.Contains(out, `\"`) {
		t.Fatalf("help output should not contain escaped quotes: %q", out)
	}
}

func TestCmdHelp_RunMentionsDryRun(t *testing.T) {
	out := captureStdout(t, func() {
		cmdHelp([]string{"run"})
	})
	if !strings.Contains(out, "--dry-run") {
		t.Fatalf("run help missing --dry-run: %q", out)
	}
}

func TestCmdHelp_Doctor(t *testing.T) {
	out := captureStdout(t, func() {
		cmdHelp([]string{"doctor"})
	})
	if !strings.Contains(out, "homepodctl doctor") {
		t.Fatalf("doctor help missing usage: %q", out)
	}
}

func TestInferSelectedOutputs(t *testing.T) {
	t.Run("dedupes and trims output names", func(t *testing.T) {
		orig := getNowPlaying
		t.Cleanup(func() { getNowPlaying = orig })
		getNowPlaying = func(context.Context) (music.NowPlaying, error) {
			return music.NowPlaying{Outputs: []music.AirPlayDevice{
				{Name: " Bedroom "},
				{Name: ""},
				{Name: "Bedroom"},
				{Name: "Living Room"},
			}}, nil
		}

		got := inferSelectedOutputs(context.Background())
		if len(got) != 2 || got[0] != "Bedroom" || got[1] != "Living Room" {
			t.Fatalf("inferSelectedOutputs=%v, want [Bedroom Living Room]", got)
		}
	})

	t.Run("returns nil on now-playing error", func(t *testing.T) {
		orig := getNowPlaying
		t.Cleanup(func() { getNowPlaying = orig })
		getNowPlaying = func(context.Context) (music.NowPlaying, error) {
			return music.NowPlaying{}, errors.New("boom")
		}

		if got := inferSelectedOutputs(context.Background()); got != nil {
			t.Fatalf("inferSelectedOutputs=%v, want nil", got)
		}
	})
}

func TestValidateAirplayVolumeSelection(t *testing.T) {
	tests := []struct {
		name           string
		volumeExplicit bool
		volume         int
		rooms          []string
		wantErr        bool
	}{
		{name: "explicit volume with no rooms errors", volumeExplicit: true, volume: 30, rooms: nil, wantErr: true},
		{name: "explicit volume with rooms passes", volumeExplicit: true, volume: 30, rooms: []string{"Bedroom"}, wantErr: false},
		{name: "implicit default volume with no rooms passes", volumeExplicit: false, volume: 30, rooms: nil, wantErr: false},
		{name: "negative volume bypasses check", volumeExplicit: true, volume: -1, rooms: nil, wantErr: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAirplayVolumeSelection(tc.volumeExplicit, tc.volume, tc.rooms)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateAirplayVolumeSelection() err=%v, wantErr=%t", err, tc.wantErr)
			}
		})
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close write pipe: %v", err)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured output: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close read pipe: %v", err)
	}
	return string(b)
}

func TestAutomationParseAndValidateYAML(t *testing.T) {
	t.Parallel()
	doc, err := parseAutomationBytes([]byte(`version: "1"
name: morning
steps:
  - type: out.set
    rooms: ["Bedroom"]
  - type: play
    query: "Morning Mix"
  - type: volume.set
    value: 30
  - type: wait
    state: playing
    timeout: 20s
`))
	if err != nil {
		t.Fatalf("parseAutomationBytes: %v", err)
	}
	if err := validateAutomation(doc); err != nil {
		t.Fatalf("validateAutomation: %v", err)
	}
}

func TestAutomationValidateRejectsInvalidPlayStep(t *testing.T) {
	t.Parallel()
	doc := &automationFile{
		Version: "1",
		Name:    "bad",
		Steps: []automationStep{{
			Type:       "play",
			Query:      "x",
			PlaylistID: "ABC",
		}},
	}
	err := validateAutomation(doc)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "exactly one of query or playlistId") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAutomationPreset(t *testing.T) {
	t.Parallel()
	doc, err := automationPreset("focus")
	if err != nil {
		t.Fatalf("automationPreset: %v", err)
	}
	if doc.Name != "focus" {
		t.Fatalf("name=%q, want focus", doc.Name)
	}
	if len(doc.Steps) == 0 {
		t.Fatalf("expected steps")
	}
	if _, err := automationPreset("unknown"); err == nil {
		t.Fatalf("expected error for unknown preset")
	}
}

func TestBuildAutomationResultJSONShape(t *testing.T) {
	t.Parallel()
	doc := &automationFile{
		Version: "1",
		Name:    "morning",
		Steps:   []automationStep{{Type: "out.set", Rooms: []string{"Bedroom"}}},
	}
	steps := resolveAutomationSteps(nil, doc)
	res := buildAutomationResult("dry-run", doc, steps)
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(b), `"mode":"dry-run"`) {
		t.Fatalf("missing mode in json: %s", string(b))
	}
	if !strings.Contains(string(b), `"steps"`) {
		t.Fatalf("missing steps in json: %s", string(b))
	}
}

func TestExecuteAutomationSteps_StopsOnFailure(t *testing.T) {
	origSetCurrentOutputs := setCurrentOutputs
	origSetDeviceVolume := setDeviceVolume
	origSetShuffle := setShuffle
	origSearchPlaylists := searchPlaylists
	origPlayPlaylistByID := playPlaylistByID
	t.Cleanup(func() {
		setCurrentOutputs = origSetCurrentOutputs
		setDeviceVolume = origSetDeviceVolume
		setShuffle = origSetShuffle
		searchPlaylists = origSearchPlaylists
		playPlaylistByID = origPlayPlaylistByID
	})

	setCurrentOutputs = func(context.Context, []string) error { return errors.New("boom") }
	setDeviceVolume = func(context.Context, string, int) error { return nil }
	setShuffle = func(context.Context, bool) error { return nil }
	searchPlaylists = func(context.Context, string) ([]music.UserPlaylist, error) {
		return []music.UserPlaylist{{PersistentID: "P1", Name: "X"}}, nil
	}
	playPlaylistByID = func(context.Context, string) error { return nil }

	doc := &automationFile{
		Version: "1",
		Name:    "test",
		Defaults: automationDefaults{
			Backend: "airplay",
			Rooms:   []string{"Bedroom"},
		},
		Steps: []automationStep{
			{Type: "out.set", Rooms: []string{"Bedroom"}},
			{Type: "play", Query: "Chill"},
		},
	}
	results, ok := executeAutomationSteps(context.Background(), &native.Config{}, doc)
	if ok {
		t.Fatalf("ok=true, want false")
	}
	if len(results) != 2 {
		t.Fatalf("len(results)=%d, want 2", len(results))
	}
	if results[0].OK {
		t.Fatalf("first step should fail")
	}
	if !results[1].Skipped {
		t.Fatalf("second step should be skipped")
	}
}

func TestExecuteAutomationPlayNative(t *testing.T) {
	origRunShortcut := runNativeShortcut
	t.Cleanup(func() { runNativeShortcut = origRunShortcut })

	called := 0
	runNativeShortcut = func(context.Context, string) error {
		called++
		return nil
	}
	cfg := &native.Config{
		Native: native.NativeConfig{
			Playlists: map[string]map[string]string{
				"Bedroom": {"Focus": "BR Focus"},
			},
		},
	}
	err := executeAutomationPlay(context.Background(), cfg, "native", automationDefaults{Backend: "native", Rooms: []string{"Bedroom"}}, automationStep{
		Type:  "play",
		Query: "Focus",
	})
	if err != nil {
		t.Fatalf("executeAutomationPlay: %v", err)
	}
	if called != 1 {
		t.Fatalf("runNativeShortcut calls=%d, want 1", called)
	}
}

func TestSetVolumeForRooms(t *testing.T) {
	orig := setDeviceVolume
	t.Cleanup(func() { setDeviceVolume = orig })

	var got []string
	setDeviceVolume = func(_ context.Context, room string, value int) error {
		got = append(got, room+":"+strconv.Itoa(value))
		if room == "Kitchen" {
			return errors.New("boom")
		}
		return nil
	}

	err := setVolumeForRooms(context.Background(), []string{"Bedroom", "Kitchen"}, 35)
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(got) != 2 {
		t.Fatalf("calls=%v, want 2 calls", got)
	}
}

func TestResolveNativeShortcuts(t *testing.T) {
	cfg := &native.Config{
		Native: native.NativeConfig{
			Playlists:       map[string]map[string]string{"Bedroom": {"Focus": "Focus Shortcut"}},
			VolumeShortcuts: map[string]map[string]string{"Bedroom": {"30": "Volume 30 Shortcut"}},
		},
	}

	playlistShortcut, err := resolveNativePlaylistShortcut(cfg, "Bedroom", "Focus")
	if err != nil {
		t.Fatalf("resolveNativePlaylistShortcut: %v", err)
	}
	if playlistShortcut != "Focus Shortcut" {
		t.Fatalf("playlist shortcut=%q", playlistShortcut)
	}

	volumeShortcut, err := resolveNativeVolumeShortcut(cfg, "Bedroom", 30)
	if err != nil {
		t.Fatalf("resolveNativeVolumeShortcut: %v", err)
	}
	if volumeShortcut != "Volume 30 Shortcut" {
		t.Fatalf("volume shortcut=%q", volumeShortcut)
	}

	if _, err := resolveNativePlaylistShortcut(cfg, "Bedroom", "Missing"); err == nil {
		t.Fatalf("expected missing playlist mapping error")
	}
	if _, err := resolveNativeVolumeShortcut(cfg, "Bedroom", 99); err == nil {
		t.Fatalf("expected missing volume mapping error")
	}
}

func TestRunNativeShortcutsUsesResolvedMappings(t *testing.T) {
	orig := runNativeShortcut
	t.Cleanup(func() { runNativeShortcut = orig })

	cfg := &native.Config{
		Native: native.NativeConfig{
			Playlists:       map[string]map[string]string{"Bedroom": {"Focus": "Focus Shortcut"}},
			VolumeShortcuts: map[string]map[string]string{"Bedroom": {"30": "Volume 30 Shortcut"}},
		},
	}

	var calls []string
	runNativeShortcut = func(_ context.Context, name string) error {
		calls = append(calls, name)
		return nil
	}

	if err := runNativePlaylistShortcuts(context.Background(), cfg, []string{"Bedroom"}, "Focus"); err != nil {
		t.Fatalf("runNativePlaylistShortcuts: %v", err)
	}
	if err := runNativeVolumeShortcuts(context.Background(), cfg, []string{"Bedroom"}, 30); err != nil {
		t.Fatalf("runNativeVolumeShortcuts: %v", err)
	}
	if len(calls) != 2 || calls[0] != "Focus Shortcut" || calls[1] != "Volume 30 Shortcut" {
		t.Fatalf("shortcut calls=%v", calls)
	}
}

func TestRunDoctorChecksUsesInjectedSeams(t *testing.T) {
	origLookPath := lookPath
	origConfigPath := configPath
	origLoadConfigOptional := loadConfigOptional
	origGetNowPlaying := getNowPlaying
	t.Cleanup(func() {
		lookPath = origLookPath
		configPath = origConfigPath
		loadConfigOptional = origLoadConfigOptional
		getNowPlaying = origGetNowPlaying
	})

	lookPath = func(name string) (string, error) {
		switch name {
		case "osascript":
			return "", errors.New("missing")
		case "shortcuts":
			return "/usr/bin/shortcuts", nil
		default:
			return "", errors.New("unexpected")
		}
	}
	configPath = func() (string, error) { return "/tmp/homepodctl/config.json", nil }
	loadConfigOptional = func() (*native.Config, error) {
		return &native.Config{Aliases: map[string]native.Alias{"bed": {Playlist: "Focus"}}}, nil
	}
	getNowPlaying = func(context.Context) (music.NowPlaying, error) {
		return music.NowPlaying{}, errors.New("music unavailable")
	}

	report := runDoctorChecks(context.Background())
	if report.OK {
		t.Fatalf("report.OK=true, want false due to missing osascript")
	}

	statusByName := map[string]string{}
	for _, check := range report.Checks {
		statusByName[check.Name] = check.Status
	}
	if statusByName["osascript"] != "fail" {
		t.Fatalf("osascript status=%q", statusByName["osascript"])
	}
	if statusByName["shortcuts"] != "pass" {
		t.Fatalf("shortcuts status=%q", statusByName["shortcuts"])
	}
	if statusByName["config"] != "pass" {
		t.Fatalf("config status=%q", statusByName["config"])
	}
	if statusByName["music-backend"] != "warn" {
		t.Fatalf("music-backend status=%q", statusByName["music-backend"])
	}
}
