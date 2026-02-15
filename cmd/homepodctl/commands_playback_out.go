package main

import (
	"context"
	"flag"
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
		fs := flag.NewFlagSet("out list", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		jsonOut := fs.Bool("json", false, "output JSON")
		includeNetwork := fs.Bool("include-network", false, "include network address (MAC) in JSON output")
		plain := fs.Bool("plain", false, "plain (no header) output")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(exitUsage)
		}
		devs, err := music.ListAirPlayDevices(ctx)
		if err != nil {
			die(err)
		}
		if *jsonOut {
			if !*includeNetwork {
				for i := range devs {
					devs[i].NetworkAddress = ""
				}
			}
			writeJSON(devs)
			return
		}
		printDevicesTable(os.Stdout, devs, *plain)
	case "set":
		flags, positionals, err := parseArgs(args[1:])
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
		rooms := positionals
		if len(rooms) == 0 {
			rooms = append(rooms, cfg.Defaults.Rooms...)
		}
		if len(rooms) == 0 {
			die(usageErrf("no rooms provided (usage: homepodctl out set <room> ...; tip: run `homepodctl devices` to list names)"))
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
