package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/agisilaos/homepodctl/internal/native"
)

func TestCmdTUIParsesOptionsBeforeLaunching(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	origTerminal := tuiTerminalAvailable
	origRunner := runTUIProgram
	origLoad := loadConfigOptional
	t.Cleanup(func() {
		tuiTerminalAvailable = origTerminal
		runTUIProgram = origRunner
		loadConfigOptional = origLoad
	})

	tuiTerminalAvailable = func() bool { return true }
	loadConfigOptional = func() (*native.Config, error) {
		cfg := &native.Config{}
		cfg.Defaults.Backend = "native"
		return cfg, nil
	}
	called := false
	runTUIProgram = func(_ context.Context, service tuiPlaybackService, opts tuiOptions) error {
		called = true
		if service != playbackApp || opts.refresh != 750*time.Millisecond || !opts.nativeDefault || !opts.noColor {
			t.Fatalf("service=%T opts=%+v", service, opts)
		}
		return nil
	}

	_, recovered := captureStdoutAndRecover(t, func() {
		cmdTUI(context.Background(), []string{"--refresh", "750ms"})
	})
	if recovered != nil || !called {
		t.Fatalf("recovered=%v called=%t", recovered, called)
	}
}

func TestCmdTUIRejectsInvalidRefreshBeforeConfig(t *testing.T) {
	origLoad := loadConfigOptional
	t.Cleanup(func() { loadConfigOptional = origLoad })
	loaded := false
	loadConfigOptional = func() (*native.Config, error) {
		loaded = true
		return &native.Config{}, nil
	}

	_, recovered := captureStdoutAndRecover(t, func() {
		cmdTUI(context.Background(), []string{"--refresh", "100ms"})
	})
	fatal, ok := recovered.(cliFatal)
	if !ok || classifyExitCode(fatal.err) != exitUsage || !strings.Contains(fatal.err.Error(), "at least 500ms") {
		t.Fatalf("recovered=%v", recovered)
	}
	if loaded {
		t.Fatal("invalid arguments loaded config")
	}
}

func TestCmdTUIRequiresInteractiveTerminal(t *testing.T) {
	origTerminal := tuiTerminalAvailable
	t.Cleanup(func() { tuiTerminalAvailable = origTerminal })
	tuiTerminalAvailable = func() bool { return false }

	_, recovered := captureStdoutAndRecover(t, func() {
		cmdTUI(context.Background(), nil)
	})
	fatal, ok := recovered.(cliFatal)
	if !ok || classifyExitCode(fatal.err) != exitUsage || !strings.Contains(fatal.err.Error(), "interactive stdin and stdout") {
		t.Fatalf("recovered=%v", recovered)
	}
}
