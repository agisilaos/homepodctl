package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/agisilaos/homepodctl/internal/native"
)

func TestRenderCompletionRejectsNUL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values completionValues
	}{
		{name: "alias", values: completionValues{aliases: []string{"bad\x00alias"}}},
		{name: "room", values: completionValues{rooms: []string{"bad\x00room"}}},
		{name: "playlist", values: completionValues{playlists: []string{"bad\x00playlist"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, shell := range []string{"bash", "zsh", "fish"} {
				if _, err := renderCompletion(shell, tc.values); err == nil || !strings.Contains(err.Error(), "NUL") {
					t.Errorf("renderCompletion(%q) error=%v, want NUL error", shell, err)
				}
			}
		})
	}
}

func TestRenderCompletionRejectsFishTabs(t *testing.T) {
	t.Parallel()

	values := completionValues{aliases: []string{"tab\talias"}}
	if _, err := renderCompletion("fish", values); err == nil || !strings.Contains(err.Error(), "Fish reserves") {
		t.Fatalf("renderCompletion(fish) error=%v, want reserved-tab error", err)
	}
	for _, shell := range []string{"bash", "zsh"} {
		if _, err := renderCompletion(shell, values); err != nil {
			t.Errorf("renderCompletion(%q) error=%v, want tab support", shell, err)
		}
	}
}

func TestCompletionScriptReportsConfigErrors(t *testing.T) {
	writeCompletionConfig(t, []byte("{"))

	if _, err := completionScript("bash"); err == nil || !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("completionScript error=%v, want config parse error", err)
	}
}

func TestCompletionScriptRejectsNULFromConfig(t *testing.T) {
	writeCompletionConfig(t, []byte(`{"aliases":{"bad\u0000alias":{}}}`))

	if _, err := completionScript("fish"); err == nil || !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("completionScript error=%v, want NUL error", err)
	}
}

func TestCompletionDataPreservesWhitespaceRules(t *testing.T) {
	t.Parallel()

	cfg := &native.Config{
		Defaults: native.DefaultsConfig{Rooms: []string{"  Default Room  "}},
		Aliases: map[string]native.Alias{
			"  alias  ": {Rooms: []string{"  Alias Room  "}, Playlist: "  Alias Playlist  "},
		},
		Native: native.NativeConfig{
			Playlists: map[string]map[string]string{
				"  Native Playlist Room  ": {"  Native Playlist  ": "shortcut"},
			},
			VolumeShortcuts: map[string]map[string]string{
				"  Native Volume Room  ": {"50": "shortcut"},
			},
		},
	}
	want := completionValues{
		aliases: []string{"  alias  "},
		rooms: []string{
			"Alias Room",
			"Default Room",
			"  Native Playlist Room  ",
			"  Native Volume Room  ",
		},
		playlists: []string{"Alias Playlist", "Native Playlist"},
	}
	sort.Strings(want.rooms)

	if got := completionData(cfg); !reflect.DeepEqual(got, want) {
		t.Fatalf("completionData=%#v, want %#v", got, want)
	}
}

func TestBashCompletionCandidatesAreOpaque(t *testing.T) {
	bash := requireShell(t, "bash")
	sentinel := filepath.Join(t.TempDir(), "executed")
	candidates := hostileCompletionCandidates(sentinel)
	script, err := renderCompletion("bash", repeatedCompletionValues(candidates))
	if err != nil {
		t.Fatalf("renderCompletion(bash): %v", err)
	}
	assertShellSyntax(t, bash, []string{"--noprofile", "--norc", "-n"}, script)

	tests := []struct {
		name  string
		setup string
		want  []string
	}{
		{
			name:  "aliases",
			setup: "COMP_WORDS=(homepodctl run '')\nCOMP_CWORD=2",
			want:  candidates,
		},
		{
			name:  "rooms",
			setup: "COMP_WORDS=(homepodctl play --room '')\nCOMP_CWORD=3",
			want:  candidates,
		},
		{
			name:  "playlists",
			setup: "COMP_WORDS=(homepodctl play '')\nCOMP_CWORD=2",
			want:  candidates,
		},
		{
			name:  "literal glob prefix",
			setup: "COMP_WORDS=(homepodctl run 'glob*')\nCOMP_CWORD=2",
			want:  []string{`glob*?[abc]`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			harness := script + "\n" + tc.setup + "\n_homepodctl_completion\nprintf '%s\\0' \"${COMPREPLY[@]}\"\n"
			got := runShellScript(t, bash, []string{"--noprofile", "--norc"}, harness)
			assertNULValues(t, got, tc.want)
			assertNotCreated(t, sentinel)
		})
	}
}

func TestZshCompletionCandidatesAreOpaque(t *testing.T) {
	zsh := requireShell(t, "zsh")
	sentinel := filepath.Join(t.TempDir(), "executed")
	candidates := hostileCompletionCandidates(sentinel)
	script, err := renderCompletion("zsh", repeatedCompletionValues(candidates))
	if err != nil {
		t.Fatalf("renderCompletion(zsh): %v", err)
	}
	assertShellSyntax(t, zsh, []string{"-f", "-n"}, script)

	tests := []struct {
		name    string
		words   string
		current int
		want    []string
	}{
		{name: "aliases", words: "words=(homepodctl run '')", current: 3, want: candidates},
		{name: "rooms", words: "words=(homepodctl play --room '')", current: 4, want: candidates},
		{name: "playlists", words: "words=(homepodctl play '')", current: 3, want: candidates},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			harness := zshCompletionHarness(tc.words, tc.current) + script + "\nprintf '%s\\0' \"${captured[@]}\"\n"
			got := runShellScript(t, zsh, []string{"-f"}, harness)
			assertNULValues(t, got, tc.want)
			assertNotCreated(t, sentinel)
		})
	}
}

