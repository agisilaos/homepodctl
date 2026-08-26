package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

type schemaEnvelope struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
}

type schemaIndex struct {
	Schemas []string `json:"schemas"`
}

type planResponse struct {
	OK      bool           `json:"ok"`
	Command string         `json:"command"`
	Args    []string       `json:"args"`
	Plan    map[string]any `json:"plan"`
}

func cmdSchema(args []string) {
	flags, pos, err := parseArgs(args)
	if err != nil {
		die(err)
	}
	jsonOut, _, err := parseOutputFlags(flags)
	if err != nil {
		die(err)
	}
	if len(pos) > 1 {
		die(usageErrf("usage: homepodctl schema [<name>] [--json]"))
	}

	names := make([]string, 0, len(cliSchemas))
	for name := range cliSchemas {
		names = append(names, name)
	}
	sort.Strings(names)

	if len(pos) == 0 {
		if jsonOut {
			writeJSON(schemaIndex{Schemas: names})
			return
		}
		fmt.Println("Available schemas:")
		for _, name := range names {
			fmt.Printf("- %s\n", name)
		}
		return
	}

	name := strings.TrimSpace(pos[0])
	schema, ok := cliSchemas[name]
	if !ok {
		die(usageErrf("unknown schema %q", name))
	}
	if jsonOut {
		writeJSON(schemaEnvelope{Name: name, Schema: schema})
		return
	}
	fmt.Printf("schema=%s\n", name)
	writeJSON(schema)
}

func cmdPlan(args []string) {
	jsonOut, pos, err := parsePlanArgs(args)
	if err != nil {
		die(err)
	}
	if len(pos) < 1 {
		die(usageErrf("usage: homepodctl plan <run|play|volume|vol|native-run|out set|automation run> [args] [--json]"))
	}

	targetCmd, targetArgs, err := normalizePlanTarget(pos[0], pos[1:])
	if err != nil {
		die(err)
	}
	payload, err := runPlanTarget(targetCmd, targetArgs)
	if err != nil {
		die(err)
	}

	resp := planResponse{OK: true, Command: targetCmd, Args: targetArgs, Plan: payload}
	if jsonOut {
		writeJSON(resp)
		return
	}
	printPlanResponse(resp)
}

func parsePlanArgs(args []string) (bool, []string, error) {
	jsonOut := false
	pos := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			if len(pos) == 0 {
				pos = append(pos, args[i+1:]...)
			} else {
				pos = append(pos, args[i:]...)
			}
			break
		}
		if a == "-h" || a == "--help" {
			return false, nil, usageErrf("usage: homepodctl plan <run|play|volume|vol|native-run|out set|automation run> [args] [--json]")
		}
		if a == "--json" {
			jsonOut = true
			if i+1 < len(args) {
				if value, ok := parseBoolWord(args[i+1]); ok {
					i++
					jsonOut = value
				}
			}
			continue
		}
		if strings.HasPrefix(a, "--json=") {
			v := strings.TrimSpace(strings.TrimPrefix(a, "--json="))
			b, ok := parseBoolWord(v)
			if !ok {
				var err error
				b, err = strconv.ParseBool(v)
				if err != nil {
					return false, nil, usageErrf("invalid boolean for --json: %q", v)
				}
			}
			jsonOut = b
			continue
		}
		pos = append(pos, a)
	}
	return jsonOut, pos, nil
}
func normalizePlanTarget(cmd string, args []string) (string, []string, error) {
	prefixLen := 0
	switch cmd {
	case "run", "play", "volume", "vol", "native-run":
	case "out":
		if len(args) == 0 || strings.TrimSpace(args[0]) != "set" {
			return "", nil, usageErrf("plan only supports `out set` (usage: homepodctl plan out set --room <name> ...)")
		}
		prefixLen = 1
	case "automation":
		if len(args) == 0 || strings.TrimSpace(args[0]) != "run" {
			return "", nil, usageErrf("plan only supports `automation run` (usage: homepodctl plan automation run -f <file>)")
		}
		prefixLen = 1
	default:
		return "", nil, usageErrf("plan only supports run, play, volume, vol, native-run, out set, and automation run")
	}
	targetArgs, err := canonicalPlanTargetArgs(args, prefixLen)
	return cmd, targetArgs, err
}

// plan owns the child dry-run and JSON flags. Keep them ahead of user arguments
// because target parsers handle positionals differently, while preserving a
// target delimiter and its literal suffix exactly.
func canonicalPlanTargetArgs(args []string, prefixLen int) ([]string, error) {
	targetArgs := make([]string, 0, len(args)+2)
	targetArgs = append(targetArgs, args[:prefixLen]...)
	targetArgs = append(targetArgs, "--dry-run=true", "--json=true")

	for i := prefixLen; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return append(targetArgs, args[i:]...), nil
		}

		name, value, hasValue := splitPlanTargetFlag(arg)
		if name == "" {
			targetArgs = append(targetArgs, arg)
			continue
		}
		if hasValue {
			if _, ok := parseBoolWord(value); !ok {
				return nil, usageErrf("invalid boolean for --%s: %q", name, strings.TrimSpace(value))
			}
			continue
		}
		if i+1 < len(args) {
			if _, ok := parseBoolWord(args[i+1]); ok {
				i++
			}
		}
	}
	return targetArgs, nil
}

