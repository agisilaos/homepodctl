package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

type jsonErrorResponse struct {
	OK    bool             `json:"ok"`
	Error jsonErrorPayload `json:"error"`
}

type jsonErrorPayload struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	ExitCode int    `json:"exitCode"`
}

func die(err error) {
	code := classifyExitCode(err)
	if verbose {
		fmt.Fprintf(os.Stderr, "debug: exit_code=%d error_type=%T\n", code, err)
	}
	if jsonErrorOut {
		enc := json.NewEncoder(os.Stderr)
		enc.SetIndent("", "  ")
		_ = enc.Encode(jsonErrorResponse{
			OK: false,
			Error: jsonErrorPayload{
				Code:     classifyErrorCode(err),
				Message:  formatError(err),
				ExitCode: code,
			},
		})
		os.Exit(code)
	}
	fmt.Fprintln(os.Stderr, "error:", formatError(err))
	os.Exit(code)
}

func wantsJSONErrors(args []string) bool {
	for _, a := range args {
		if a == "--json" {
			return true
		}
		if strings.HasPrefix(a, "--json=") {
			v := strings.TrimSpace(strings.TrimPrefix(a, "--json="))
			switch strings.ToLower(v) {
			case "1", "true", "yes", "on":
				return true
			}
		}
	}
	return false
}

func classifyErrorCode(err error) string {
	var autoValErr *automationValidationError
	if errors.As(err, &autoValErr) {
		return "AUTOMATION_VALIDATION_ERROR"
	}
	switch classifyExitCode(err) {
	case exitUsage:
		return "USAGE_ERROR"
	case exitConfig:
		return "CONFIG_ERROR"
	case exitBackend:
		return "BACKEND_ERROR"
	default:
		return "GENERIC_ERROR"
	}
}

func formatError(err error) string {
	if verbose {
		return err.Error()
	}
	var scriptErr *music.ScriptError
	if errors.As(err, &scriptErr) {
		if msg := friendlyScriptError(scriptErr.Output); msg != "" {
			return msg
		}
		return "backend command failed (Music/AppleScript). Re-run with --verbose for details."
	}
	var shortcutErr *native.ShortcutError
	if errors.As(err, &shortcutErr) {
		return "backend command failed (Shortcuts). Re-run with --verbose for details."
	}
	return err.Error()
}

func friendlyScriptError(output string) string {
	o := strings.ToLower(output)
	switch {
	case strings.Contains(o, "not authorised"), strings.Contains(o, "not authorized"), strings.Contains(o, "not permitted"):
		return "Music automation is not permitted. Grant Automation permission to your terminal/binary in System Settings."
	case strings.Contains(o, "connection invalid"):
		return "Could not connect to Music app. Open Music and retry. Use --verbose for backend details."
	case strings.Contains(o, "airplay device"):
		return "AirPlay device lookup failed. Run `homepodctl devices` and use the exact room name."
	default:
		return ""
	}
}

type usageError struct {
	msg string
}

func (e *usageError) Error() string { return e.msg }

func usageErrf(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

type automationValidationError struct {
	msg string
}

func (e *automationValidationError) Error() string { return e.msg }

func automationValidationErrf(format string, args ...any) error {
	return &automationValidationError{msg: fmt.Sprintf(format, args...)}
}

func classifyExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ue *usageError
	if errors.As(err, &ue) {
		return exitUsage
	}
	var cfgErr *native.ConfigError
	if errors.As(err, &cfgErr) {
		return exitConfig
	}
	var autoValErr *automationValidationError
	if errors.As(err, &autoValErr) {
		return exitConfig
	}
	var scriptErr *music.ScriptError
	if errors.As(err, &scriptErr) {
		return exitBackend
	}
	var shortcutErr *native.ShortcutError
	if errors.As(err, &shortcutErr) {
		return exitBackend
	}
	return exitGeneric
}

func debugf(format string, args ...any) {
	if !verbose {
		return
	}
	fmt.Fprintf(os.Stderr, "debug: "+format+"\n", args...)
}

func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
