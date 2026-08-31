package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestCompletionFlagParity(t *testing.T) {
	commands := sortedKeys(commandFlagSpecs)
	commands = append(commands, sortedKeys(commandFlagAliases)...)
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			want := []string{"--help", "-h"}
			if command == "plan" {
				want = nil // plan --help is a usage error, not a target flag.
			}
			for name := range flagsForCommand(command).flags {
				if name == "f" {
					want = append(want, "-f")
				} else {
					want = append(want, "--"+name)
				}
			}
			words := append([]string{"homepodctl"}, strings.Fields(command)...)
			assertCompletionParity(t, append(words, "-"), want)
		})
	}
}

func TestCompletionContextParity(t *testing.T) {
	commands := strings.Fields("aliases automation completion config config-init devices doctor help native-run next now out pause plan play playlists prev run schema setup status stop version vol volume")
	playFlags := strings.Fields("--backend --choose --dry-run --help --json --no-input --plain --playlist --playlist-id --room --shuffle --volume -h")
	tests := []struct {
		name  string
		words []string
		want  []string
	}{
		{"root", []string{""}, commands},
		{"root flags", []string{"-"}, strings.Fields("--help --quiet --verbose --version -h -q -v")},
		{"global option then command", []string{"-v", ""}, commands},
		{"global option then leaf", []string{"--quiet", "play", "-"}, playFlags},
		{"root version terminates", []string{"--version", ""}, nil},
		{"help targets", []string{"help", ""}, commands},
		{"help target terminates", []string{"help", "play", "-"}, nil},
		{"automation children", []string{"automation", ""}, strings.Fields("init plan run validate")},
		{"config children", []string{"config", ""}, strings.Fields("get set validate")},
		{"out children", []string{"out", ""}, strings.Fields("list set")},
		{"parent rejects flags", []string{"automation", "--"}, nil},
		{"plan targets", []string{"plan", ""}, strings.Fields("automation native-run out play run vol volume")},
		{"plan out", []string{"plan", "out", ""}, []string{"set"}},
		{"plan automation", []string{"plan", "automation", ""}, []string{"run"}},
		{"unsupported plan target", []string{"plan", "status", "-"}, nil},
		{"unsupported plan child", []string{"plan", "automation", "init", "-"}, nil},
		{"plan play flags", []string{"plan", "--json", "play", "-"}, strings.Fields("--backend --choose --dry-run --json --no-input --plain --playlist --playlist-id --room --shuffle --volume")},
		{"plan json before child", []string{"plan", "out", "--json", "set", "--r"}, []string{"--room"}},
		{"plan automation flags", []string{"plan", "automation", "run", "-"}, strings.Fields("--dry-run --file --json --no-input -f")},
		{"plan delimiter before target", []string{"plan", "--", "play", "--c"}, []string{"--choose"}},
		{"post-command global flags excluded", []string{"play", "--v"}, []string{"--volume"}},
		{"irrelevant flag excluded", []string{"setup", "--c"}, nil},
		{"short file alias", []string{"automation", "run", "-f"}, []string{"-f"}},
		{"not a long f alias", []string{"automation", "run", "--f"}, []string{"--file"}},
		{"legacy spellings not advertised", []string{"devices", "-j"}, nil},
		{"legacy spelling still understood", []string{"playlists", "-query", "play", "--"}, strings.Fields("--help --json --limit --plain --query")},
		{"shell choices", []string{"completion", ""}, strings.Fields("bash fish install zsh")},
		{"install shells", []string{"completion", "install", ""}, strings.Fields("bash fish zsh")},
		{"install path consumed", []string{"completion", "install", "--path", "/tmp", ""}, strings.Fields("bash fish zsh")},
		{"no file candidates", []string{"completion", "install", "bash", "--path", ""}, nil},
		{"install path flag", []string{"completion", "install", "bash", "--p"}, []string{"--path"}},
		{"config literal tail", []string{"config", "set", "defaults.rooms", "--"}, nil},
		{"delimiter ends flags", []string{"play", "--", "--c"}, nil},
		{"delimiter keeps positionals", []string{"play", "--", ""}, []string{"Morning Mix", "Quiet"}},
		{"literal delimiter is a flag value", []string{"play", "--room", "--", "--c"}, []string{"--choose"}},
		{"value is a command name", []string{"native-run", "--shortcut", "play", "--c"}, nil},
		{"pending value hides flags", []string{"play", "--playlist", "--c"}, nil},
		{"flag-looking value keeps target", []string{"plan", "native-run", "--shortcut", "--json", "--s"}, []string{"--shortcut"}},
		{"option before alias", []string{"run", "--json", "mor"}, []string{"morning"}},
		{"option before playlist", []string{"play", "--room", "Kitchen", ""}, []string{"Morning Mix", "Quiet"}},
		{"room values", []string{"play", "--room", ""}, []string{"Kitchen", "Living Room"}},
		{"inline room values", []string{"play", "--room=L"}, []string{"--room=Living Room"}},
		{"inline playlist values", []string{"play", "--playlist=M"}, []string{"--playlist=Morning Mix"}},
		{"out room positionals", []string{"out", "set", ""}, []string{"Kitchen", "Living Room"}},
		{"repeat positional rooms", []string{"out", "set", "Kitchen", "L"}, []string{"Living Room"}},
		{"volume room positionals", []string{"volume", "40", "L"}, []string{"Living Room"}},
		{"volume value option", []string{"volume", "--value", "40", "L"}, []string{"Living Room"}},
		{"volume volume option", []string{"vol", "--volume=40", ""}, []string{"Kitchen", "Living Room"}},
		{"plan volume option", []string{"plan", "volume", "--volume", "40", ""}, []string{"Kitchen", "Living Room"}},
		{"volume number has no candidates", []string{"volume", "4"}, nil},
		{"repeat room option", []string{"play", "--room", "Kitchen", "--r"}, []string{"--room"}},
		{"empty equals consumes next value", []string{"play", "--room=", "Kitchen", "--c"}, []string{"--choose"}},
		{"legacy empty equals does not consume", []string{"completion", "install", "--path=", "bash", "--p"}, []string{"--path"}},
		{"optional boolean permits flags", []string{"play", "--choose", "--r"}, []string{"--room"}},
		{"boolean assignment", []string{"play", "--choose="}, []string{"--choose=false", "--choose=true"}},
		{"boolean value prefix", []string{"play", "--choose=f"}, []string{"--choose=false"}},
		{"separate boolean consumed", []string{"play", "--choose", " YES ", ""}, []string{"Morning Mix", "Quiet"}},
		{"short boolean stays positional", []string{"play", "--choose", "f", "M"}, nil},
		{"presets", []string{"automation", "init", "--preset", ""}, strings.Fields("focus morning party reset winddown")},
		{"backends", []string{"setup", "--backend", ""}, strings.Fields("airplay native")},
		{"inline backend", []string{"setup", "--backend=n"}, []string{"--backend=native"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertCompletionParity(t, append([]string{"homepodctl"}, tc.words...), tc.want)
		})
	}
}

