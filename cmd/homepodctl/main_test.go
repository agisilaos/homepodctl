package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/agisilaos/homepodctl/internal/music"
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
