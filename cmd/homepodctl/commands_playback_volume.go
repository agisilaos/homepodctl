package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/agisilaos/homepodctl/internal/native"
)

func cmdVolume(ctx context.Context, cfg *native.Config, name string, args []string) {
	flags, positionals, err := parseArgs(args)
	if err != nil {
		die(err)
	}
	opts, err := parseOutputOptions(flags)
	if err != nil {
		die(err)
	}
	backend := strings.TrimSpace(flags.string("backend"))
	if backend == "" {
		backend = cfg.Defaults.Backend
	}

	value := -1
	if v, ok, err := flags.intStrict("value"); err != nil {
		die(err)
	} else if ok {
		value = v
	} else if v, ok, err := flags.intStrict("volume"); err != nil {
		die(err)
	} else if ok {
		value = v
	}
	if value < 0 && len(positionals) > 0 {
		v, err := strconv.Atoi(positionals[0])
		if err != nil {
			die(usageErrf("usage: homepodctl %s <0-100> [<room> ...] [--backend airplay|native]", name))
		}
		value = v
		positionals = positionals[1:]
	}
	if value < 0 || value > 100 {
		die(usageErrf("volume must be 0-100"))
	}

	rooms := append([]string(nil), flags.strings("room")...)
	if len(rooms) == 0 && len(positionals) > 0 {
		rooms = append(rooms, positionals...)
	}
	if len(rooms) == 0 {
		rooms = append(rooms, cfg.Defaults.Rooms...)
	}

	switch backend {
	case "airplay":
		if len(rooms) == 0 {
			rooms = inferSelectedOutputs(ctx)
		}
		if len(rooms) == 0 {
			die(usageErrf("no rooms provided (pass room names, set defaults.rooms via `homepodctl config-init`, or select outputs in Music.app / `homepodctl out set`)"))
		}
		debugf("%s: backend=airplay value=%d rooms=%v", name, value, rooms)
		if opts.DryRun {
			writeActionOutput(name, opts.JSON, opts.Plain, actionOutput{
				DryRun:  true,
				Backend: backend,
				Rooms:   rooms,
			})
			return
		}
		if err := setVolumeForRooms(ctx, rooms, value); err != nil {
			die(err)
		}
		if np, err := getNowPlaying(ctx); err == nil {
			writeActionOutput(name, opts.JSON, opts.Plain, actionOutput{
				Backend:    backend,
				Rooms:      rooms,
				NowPlaying: &np,
			})
		} else {
			writeActionOutput(name, opts.JSON, opts.Plain, actionOutput{
				Backend: backend,
				Rooms:   rooms,
			})
		}
	case "native":
		debugf("%s: backend=native value=%d rooms=%v", name, value, rooms)
		if opts.DryRun {
			writeActionOutput(name, opts.JSON, opts.Plain, actionOutput{
				DryRun:  true,
				Backend: backend,
				Rooms:   rooms,
			})
			return
		}
		if err := runNativeVolumeShortcuts(ctx, cfg, rooms, value); err != nil {
			die(fmt.Errorf("%w (config-native volume is discrete)", err))
		}
		if np, err := getNowPlaying(ctx); err == nil {
			writeActionOutput(name, opts.JSON, opts.Plain, actionOutput{
				Backend:    backend,
				Rooms:      rooms,
				NowPlaying: &np,
			})
		} else {
			writeActionOutput(name, opts.JSON, opts.Plain, actionOutput{
				Backend: backend,
				Rooms:   rooms,
			})
		}
	default:
		die(usageErrf("unknown backend: %q", backend))
	}
}
