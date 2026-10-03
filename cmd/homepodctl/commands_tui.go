package main

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	defaultTUIRefresh = 2 * time.Second
	minimumTUIRefresh = 500 * time.Millisecond
	tuiActionTimeout  = 5 * time.Second
)

type tuiOptions struct {
	refresh       time.Duration
	actionTimeout time.Duration
	nativeDefault bool
	verbose       bool
	quiet         bool
	noColor       bool
}

var (
	tuiTerminalAvailable = func() bool {
		stdin, stdinErr := os.Stdin.Stat()
		stdout, stdoutErr := os.Stdout.Stat()
		return stdinErr == nil && stdoutErr == nil && stdin.Mode()&os.ModeCharDevice != 0 && stdout.Mode()&os.ModeCharDevice != 0
	}
	runTUIProgram = runTUI
)

func cmdTUI(ctx context.Context, args []string) {
	flags, positionals, err := parseArgs("tui", args)
	if err != nil {
		die(err)
	}
	if len(positionals) != 0 {
		die(usageErrf("usage: homepodctl tui [--refresh <duration>]"))
	}

	refresh := defaultTUIRefresh
	if value := flags.string("refresh"); value != "" {
		refresh, err = time.ParseDuration(value)
		if err != nil {
			die(usageErrf("invalid --refresh %q (expected duration like 1s)", value))
		}
		if refresh < minimumTUIRefresh {
			die(usageErrf("--refresh must be at least %s", minimumTUIRefresh))
		}
	}
	if !tuiTerminalAvailable() {
		die(usageErrf("tui requires interactive stdin and stdout"))
	}

	cfg, err := loadConfigOptional()
	if err != nil {
		die(err)
	}
	_, noColor := os.LookupEnv("NO_COLOR")
	opts := tuiOptions{
		refresh:       refresh,
		actionTimeout: tuiActionTimeout,
		nativeDefault: cfg.Defaults.Backend == "native",
		verbose:       verbose,
		quiet:         quiet,
		noColor:       noColor,
	}
	if err := runTUIProgram(ctx, playbackApp, opts); err != nil {
		die(fmt.Errorf("run tui: %w", err))
	}
}

func runTUI(ctx context.Context, service tuiPlaybackService, opts tuiOptions) error {
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	model := newTUIModel(sessionCtx, service, opts)
	program := tea.NewProgram(model, tea.WithContext(sessionCtx), tea.WithAltScreen())
	_, err := program.Run()
	return err
}
