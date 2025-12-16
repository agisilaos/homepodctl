package main

import "testing"

func TestParseArgs(t *testing.T) {
	t.Parallel()

	flags, pos, err := parseArgs([]string{
		"chill",
		"--backend", "airplay",
		"--room", "Living Room",
		"--room=Bedroom",
		"--shuffle=false",
		"--choose",
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
