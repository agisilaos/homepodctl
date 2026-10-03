package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/agisilaos/homepodctl/internal/native"
)

func TestCLIRejectsIrrelevantFlags(t *testing.T) {
	cli := newCLIHarness(t)
	for _, command := range []string{
		"setup", "play", "run", "doctor", "devices", "playlists", "aliases",
		"pause", "stop", "next", "prev", "volume", "vol", "native-run",
		"tui",
		"out list", "out set", "automation run", "automation validate",
		"automation plan", "automation init", "config get", "config validate",
		"config set", "schema", "version", "config-init", "help", "completion",
		"completion install", "plan play",
	} {
		t.Run(command, func(t *testing.T) {
			args := append(strings.Fields(command), "--watch=1s", "--json")
			result := cli.run(t, args...)
			if result.ExitCode != exitUsage || result.Stdout != "" {
				t.Fatalf("expected usage failure, got %+v", result)
			}
			var payload jsonErrorResponse
			if err := json.Unmarshal([]byte(result.Stderr), &payload); err != nil {
				t.Fatalf("not a JSON error: %v: %s", err, result.Stderr)
			}
			if payload.Error.Code != "USAGE_ERROR" || !strings.Contains(payload.Error.Message, "--watch") {
				t.Fatalf("unexpected error: %+v", payload)
			}
		})
	}
	for _, args := range [][]string{
		{"status", "--room=Bedroom"}, {"now", "--room=Bedroom"},
		{"schema", "--plain"}, {"config", "get", "defaults.backend", "--plain"},
		{"play", "focus", "-f", "routine.yaml"},
		{"devices", "unexpected", "--room=Bedroom"},
		{"aliases", "unexpected"}, {"native-run", "--dry-run", "unexpected"},
		{"out", "list", "unexpected"}, {"playlists", "unexpected"},
		{"config", "validate", "unexpected"}, {"version", "unexpected"},
		{"config-init", "unexpected"}, {"help", "play", "unexpected"},
		{"tui", "--json"}, {"tui", "--plain"}, {"tui", "--backend", "airplay"},
	} {
		result := cli.run(t, args...)
		if result.ExitCode != exitUsage || result.Stdout != "" {
			t.Errorf("%v: expected usage failure, got %+v", args, result)
		}
	}
	entries, err := os.ReadDir(cli.home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid arguments modified home: entries=%v err=%v", entries, err)
	}
}

func TestCommandFlagSyntaxCompatibility(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, command string
		args          []string
		key, value    string
		pos           []string
	}{
		{"single dash long", "native-run", []string{"-shortcut=Example", "-dry-run=t"}, "dry-run", "true", nil},
		{"short false", "aliases", []string{"-json=F"}, "json", "false", nil},
		{"explicit empty value", "playlists", []string{"--query=", "--json"}, "query", "", nil},
		{"separate false", "devices", []string{"--json", "off"}, "json", "off", nil},
		{"legacy flag-like value", "native-run", []string{"--shortcut", "--json"}, "shortcut", "--json", nil},
		{"delimiter as value", "play", []string{"--room", "--", "--json"}, "room", "--", nil},
		{"literal suffix", "play", []string{"--json=false", "--", "--json", "f"}, "json", "false", []string{"--json", "f"}},
		{"empty boolean equals", "play", []string{"--json="}, "json", "true", nil},
		{"empty equals consumes value", "setup", []string{"--json=", "false"}, "json", "false", nil},
		{"last value wins", "setup", []string{"--json=invalid", "--json=false"}, "json", "false", nil},
		{"volume value alias", "volume", []string{"--value=25"}, "value", "25", nil},
		{"volume volume alias", "vol", []string{"--volume=25"}, "volume", "25", nil},
		{"config values are literal", "config set", []string{"defaults.rooms", "--json", "--watch=1s"}, "", "", []string{"defaults.rooms", "--json", "--watch=1s"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags, pos, err := parseArgs(tc.command, tc.args)
			if err != nil || !slices.Equal(pos, tc.pos) || flags.string(tc.key) != tc.value {
				t.Fatalf("flags=%v pos=%v err=%v", flags.kv, pos, err)
			}
		})
	}
	for _, args := range [][]string{{"--json="}, {"--json=invalid", "--json=false"}} {
		if _, _, err := parseArgs("aliases", args); err == nil {
			t.Errorf("legacy syntax must reject invalid attached booleans: %v", args)
		}
	}
}

