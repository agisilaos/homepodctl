package main

import (
	"os"
	"strings"
	"testing"
)

func TestDiscoveryHelpRoutesOffline(t *testing.T) {
	cli := newCLIHarness(t)
	for i, entry := range cli.env {
		if strings.HasPrefix(entry, "PATH=") {
			cli.env[i] = "PATH=" + t.TempDir()
		}
	}
	for _, command := range []string{"devices", "status", "now"} {
		expected := "homepodctl " + command + " -"
		if command == "now" {
			expected = "homepodctl status -"
		}
		help := cli.run(t, "help", command)
		flag := cli.run(t, command, "--help")
		if help.ExitCode != 0 || flag.ExitCode != 0 || help.Stderr != "" || flag.Stderr != "" || help.Stdout != flag.Stdout || !strings.HasPrefix(help.Stdout, expected) {
			t.Fatalf("%s: focused help routes differ: %+v %+v", command, help, flag)
		}
	}
	entries, err := os.ReadDir(cli.home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("help created state: %v %v", entries, err)
	}
}