func assertCompletionParity(t *testing.T, words, want []string) {
	t.Helper()
	values := completionValues{aliases: []string{"morning", "party"}, rooms: []string{"Kitchen", "Living Room"}, playlists: []string{"Morning Mix", "Quiet"}}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			got := completionCandidates(t, shell, values, words)
			sortedWant := append([]string{}, want...)
			sort.Strings(sortedWant)
			sort.Strings(got)
			if strings.Join(got, "\x00") != strings.Join(sortedWant, "\x00") || len(got) != len(sortedWant) {
				t.Fatalf("%q candidates\ngot:  %#v\nwant: %#v", words, got, sortedWant)
			}
		})
	}
}

func completionCandidates(t *testing.T, shell string, values completionValues, words []string) []string {
	t.Helper()
	script, err := renderCompletion(shell, values)
	if err != nil {
		t.Fatal(err)
	}
	var output []byte
	switch shell {
	case "bash":
		prefix := ""
		if current := words[len(words)-1]; strings.Contains(current, "=") {
			prefix = current[:strings.LastIndex(current, "=")+1]
		}
		harness := script + fmt.Sprintf("\nCOMP_WORDS=(%s)\nCOMP_CWORD=%d\n_homepodctl_completion\n", bashArrayLiteral(words), len(words)-1) +
			decodeBashReplies(prefix, "")
		output = runShellScript(t, requireShell(t, "bash"), []string{"--noprofile", "--norc"}, harness)
	case "zsh":
		harness := zshCompletionHarness("words=("+zshArrayLiteral(words)+")", len(words)) + script +
			"if (( ${#captured[@]} )); then printf '%s\\0' \"${captured[@]}\"; fi\n"
		output = runShellScript(t, requireShell(t, "zsh"), []string{"-f"}, harness)
	case "fish":
		commandLine := fishArrayLiteral(words[:len(words)-1]) + " "
		if current := words[len(words)-1]; current != "" {
			commandLine += fishStringLiteral(current)
		}
		output = runShellScript(t, requireFish(t), []string{"--no-config"}, script+"\ncomplete -C "+fishStringLiteral(commandLine)+"\n")
		var candidates []string
		for _, line := range strings.Split(strings.TrimSuffix(string(output), "\n"), "\n") {
			if line != "" {
				value, _, _ := strings.Cut(line, "\t")
				candidates = append(candidates, value)
			}
		}
		return candidates
	}
	if len(output) == 0 {
		return nil
	}
	if !bytes.HasSuffix(output, []byte{0}) {
		t.Fatalf("completion output is not NUL terminated: %q", output)
	}
	return strings.Split(string(output[:len(output)-1]), "\x00")
}