func TestFishCompletionArgumentsAreOpaque(t *testing.T) {
	fish := requireFish(t)
	sentinel := filepath.Join(t.TempDir(), "executed")
	candidates := fishHostileCompletionCandidates(sentinel)
	harness := "set deferred " + fishCompletionArguments(candidates) + "\n" +
		"set decoded\n" +
		"eval \"set decoded $deferred\"\n" +
		"printf '%s\\0' $decoded\n"
	got := runShellScript(t, fish, []string{"--no-config"}, harness)
	assertNULValues(t, got, candidates)
	assertNotCreated(t, sentinel)
}

func TestFishCompletionScriptCandidatesAreOpaque(t *testing.T) {
	fish := requireFish(t)
	sentinel := filepath.Join(t.TempDir(), "executed")
	candidates := fishHostileCompletionCandidates(sentinel)
	values := repeatedCompletionValues(candidates)
	script, err := renderCompletion("fish", values)
	if err != nil {
		t.Fatalf("renderCompletion(fish): %v", err)
	}
	assertShellSyntax(t, fish, []string{"--no-config", "-n"}, script)

	for _, commandLine := range []string{
		"homepodctl run ",
		"homepodctl play --room ",
		"homepodctl play ",
	} {
		output := runShellScript(t, fish, []string{"--no-config"}, script+"\ncomplete -C '"+commandLine+"'\n")
		for _, candidate := range candidates {
			record := []byte(candidate + "\n")
			if count := bytes.Count(output, record); count != 1 {
				t.Errorf("complete -C %q contains candidate %q %d times, want once\noutput: %q", commandLine, candidate, count, output)
			}
		}
		assertNotCreated(t, sentinel)
	}
}

func TestFishCompletionScriptConsolidatesDynamicCandidates(t *testing.T) {
	t.Parallel()

	script, err := renderCompletion("fish", completionValues{
		aliases:   []string{"one", "two"},
		rooms:     []string{"one", "two"},
		playlists: []string{"one", "two"},
	})
	if err != nil {
		t.Fatalf("renderCompletion(fish): %v", err)
	}
	conditions := []string{
		"__fish_seen_subcommand_from run",
		"__fish_seen_argument --room",
		"__fish_seen_subcommand_from out; and __fish_seen_subcommand_from set",
		"__fish_seen_subcommand_from play",
		"__fish_seen_argument --playlist",
	}
	for _, condition := range conditions {
		if count := strings.Count(script, fishStringLiteral(condition)); count != 1 {
			t.Errorf("condition %q appears %d times, want one registration", condition, count)
		}
	}
}

func hostileCompletionCandidates(sentinel string) []string {
	return []string{
		"opaque-plain",
		"two words",
		"single'quote",
		`double"quote`,
		"$(touch " + sentinel + ")",
		"`touch " + sentinel + "`",
		"dollar-$HOME",
		"semi;colon",
		`back\slash`,
		`glob*?[abc]`,
		"colon:value",
		"snowman-☃",
		"line one\nline two",
		"tab\tvalue",
		"carriage\rreturn",
		"-leading-dash",
	}
}

func fishHostileCompletionCandidates(sentinel string) []string {
	candidates := hostileCompletionCandidates(sentinel)
	result := make([]string, 0, len(candidates)-1)
	for _, candidate := range candidates {
		if !strings.ContainsRune(candidate, '\t') {
			result = append(result, candidate)
		}
	}
	return result
}

func repeatedCompletionValues(candidates []string) completionValues {
	return completionValues{
		aliases:   append([]string(nil), candidates...),
		rooms:     append([]string(nil), candidates...),
		playlists: append([]string(nil), candidates...),
	}
}

func requireShell(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("required shell %q is not installed: %v", name, err)
	}
	return path
}

func requireFish(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("fish")
	if err != nil {
		t.Skipf("fish is optional locally and installed by CI: %v", err)
	}
	return path
}

func writeCompletionConfig(t *testing.T, contents []byte) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	path, err := native.ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func assertShellSyntax(t *testing.T, shell string, args []string, script string) {
	t.Helper()
	cmd := exec.Command(shell, args...)
	cmd.Stdin = strings.NewReader(script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s syntax check failed: %v\n%s", filepath.Base(shell), err, output)
	}
}

func runShellScript(t *testing.T, shell string, args []string, script string) []byte {
	t.Helper()
	cmd := exec.Command(shell, args...)
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s script failed: %v\n%s", filepath.Base(shell), err, output)
	}
	return output
}

func assertNULValues(t *testing.T, output []byte, want []string) {
	t.Helper()
	parts := bytes.Split(output, []byte{0})
	if len(parts) == 0 || len(parts[len(parts)-1]) != 0 {
		t.Fatalf("output is not NUL-terminated: %q", output)
	}
	parts = parts[:len(parts)-1]
	got := make([]string, len(parts))
	for i, part := range parts {
		got[i] = string(part)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("completion values mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}

func assertNotCreated(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("completion candidate executed and created %s", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat sentinel: %v", err)
	}
}

func zshCompletionHarness(words string, current int) string {
	return `typeset -ga captured
_description() { : }
_arguments() { : }
_describe() { : }
compadd() {
  local array_name
  while (( $# > 0 )); do
    if [[ "$1" == -a ]]; then
      array_name="$2"
      shift 2
    else
      shift
    fi
  done
  captured=("${(@P)array_name}")
}
` + words + "\nCURRENT=" + strconv.Itoa(current) + "\n"
}
