package main

import (
	"context"
	"strings"

	"github.com/agisilaos/homepodctl/internal/native"
)

type playBackend string

const (
	playAirplay playBackend = "airplay"
	playNative  playBackend = "native"
)

type playTargetKind uint8

const (
	playQueryTarget playTargetKind = iota
	playIDTarget
)

// A target holds one selection, never competing query and ID values.
type playTarget struct {
	kind  playTargetKind
	value string
}

type playVolumeSource string

const (
	playVolumeAbsent   playVolumeSource = "absent"
	playVolumeDefault  playVolumeSource = "default"
	playVolumeExplicit playVolumeSource = "explicit"
)

type playVolume struct {
	source playVolumeSource
	value  int // 0-100 when source is default or explicit
}

// Resolution includes defaults and room inference, but not playlist lookup or
// prompting. Both preview and execution consume this request unchanged.
type playRequest struct {
	backend playBackend
	rooms   []string
	target  playTarget
	volume  playVolume
	shuffle bool
	choose  bool
	noInput bool
	output  outputOptions
}

func resolvePlayRequest(ctx context.Context, cfg *native.Config, args []string) (playRequest, error) {
	flags, positionals, err := parseArgs(args)
	if err != nil {
		return playRequest{}, err
	}
	opts, err := parseOutputOptions(flags)
	if err != nil {
		return playRequest{}, err
	}
	target, err := parsePlayTarget(flags, positionals)
	if err != nil {
		return playRequest{}, err
	}

	backend := strings.TrimSpace(flags.string("backend"))
	if backend == "" {
		backend = cfg.Defaults.Backend
	}
	req := playRequest{
		backend: playBackend(backend),
		rooms:   append([]string(nil), flags.strings("room")...),
		target:  target,
		volume:  playVolume{source: playVolumeAbsent},
		output:  opts,
	}
	if len(req.rooms) == 0 {
		req.rooms = append(req.rooms, cfg.Defaults.Rooms...)
	}
	if v, ok, err := flags.intStrict("volume"); err != nil {
		return playRequest{}, err
	} else if ok {
		req.volume = playVolume{source: playVolumeExplicit, value: v}
	} else if cfg.Defaults.Volume != nil {
		req.volume = playVolume{source: playVolumeDefault, value: *cfg.Defaults.Volume}
	}
	if req.volume.source != playVolumeAbsent && (req.volume.value < 0 || req.volume.value > 100) {
		return playRequest{}, usageErrf("volume must be 0-100")
	}
	shuffle, shuffleSet, err := flags.boolStrict("shuffle")
	if err != nil {
		return playRequest{}, err
	}
	if !shuffleSet {
		shuffle = cfg.Defaults.Shuffle
	}
	req.shuffle = shuffle
	if req.choose, _, err = flags.boolStrict("choose"); err != nil {
		return playRequest{}, err
	}
	if req.noInput, _, err = flags.boolStrict("no-input"); err != nil {
		return playRequest{}, err
	}

	switch req.backend {
	case playAirplay:
		if len(req.rooms) == 0 {
			req.rooms = inferSelectedOutputs(ctx)
		}
		if req.volume.source == playVolumeExplicit && len(req.rooms) == 0 {
			return playRequest{}, usageErrf("cannot set volume without rooms (pass --room <name> or select outputs first via `homepodctl out set`)")
		}
	case playNative:
		if len(req.rooms) == 0 {
			return playRequest{}, usageErrf("no rooms provided (pass --room <name> ... or set defaults.rooms via `homepodctl config-init`)")
		}
		// Native text targets are exact configured names, not fuzzy searches.
		req.target.value = strings.TrimSpace(req.target.value)
	default:
		return playRequest{}, usageErrf("unknown backend: %q", backend)
	}
	return req, nil
}

func parsePlayTarget(flags parsedArgs, positionals []string) (playTarget, error) {
	if (flags.has("playlist") && flags.has("playlist-id")) ||
		(len(positionals) > 0 && (flags.has("playlist") || flags.has("playlist-id"))) {
		return playTarget{}, usageErrf("pass exactly one playlist target: <playlist-query>, --playlist, or --playlist-id")
	}
	var target playTarget
	switch {
	case flags.has("playlist-id"):
		target = playTarget{kind: playIDTarget, value: strings.TrimSpace(flags.string("playlist-id"))}
	case flags.has("playlist"):
		target = playTarget{kind: playQueryTarget, value: strings.TrimSpace(flags.string("playlist"))}
	default:
		target = playTarget{kind: playQueryTarget, value: strings.Join(positionals, " ")}
	}
	if strings.TrimSpace(target.value) == "" {
		return playTarget{}, usageErrf("playlist is required (pass <playlist-query>, --playlist, or --playlist-id)")
	}
	return target, nil
}

func (req playRequest) actionOutput() actionOutput {
	out := actionOutput{
		Backend: string(req.backend),
		Rooms:   req.rooms,
		DryRun:  req.output.DryRun,
	}
	switch req.target.kind {
	case playQueryTarget:
		out.Playlist = req.target.value
	case playIDTarget:
		out.PlaylistID = req.target.value
	}
	return out
}
