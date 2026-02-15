package main

import (
	"context"
	"flag"
	"os"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
)

func cmdStatus(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "output JSON")
	plain := fs.Bool("plain", false, "plain output")
	watch := fs.Duration("watch", 0, "poll interval (e.g. 1s); 0 prints once")
	if err := fs.Parse(args); err != nil {
		os.Exit(exitUsage)
	}
	debugf("status: json=%t plain=%t watch=%s", *jsonOut, *plain, watch.String())
	printOnce := func() error {
		np, err := getNowPlaying(ctx)
		if err != nil {
			return err
		}
		if *jsonOut {
			writeJSON(np)
			return nil
		}
		if *plain {
			printNowPlayingPlain(np)
		} else {
			printNowPlaying(np)
		}
		return nil
	}
	if err := runStatusLoop(ctx, *watch, printOnce); err != nil {
		die(err)
	}
}

func runStatusLoop(ctx context.Context, watch time.Duration, printOnce func() error) error {
	if watch <= 0 {
		return printOnce()
	}
	ticker := newStatusTicker(watch)
	defer ticker.Stop()
	for {
		if err := printOnce(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.Chan():
		}
	}
}

func cmdTransport(ctx context.Context, args []string, action string, fn func(context.Context) error) {
	flags, positionals, err := parseArgs(args)
	if err != nil {
		die(err)
	}
	if len(positionals) != 0 {
		die(usageErrf("usage: homepodctl %s [--json] [--plain]", action))
	}
	jsonOut, plainOut, err := parseOutputFlags(flags)
	if err != nil {
		die(err)
	}
	if err := fn(ctx); err != nil {
		die(err)
	}
	if np, err := music.GetNowPlaying(ctx); err == nil {
		writeActionOutput(action, jsonOut, plainOut, actionOutput{NowPlaying: &np})
		return
	}
	writeActionOutput(action, jsonOut, plainOut, actionOutput{})
}
