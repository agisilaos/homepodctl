package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlaylistDiscoveryOutput(t *testing.T) {
	cli := newCLIHarness(t)
	backendDir := t.TempDir()
	backend := filepath.Join(backendDir, "osascript")
	cli.env = append(cli.env, "PATH="+backendDir)
	if err := os.WriteFile(backend, []byte("#!/bin/sh\nprintf 'A1\\tFocus\\tfalse\\tfalse\\nB2\\tFocus Mix\\ttrue\\tfalse\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no matches human", []string{"--query", "missing"}, "No playlists matched. Try a broader --query or omit it.\n"},
		{"no matches JSON", []string{"--query", "missing", "--json"}, "[]\n"},
		{"no matches plain", []string{"--query", "missing", "--plain"}, ""},
		{"match human", []string{"--query", "FOCUS", "--limit", "1"}, "PERSISTENT_ID\tNAME\nA1\tFocus\n"},
		{"match plain unlimited", []string{"--query", "focus", "--limit", "0", "--plain"}, "A1\tFocus\nB2\tFocus Mix\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := cli.run(t, append([]string{"playlists"}, tc.args...)...)
			if r.ExitCode != 0 || r.Stderr != "" || r.Stdout != tc.want {
				t.Fatalf("got %+v; want stdout %q", r, tc.want)
			}
		})
	}
	if err := os.WriteFile(backend, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "No user playlists found in Music.app.\n"},
		{[]string{"--json"}, "[]\n"},
		{[]string{"--plain"}, ""},
	} {
		r := cli.run(t, append([]string{"playlists"}, tc.args...)...)
		if r.ExitCode != 0 || r.Stderr != "" || r.Stdout != tc.want {
			t.Fatalf("empty library: %+v", r)
		}
	}
}
