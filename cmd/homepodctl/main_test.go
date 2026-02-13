package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