func splitPlanTargetFlag(arg string) (name string, value string, hasValue bool) {
	const (
		dryRunFlag = "--dry-run"
		jsonFlag   = "--json"
	)

	switch {
	case arg == dryRunFlag:
		return "dry-run", "", false
	case strings.HasPrefix(arg, dryRunFlag+"="):
		return "dry-run", strings.TrimPrefix(arg, dryRunFlag+"="), true
	case arg == jsonFlag:
		return "json", "", false
	case strings.HasPrefix(arg, jsonFlag+"="):
		return "json", strings.TrimPrefix(arg, jsonFlag+"="), true
	default:
		return "", "", false
	}
}

func runPlanTarget(cmd string, args []string) (map[string]any, error) {
	childArgs := append([]string{cmd}, args...)
	child := exec.Command(os.Args[0], childArgs...)
	child.Env = os.Environ()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	child.Stdout = &stdout
	child.Stderr = &stderr

	if err := child.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("plan target failed: %s", msg)
	}

	out := bytes.TrimSpace(stdout.Bytes())
	if len(out) == 0 {
		return nil, errors.New("plan target returned empty output")
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("plan target did not return valid JSON: %w", err)
	}
	return payload, nil
}

func printPlanResponse(resp planResponse) {
	if resp.Command == "automation" {
		name, _ := resp.Plan["name"].(string)
		mode, _ := resp.Plan["mode"].(string)
		ok, _ := resp.Plan["ok"].(bool)
		steps := anyObjects(resp.Plan["steps"])
		fmt.Printf("plan command=automation name=%q mode=%s ok=%t steps=%d\n", name, mode, ok, len(steps))
		return
	}
	action, _ := resp.Plan["action"].(string)
	backend, _ := resp.Plan["backend"].(string)
	playlist, _ := resp.Plan["playlist"].(string)
	playlistID, _ := resp.Plan["playlistId"].(string)
	shortcut, _ := resp.Plan["shortcut"].(string)
	rooms := anyStrings(resp.Plan["rooms"])
	fmt.Printf("plan command=%s action=%s backend=%s dry_run=true rooms=%s playlist=%q playlist_id=%q shortcut=%q\n",
		resp.Command,
		action,
		backend,
		strings.Join(rooms, ","),
		playlist,
		playlistID,
		shortcut,
	)
}

func anyObjects(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if ok {
			out = append(out, m)
		}
	}
	return out
}

func anyStrings(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

var cliSchemas = map[string]map[string]any{
	"action-result": {
		"$schema":  "https://json-schema.org/draft/2020-12/schema",
		"type":     "object",
		"required": []any{"ok", "action"},
		"properties": map[string]any{
			"ok":         map[string]any{"type": "boolean"},
			"action":     map[string]any{"type": "string"},
			"dryRun":     map[string]any{"type": "boolean"},
			"backend":    map[string]any{"type": "string"},
			"rooms":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"playlist":   map[string]any{"type": "string"},
			"playlistId": map[string]any{"type": "string"},
			"shortcut":   map[string]any{"type": "string"},
			"nowPlaying": map[string]any{"type": "object"},
		},
	},
	"error-response": {
		"$schema":  "https://json-schema.org/draft/2020-12/schema",
		"type":     "object",
		"required": []any{"ok", "error"},
		"properties": map[string]any{
			"ok": map[string]any{"const": false},
			"error": map[string]any{
				"type":     "object",
				"required": []any{"code", "message", "exitCode"},
				"properties": map[string]any{
					"code":     map[string]any{"type": "string"},
					"message":  map[string]any{"type": "string"},
					"exitCode": map[string]any{"type": "integer"},
				},
			},
		},
	},
	"automation-result": {
		"$schema":  "https://json-schema.org/draft/2020-12/schema",
		"type":     "object",
		"required": []any{"name", "version", "mode", "ok", "steps"},
		"properties": map[string]any{
			"name":       map[string]any{"type": "string"},
			"version":    map[string]any{"type": "string"},
			"mode":       map[string]any{"type": "string"},
			"ok":         map[string]any{"type": "boolean"},
			"startedAt":  map[string]any{"type": "string"},
			"endedAt":    map[string]any{"type": "string"},
			"durationMs": map[string]any{"type": "integer"},
			"steps":      map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		},
	},
	"plan-response": {
		"$schema":  "https://json-schema.org/draft/2020-12/schema",
		"type":     "object",
		"required": []any{"ok", "command", "args", "plan"},
		"properties": map[string]any{
			"ok":      map[string]any{"const": true},
			"command": map[string]any{"type": "string"},
			"args":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"plan":    map[string]any{"type": "object"},
		},
	},
}
