package main

import (
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

func TestDiePanicsCliFatal(t *testing.T) {
	defer func() {
		r := recover()
		f, ok := r.(cliFatal)
		if !ok {
			t.Fatalf("panic type=%T, want cliFatal", r)
		}
		if f.err == nil || f.err.Error() != "boom" {
			t.Fatalf("fatal err=%v", f.err)
		}
	}()
	die(errors.New("boom"))
}

func TestExitCodePanicsCliExit(t *testing.T) {
	defer func() {
		r := recover()
		e, ok := r.(cliExit)
		if !ok {
			t.Fatalf("panic type=%T, want cliExit", r)
		}
		if e.code != 7 {
			t.Fatalf("exit code=%d, want 7", e.code)
		}
	}()
	exitCode(7)
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
			"bed": {Rooms: []string{"Bedroom"}, Playlist: "Morning Chill"},
			"lr":  {Rooms: []string{"Living Room"}},
		},
		Native: native.NativeConfig{
			Playlists: map[string]map[string]string{
				"Kitchen": {"Focus": "Y"},
			},
		},
	}
	aliases, rooms, playlists := completionData(cfg)
	if len(aliases) != 2 || aliases[0] != "bed" || aliases[1] != "lr" {
		t.Fatalf("aliases=%v", aliases)
	}
	if len(rooms) != 3 {
		t.Fatalf("rooms=%v", rooms)
	}
	if len(playlists) != 2 || playlists[0] != "Focus" || playlists[1] != "Morning Chill" {
		t.Fatalf("playlists=%v", playlists)
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