func TestBashCompletionWordBreakEquals(t *testing.T) {
	for _, words := range [][]string{
		{"homepodctl", "play", "--room", "=", "L"},
		{"homepodctl", "play", "--room", "=", "Living Room", "--choose", "=", "f"},
	} {
		got := completionCandidates(t, "bash", completionValues{rooms: []string{"Living Room"}}, words)
		want := "Living Room"
		if words[len(words)-1] == "f" {
			want = "false"
		}
		if len(got) != 1 || got[0] != want {
			t.Fatalf("split equals %q: got %q, want %q", words, got, want)
		}
	}
}

func TestCompletionRawShellWords(t *testing.T) {
	values := completionValues{rooms: []string{"Living Room"}, playlists: []string{"Morning Mix"}}
	tests := []struct {
		name, line, prefix string
		words              []string
		want               string
	}{
		{"open quote", "homepodctl play --room 'Liv", "Liv", []string{"homepodctl", "play", "--room", "'Liv"}, "Living Room"},
		{"quoted boolean", "homepodctl play --choose ' YES ' M", "M", []string{"homepodctl", "play", "--choose", "' YES '", "M"}, "Morning Mix"},
		{"quoted equals value", "homepodctl play --room '=' --c", "--c", []string{"homepodctl", "play", "--room", "'='", "--c"}, "--choose"},
		{"standalone equals value", "homepodctl play --room = --c", "--c", []string{"homepodctl", "play", "--room", "=", "--c"}, "--choose"},
		{"inline quoted room", "homepodctl play --room=\"Living Room\" --c", "--c", []string{"homepodctl", "play", "--room=\"Living Room\"", "--c"}, "--choose"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, shell := range []string{"bash", "zsh"} {
				script, err := renderCompletion(shell, values)
				if err != nil {
					t.Fatal(err)
				}
				var output []byte
				if shell == "bash" {
					quote := ""
					if tc.name == "open quote" {
						quote = "'"
					}
					harness := script + fmt.Sprintf("\nCOMP_LINE=%s\nCOMP_WORDS=(%s)\nCOMP_CWORD=%d\n_homepodctl_completion\n",
						bashArrayLiteral([]string{tc.line}), bashArrayLiteral(tc.words), len(tc.words)-1)
					harness += decodeBashReplies("", quote)
					output = runShellScript(t, requireShell(t, shell), []string{"--noprofile", "--norc"}, harness)
				} else {
					harness := zshCompletionHarness("words=("+zshArrayLiteral(tc.words)+")", len(tc.words)) +
						"PREFIX=" + zshArrayLiteral([]string{tc.prefix}) + "\n" + script + "\nprintf '%s\\0' \"${captured[@]}\"\n"
					output = runShellScript(t, requireShell(t, shell), []string{"-f"}, harness)
				}
				assertNULValues(t, output, []string{tc.want})
			}
		})
	}
}

// Only tests evaluate returned completion syntax: this simulates accepting a
// candidate and executing the command. Hostile fixtures assert no substitution
// runs and each decoded candidate remains exactly one argument.
func decodeBashReplies(prefix, quote string) string {
	return "decoded=()\nprefix=" + bashArrayLiteral([]string{prefix}) + "\nquote_marker=" + bashArrayLiteral([]string{quote}) + `
for reply in "${COMPREPLY[@]}"; do
  eval "set -- $quote_marker$reply$quote_marker"
  for value in "$@"; do decoded+=("$prefix$value"); done
done
if (( ${#decoded[@]} )); then printf '%s\0' "${decoded[@]}"; fi
`
}

func TestBashReadlinePreservesArguments(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("python3 is required by repository verification and the Readline harness")
	}
	dir := t.TempDir()
	values := completionValues{rooms: []string{"Living Room", "Danger$(printf SENTINEL)", "single'quote", "double\"quote", `back\slash`}}
	script, err := renderCompletion("bash", values)
	if err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(dir, "completion.bash")
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	// A room whose name is also a directory must not gain a trailing slash.
	if err := os.Mkdir(filepath.Join(dir, "Living Room"), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "testdata/bash_readline.py", requireShell(t, "bash"), scriptPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Readline argument round trip: %v\n%s", err, output)
	}
}
