package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/agisilaos/homepodctl/internal/native"
)

func TestWantsJSONErrors(t *testing.T) {
	t.Parallel()

	if !wantsJSONErrors([]string{"status", "--json"}) {
		t.Fatalf("expected --json to enable JSON errors")
	}
	if !wantsJSONErrors([]string{"status", "--json=true"}) {
		t.Fatalf("expected --json=true to enable JSON errors")
	}
	if wantsJSONErrors([]string{"status", "--json=false"}) {
		t.Fatalf("did not expect --json=false to enable JSON errors")
	}
}

func TestClassifyErrorCode(t *testing.T) {
	t.Parallel()

	if got := classifyErrorCode(usageErrf("bad")); got != "USAGE_ERROR" {
		t.Fatalf("usage code=%q", got)
	}
	if got := classifyErrorCode(&native.ConfigError{Op: "read", Err: errors.New("x")}); got != "CONFIG_ERROR" {
		t.Fatalf("config code=%q", got)
	}
	if got := classifyErrorCode(automationValidationErrf("bad automation")); got != "AUTOMATION_VALIDATION_ERROR" {
		t.Fatalf("automation code=%q", got)
	}
}

func TestEnvTruthy(t *testing.T) {
	t.Parallel()

	if !envTruthy(" yes ") {
		t.Fatalf("expected yes to be truthy")
	}
	if envTruthy("0") {
		t.Fatalf("expected 0 to be falsy")
	}
}

func TestParseGlobalOptions_MissingCommandAfterDoubleDash(t *testing.T) {
	t.Parallel()

	_, _, _, err := parseGlobalOptions([]string{"--"})
	if err == nil || !strings.Contains(err.Error(), "missing command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParsePlanArgsAndNormalizeTarget(t *testing.T) {
	t.Parallel()

	jsonOut, pos, err := parsePlanArgs([]string{"play", "focus", "--json=true"})
	if err != nil {
		t.Fatalf("parsePlanArgs: %v", err)
	}
	if !jsonOut || len(pos) != 2 {
		t.Fatalf("jsonOut=%v pos=%v", jsonOut, pos)
	}
	cmd, args, err := normalizePlanTarget(pos[0], pos[1:])
	if err != nil {
		t.Fatalf("normalizePlanTarget: %v", err)
	}
	if cmd != "play" || !hasLongFlag(args, "dry-run") || !hasLongFlag(args, "json") {
		t.Fatalf("cmd=%q args=%v", cmd, args)
	}
}

func TestParsePlanArgs_InvalidJSONBool(t *testing.T) {
	t.Parallel()

	_, _, err := parsePlanArgs([]string{"run", "--json=maybe"})
	if err == nil {
		t.Fatalf("expected invalid bool error")
	}
}

func TestAnyHelpers(t *testing.T) {
	t.Parallel()

	objs := anyObjects([]any{map[string]any{"a": 1}, "skip"})
	if len(objs) != 1 {
		t.Fatalf("objs=%v", objs)
	}
	ss := anyStrings([]any{"Bedroom", " ", 1})
	if len(ss) != 1 || ss[0] != "Bedroom" {
		t.Fatalf("strings=%v", ss)
	}
}

func TestPrintDoctorReportPlain(t *testing.T) {
	out := captureStdout(t, func() {
		printDoctorReport(doctorReport{
			OK: true,
			Checks: []doctorCheck{
				{Name: "osascript", Status: "pass", Message: "ok"},
			},
		}, true)
	})
	if !strings.Contains(out, "STATUS\tCHECK\tMESSAGE\tTIP") || !strings.Contains(out, "pass\tosascript\tok") {
		t.Fatalf("unexpected plain report: %q", out)
	}
}

func TestReadAutomationInputFromStdin(t *testing.T) {
	orig := os.Stdin
	defer func() { os.Stdin = orig }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	if _, err := io.WriteString(w, "version: \"1\"\nname: stdin\nsteps:\n  - type: transport\n    action: stop\n"); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	os.Stdin = r

	b, err := readAutomationInput("-")
	if err != nil {
		t.Fatalf("readAutomationInput: %v", err)
	}
	if !strings.Contains(string(b), "name: stdin") {
		t.Fatalf("unexpected stdin content: %s", string(b))
	}
}
