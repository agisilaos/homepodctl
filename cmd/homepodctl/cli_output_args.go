package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/agisilaos/homepodctl/internal/music"
)

func writeJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

type actionResult struct {
	OK         bool              `json:"ok"`
	Action     string            `json:"action"`
	DryRun     bool              `json:"dryRun,omitempty"`
	Backend    string            `json:"backend,omitempty"`
	Rooms      []string          `json:"rooms,omitempty"`
	Playlist   string            `json:"playlist,omitempty"`
	PlaylistID string            `json:"playlistId,omitempty"`
	Shortcut   string            `json:"shortcut,omitempty"`
	NowPlaying *music.NowPlaying `json:"nowPlaying,omitempty"`
}

type actionOutput struct {
	Backend    string
	DryRun     bool
	Rooms      []string
	Playlist   string
	PlaylistID string
	Shortcut   string
	NowPlaying *music.NowPlaying
}

type outputOptions struct {
	JSON   bool
	Plain  bool
	DryRun bool
}

func parseOutputFlags(flags parsedArgs) (bool, bool, error) {
	jsonOut, _, err := flags.boolStrict("json")
	if err != nil {
		return false, false, err
	}
	plainOut, _, err := flags.boolStrict("plain")
	if err != nil {
		return false, false, err
	}
	return jsonOut, plainOut, nil
}

func parseOutputOptions(flags parsedArgs) (outputOptions, error) {
	jsonOut, plainOut, err := parseOutputFlags(flags)
	if err != nil {
		return outputOptions{}, err
	}
	dryRun, _, err := flags.boolStrict("dry-run")
	if err != nil {
		return outputOptions{}, err
	}
	return outputOptions{
		JSON:   jsonOut,
		Plain:  plainOut,
		DryRun: dryRun,
	}, nil
}

func writeActionOutput(action string, jsonOut bool, plainOut bool, out actionOutput) {
	if jsonOut {
		writeJSON(actionResult{
			OK:         true,
			Action:     action,
			DryRun:     out.DryRun,
			Backend:    out.Backend,
			Rooms:      out.Rooms,
			Playlist:   out.Playlist,
			PlaylistID: out.PlaylistID,
			Shortcut:   out.Shortcut,
			NowPlaying: out.NowPlaying,
		})
		return
	}
	if out.NowPlaying != nil {
		if plainOut {
			printNowPlayingPlain(*out.NowPlaying)
		} else {
			printNowPlaying(*out.NowPlaying)
		}
		return
	}
	if out.DryRun {
		fmt.Printf("dry-run action=%s backend=%s rooms=%s playlist=%q playlist_id=%q shortcut=%q\n",
			action,
			out.Backend,
			strings.Join(out.Rooms, ","),
			out.Playlist,
			out.PlaylistID,
			out.Shortcut,
		)
	}
}

type parsedArgs struct {
	kv map[string][]string
}

func (p parsedArgs) has(key string) bool {
	v := p.strings(key)
	return len(v) > 0
}

func (p parsedArgs) strings(key string) []string {
	if p.kv == nil {
		return nil
	}
	return p.kv[key]
}

func (p parsedArgs) string(key string) string {
	v := p.strings(key)
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1]
}

func (p parsedArgs) int(key string, def int) int {
	s := strings.TrimSpace(p.string(key))
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func (p parsedArgs) intStrict(key string) (int, bool, error) {
	if !p.has(key) {
		return 0, false, nil
	}
	s := strings.TrimSpace(p.string(key))
	if s == "" {
		return 0, true, usageErrf("--%s requires a value", key)
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, true, usageErrf("invalid --%s %q", key, s)
	}
	return n, true, nil
}

func (p parsedArgs) bool(key string) (bool, bool) {
	v := p.strings(key)
	if len(v) == 0 {
		return false, false
	}
	s := strings.TrimSpace(v[len(v)-1])
	if s == "" {
		return true, true
	}
	switch strings.ToLower(s) {
	case "true", "1", "yes", "y", "on":
		return true, true
	case "false", "0", "no", "n", "off":
		return false, true
	default:
		return false, false
	}
}

func (p parsedArgs) boolStrict(key string) (bool, bool, error) {
	if !p.has(key) {
		return false, false, nil
	}
	b, ok := p.bool(key)
	if ok {
		return b, true, nil
	}
	return false, true, usageErrf("invalid --%s %q (expected true/false)", key, p.string(key))
}

func (p parsedArgs) boolDefault(key string, def bool) bool {
	b, ok := p.bool(key)
	if !ok {
		return def
	}
	return b
}

func parseArgs(args []string) (parsedArgs, []string, error) {
	out := parsedArgs{kv: map[string][]string{}}
	var positionals []string

	push := func(k, v string) {
		out.kv[k] = append(out.kv[k], v)
	}

	isBoolWord := func(s string) bool {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true", "false", "1", "0", "yes", "no", "y", "n", "on", "off":
			return true
		default:
			return false
		}
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if a == "-h" || a == "--help" {
			usage()
			exitCode(0)
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positionals = append(positionals, a)
			continue
		}

		if strings.HasPrefix(a, "--") {
			key := strings.TrimPrefix(a, "--")
			val := ""
			if eq := strings.IndexByte(key, '='); eq >= 0 {
				val = key[eq+1:]
				key = key[:eq]
			}

			switch key {
			case "backend", "playlist", "playlist-id", "volume", "value", "room":
				if key == "room" {
					if val == "" {
						if i+1 >= len(args) {
							return parsedArgs{}, nil, usageErrf("--room requires a value")
						}
						i++
						val = args[i]
					}
					push("room", val)
					continue
				}
				if val == "" {
					if i+1 >= len(args) {
						return parsedArgs{}, nil, usageErrf("--%s requires a value", key)
					}
					i++
					val = args[i]
				}
				push(key, val)
			case "shuffle", "choose", "json", "plain", "dry-run":
				if val == "" && i+1 < len(args) && isBoolWord(args[i+1]) {
					i++
					val = args[i]
				}
				if val == "" {
					val = "true"
				}
				push(key, val)
			default:
				return parsedArgs{}, nil, usageErrf("unknown flag: %s (tip: rooms use --room <name>; run `homepodctl help`)", a)
			}
			continue
		}

		return parsedArgs{}, nil, usageErrf("unknown flag: %s (tip: run `homepodctl help`)", a)
	}
	return out, positionals, nil
}