func TestJSONErrorModeUsesCommandSyntax(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"status", "--bogus", "--json"}, true},
		{[]string{"status", "--json", "false"}, false},
		{[]string{"status", "--json=true", "--json=false"}, false},
		{[]string{"status", "--json=false", "--json=y"}, true},
		{[]string{"status", "--json="}, true},
		{[]string{"status", "--json= \t "}, true},
		{[]string{"status", "--json=", "off"}, false},
		{[]string{"status", "--json=invalid", "--json=on"}, true},
		{[]string{"status", "--json=on", "--json=invalid"}, false},
		{[]string{"setup", "--room", "--json"}, false},
		{[]string{"setup", "--room=", "--json"}, false},
		{[]string{"setup", "--room", "--", "--json"}, true},
		{[]string{"play", "--", "--json"}, false},
		{[]string{"play", "--json", "--", "--json=false"}, true},
		{[]string{"config", "set", "defaults.rooms", "--json"}, false},
		{[]string{"automation", "run", "-f", "--json"}, false},
		{[]string{"automation", "run", "--file", "--json"}, false},
		{[]string{"completion", "install", "bash", "--path", "--json"}, false},
		{[]string{"playlists", "--query=", "--json"}, true},
		{[]string{"native-run", "-shortcut", "--json"}, false},
		{[]string{"aliases", "-json=t"}, true},
		{[]string{"aliases", "--json="}, false},
		{[]string{"devices", "--json", "no"}, false},
		{[]string{"plan", "play", "focus", "--json=t"}, true},
		{[]string{"plan", "play", "--json", "f"}, true},
		{[]string{"plan", "play", "focus", "--json=f"}, false},
		{[]string{"plan", "play", "focus", "--json="}, false},
		{[]string{"plan", "native-run", "--shortcut", "--json"}, false},
		{[]string{"plan", "play", "--", "--json"}, false},
		{[]string{"plan", "--", "play", "--json"}, false},
		{[]string{"unknown-command", "--json"}, true},
		{[]string{"config", "--json"}, true},
		{[]string{"out", "--json=false"}, false},
		{[]string{"automation", "--bogus", "--json"}, true},
		{[]string{"--bogus", "status", "--json"}, true},
		{[]string{"--json", "false", "status"}, false},
		{[]string{"--json", "status", "--json=false"}, false},
	} {
		if got := wantsJSONErrors(tc.args); got != tc.want {
			t.Errorf("%v: JSON errors=%t, want %t", tc.args, got, tc.want)
		}
	}
}

func TestBooleanVocabularyAcrossCLIAndEnvironment(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"true", true}, {"1", true}, {"yes", true}, {"y", true}, {"on", true},
		{"false", false}, {"0", false}, {"no", false}, {"n", false}, {"off", false},
	} {
		value := " \t" + strings.ToUpper(tc.value) + " "
		for _, command := range []string{"status", "devices"} {
			flags, _, err := parseArgs(command, []string{"--json=" + value})
			if err != nil {
				t.Fatal(err)
			}
			got, present, err := flags.boolStrict("json")
			if err != nil || !present || got != tc.want || wantsJSONErrors([]string{command, "--json=" + value}) != tc.want {
				t.Errorf("%s %q: bool=%t present=%t err=%v", command, value, got, present, err)
			}
		}
		cfg := &native.Config{Aliases: map[string]native.Alias{}}
		for _, key := range []string{"defaults.shuffle", "aliases.test.shuffle"} {
			if err := setConfigPathValue(cfg, key, []string{value}); err != nil {
				t.Fatalf("config %s %q: %v", key, value, err)
			}
		}
		if cfg.Defaults.Shuffle != tc.want || *cfg.Aliases["test"].Shuffle != tc.want {
			t.Errorf("config %q: want %t", value, tc.want)
		}
		if envTruthy(value) != tc.want {
			t.Errorf("environment %q: want %t", value, tc.want)
		}
	}
	for _, value := range []string{"", " \t ", "invalid", "t", "f"} {
		if envTruthy(value) {
			t.Errorf("environment %q should default to false", value)
		}
	}
}

func TestCLIJSONErrorMode(t *testing.T) {
	cli := newCLIHarness(t)
	for _, tc := range []struct {
		args []string
		json bool
	}{
		{[]string{"status", "--bogus", "--json=y"}, true},
		{[]string{"status", "--bogus", "--json", "false"}, false},
		{[]string{"status", "--json", "--json=false", "--bogus"}, false},
		{[]string{"setup", "--room", "--json", "--bogus"}, false},
		{[]string{"play", "--bogus", "--", "--json", "--playlist=x"}, false},
		{[]string{"native-run", "--shortcut", "--json", "--bogus"}, false},
		{[]string{"aliases", "--bogus", "-json=t"}, true},
	} {
		result := cli.run(t, tc.args...)
		if result.ExitCode != exitUsage || json.Valid([]byte(result.Stderr)) != tc.json {
			t.Errorf("%v: expected usage error json=%t, got %+v", tc.args, tc.json, result)
		}
	}
}

func TestLegacyIntegerSyntax(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]int{"50": 50, "010": 8, "0x10": 16, "0b10": 2, "-1": -1} {
		flags, _, err := parseArgs("playlists", []string{"--limit=" + input})
		if err != nil {
			t.Fatal(err)
		}
		got, present, err := flags.intStrictBase("limit", 0)
		if err != nil || !present || got != want {
			t.Errorf("--limit=%s: got=%d want=%d present=%t err=%v", input, got, want, present, err)
		}
	}
}

func TestCLIPlanFlagLookingShortcut(t *testing.T) {
	cli := newCLIHarness(t)
	for _, shortcut := range []string{"--json", "--dry-run=false"} {
		result := cli.run(t, "plan", "native-run", "--shortcut", shortcut, "-dry-run=f", "-json=f", "--json")
		if result.ExitCode != 0 {
			t.Fatalf("plan failed: %+v", result)
		}
		var payload struct {
			Plan actionResult `json:"plan"`
		}
		if err := json.Unmarshal([]byte(result.Stdout), &payload); err != nil {
			t.Fatal(err)
		}
		if !payload.Plan.DryRun || payload.Plan.Shortcut != shortcut {
			t.Fatalf("plan lost shortcut or dry run: %+v", payload)
		}
	}
}
