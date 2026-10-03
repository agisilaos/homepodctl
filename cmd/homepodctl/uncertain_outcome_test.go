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
