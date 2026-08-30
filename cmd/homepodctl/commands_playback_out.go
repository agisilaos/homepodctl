package main

import (
	"context"
	"os"
	"strings"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func cmdOut(ctx context.Context, cfg *native.Config, args []string) {
	if len(args) < 1 {
		die(usageErrf("usage: homepodctl out <list|set> [args]"))
	}
	switch args[0] {
	case "list":
		flags := parseFlagOnlyArgs("out list", args[1:])
		jsonOut := flags.boolDefault("json", false)
		includeNetwork := flags.boolDefault("include-network", false)
		plain := flags.boolDefault("plain", false)
		devs, err := music.ListAirPlayDevices(ctx)
		if err != nil {
			die(err)
		}
		if jsonOut {
			if !includeNetwork {
				for i := range devs {
					devs[i].NetworkAddress = ""
				}
			}
			writeJSON(devs)
			return
		}
		printDevicesTable(os.Stdout, devs, plain)
	case "set":
		flags, positionals, err := parseArgs("out set", args[1:])
		if err != nil {
			die(err)
		}
		opts, err := parseOutputOptions(flags)
		if err != nil {
			die(err)
		}
		backend := strings.TrimSpace(flags.string("backend"))
		if backend == "" {
			backend = "airplay"
		}
		if backend != "airplay" {
			die(usageErrf("out set only supports backend=airplay (got %q)", backend))
		}
		rooms := append([]string(nil), flags.strings("room")...)
		if len(rooms) == 0 {
			rooms = append(rooms, positionals...)
		}
		if len(rooms) == 0 {
			rooms = append(rooms, cfg.Defaults.Rooms...)
		}
		if len(rooms) == 0 {
			die(usageErrf("no rooms provided (usage: homepodctl out set --room <name> [--room <name> ...]; tip: run `homepodctl devices` to list names)"))
		}
		debugf("out set: backend=%s rooms=%v", backend, rooms)
		if opts.DryRun {
			writeActionOutput("out.set", opts.JSON, opts.Plain, actionOutput{
				DryRun:  true,
				Backend: backend,
				Rooms:   rooms,
			})
			return
		}
		if err := setCurrentOutputs(ctx, rooms); err != nil {
			die(err)
		}
		if np, err := getNowPlaying(ctx); err == nil {
			writeActionOutput("out.set", opts.JSON, opts.Plain, actionOutput{
				Backend:    backend,
				Rooms:      rooms,
				NowPlaying: &np,
			})
		} else {
			writeActionOutput("out.set", opts.JSON, opts.Plain, actionOutput{
				Backend: backend,
				Rooms:   rooms,
			})
		}
	default:
		die(usageErrf("usage: homepodctl out <list|set> [args]"))
	}
}
