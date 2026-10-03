package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaybackHelpBeforeConfiguration(t *testing.T) {
	cli := newCLIHarness(t)
	for i, entry := range cli.env {
		if strings.HasPrefix(entry, "PATH=") {
			cli.env[i] = "PATH=" + t.TempDir()
		}
	}
	path := filepath.Join(cli.home, "Library", "Application Support", "homepodctl", "config.json")
	for _, malformed := range []bool{false, true} {
		if malformed {
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		for _, command := range []string{"play", "volume", "vol", "run", "native-run", "setup"} {
			topic := cli.run(t, "help", command)
			for _, flag := range []string{"--help", "-h"} {
				result := cli.run(t, command, flag)
				if result.ExitCode != 0 || result.Stderr != "" || result.Stdout != topic.Stdout {
					t.Fatalf("%s %s malformed=%v: %+v", command, flag, malformed, result)
				}
			}
		}
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "{" {
		t.Fatalf("help changed config: %q %v", contents, err)
	}
}

func TestPlaybackHelpRespectsArgumentBoundaries(t *testing.T) {
	for _, tc := range []struct {
		command   string
		args      []string
		help      bool
		wantError bool
	}{
		{"play", []string{"--playlist", "--help"}, false, false},
		{"play", []string{"--playlist=--help"}, false, false},
		{"play", []string{"--", "--help"}, false, false},
		{"play", []string{"--unknown", "--help"}, false, true},
		{"play", []string{"--room", "Study", "--help"}, true, false},
		{"run", []string{"morning", "-h"}, true, false},
		{"native-run", []string{"--shortcut", "--help"}, false, false},
		{"native-run", []string{"-shortcut", "--help"}, false, false},
		{"setup", []string{"--room", "--help"}, false, false},
		{"volume", []string{"--value", "--help"}, false, false},
		{"vol", []string{"--help"}, true, false},
	} {
		_, _, help, err := parseCommandArgs(tc.command, tc.args)
		if help != tc.help || (err != nil) != tc.wantError {
			t.Fatalf("%s %v: help=%v err=%v", tc.command, tc.args, help, err)
		}
	}
}
