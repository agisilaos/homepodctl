package main

import (
	"context"
	"errors"
	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
	"strings"
	"testing"
)

func TestUncertainOutcomeRendering(t *testing.T) {
	for _, err := range []error{&music.ScriptError{Err: context.DeadlineExceeded, Uncertain: true}, &native.ShortcutError{Err: errors.New("exit"), Uncertain: true}} {
		if classifyExitCode(err) != exitBackend || classifyErrorCode(err) != "OUTCOME_UNCERTAIN" {
			t.Fatalf("incorrect classification: %v", err)
		}
		if !strings.Contains(formatError(err), "before retrying") || !strings.Contains(tuiErrorText(err, false), "before retrying") {
			t.Fatalf("missing recovery guidance: %v", err)
		}
	}
}

func TestUncertainTUIRespectsItsVerbosity(t *testing.T) {
	previous := verbose
	t.Cleanup(func() { verbose = previous })
	err := &music.ScriptError{Err: errors.New("backend detail"), Uncertain: true}
	verbose = true
	if got := tuiErrorText(err, false); strings.Contains(got, "backend detail") {
		t.Fatalf("verbose detail leaked: %s", got)
	}
	verbose = false
	if got := tuiErrorText(err, true); !strings.Contains(got, "backend detail") {
		t.Fatalf("verbose detail missing: %s", got)
	}
}
