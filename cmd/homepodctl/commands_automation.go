package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/agisilaos/homepodctl/internal/native"
	"gopkg.in/yaml.v3"
)

type automationFile struct {
	Version  string             `json:"version" yaml:"version"`
	Name     string             `json:"name" yaml:"name"`
	Defaults automationDefaults `json:"defaults" yaml:"defaults"`
	Steps    []automationStep   `json:"steps" yaml:"steps"`
}

type automationDefaults struct {
	Backend string   `json:"backend,omitempty" yaml:"backend,omitempty"`
	Rooms   []string `json:"rooms,omitempty" yaml:"rooms,omitempty"`
	Volume  *int     `json:"volume,omitempty" yaml:"volume,omitempty"`
	Shuffle *bool    `json:"shuffle,omitempty" yaml:"shuffle,omitempty"`
}

type automationStep struct {
	Type       string   `json:"type" yaml:"type"`
	Rooms      []string `json:"rooms,omitempty" yaml:"rooms,omitempty"`
	Query      string   `json:"query,omitempty" yaml:"query,omitempty"`
	PlaylistID string   `json:"playlistId,omitempty" yaml:"playlistId,omitempty"`
	Value      *int     `json:"value,omitempty" yaml:"value,omitempty"`
	State      string   `json:"state,omitempty" yaml:"state,omitempty"`
	Timeout    string   `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Action     string   `json:"action,omitempty" yaml:"action,omitempty"`
}

type automationStepResult struct {
	Index      int            `json:"index"`
	Type       string         `json:"type"`
	Input      automationStep `json:"input"`
	Resolved   any            `json:"resolved,omitempty"`
	OK         bool           `json:"ok"`
	Skipped    bool           `json:"skipped"`
	Error      string         `json:"error,omitempty"`
	DurationMS int64          `json:"durationMs"`
}

type automationCommandResult struct {
	Name       string                 `json:"name"`
	Version    string                 `json:"version"`
	Mode       string                 `json:"mode"`
	OK         bool                   `json:"ok"`
	StartedAt  string                 `json:"startedAt"`
	EndedAt    string                 `json:"endedAt"`
	DurationMS int64                  `json:"durationMs"`
	Steps      []automationStepResult `json:"steps"`
}

type automationInitResult struct {
	Preset  string `json:"preset"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

func cmdAutomation(ctx context.Context, cfg *native.Config, args []string) {
	if len(args) == 0 {
		die(usageErrf("usage: homepodctl automation <run|validate|plan|init> [args]"))
	}
	switch args[0] {
	case "run":
		cmdAutomationRun(ctx, cfg, args[1:])
	case "validate":
		cmdAutomationValidate(cfg, args[1:])
	case "plan":
		cmdAutomationPlan(cfg, args[1:])
	case "init":
		cmdAutomationInit(args[1:])
	default:
		die(usageErrf("unknown automation subcommand: %q", args[0]))
	}
}

func cmdAutomationRun(ctx context.Context, cfg *native.Config, args []string) {
	fs := flag.NewFlagSet("automation run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	filePath := fs.String("file", "", "automation file path or - for stdin")
	fs.StringVar(filePath, "f", "", "automation file path or - for stdin")
	dryRun := fs.Bool("dry-run", false, "resolve and print without executing")
	jsonOut := fs.Bool("json", false, "output JSON")
	noInput := fs.Bool("no-input", false, "disable prompts (no-op: automation is non-interactive by default)")
	if err := fs.Parse(args); err != nil {
		die(usageErrf("usage: homepodctl automation run -f <file|-> [--dry-run] [--json] [--no-input]"))
	}
	if strings.TrimSpace(*filePath) == "" {
		die(usageErrf("--file is required"))
	}
	doc, err := loadAutomationFile(*filePath)
	if err != nil {
		die(err)
	}
	if err := validateAutomation(doc); err != nil {
		die(err)
	}

	mode := "run"
	steps := resolveAutomationSteps(cfg, doc)
	if *dryRun {
		mode = "dry-run"
		result := buildAutomationResult(mode, doc, steps)
		emitAutomationResult(result, *jsonOut)
		return
	}
	_ = noInput // accepted for compatibility; automation runs are non-interactive.
	// automation runs can include waits; use a longer timeout than one-off commands.
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	executed, ok := executeAutomationSteps(runCtx, cfg, doc)
	result := buildAutomationResult(mode, doc, executed)
	result.OK = ok
	emitAutomationResult(result, *jsonOut)
	if !result.OK {
		os.Exit(exitGeneric)
	}
}

func cmdAutomationValidate(_ *native.Config, args []string) {
	fs := flag.NewFlagSet("automation validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	filePath := fs.String("file", "", "automation file path or - for stdin")
	fs.StringVar(filePath, "f", "", "automation file path or - for stdin")
	jsonOut := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		die(usageErrf("usage: homepodctl automation validate -f <file|-> [--json]"))
	}
	if strings.TrimSpace(*filePath) == "" {
		die(usageErrf("--file is required"))
	}
	doc, err := loadAutomationFile(*filePath)
	if err != nil {
		die(err)
	}
	if err := validateAutomation(doc); err != nil {
		die(err)
	}
	result := buildAutomationResult("validate", doc, resolveAutomationSteps(nil, doc))
	emitAutomationResult(result, *jsonOut)
}

func cmdAutomationPlan(cfg *native.Config, args []string) {
	fs := flag.NewFlagSet("automation plan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	filePath := fs.String("file", "", "automation file path or - for stdin")
	fs.StringVar(filePath, "f", "", "automation file path or - for stdin")
	jsonOut := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		die(usageErrf("usage: homepodctl automation plan -f <file|-> [--json]"))
	}
	if strings.TrimSpace(*filePath) == "" {
		die(usageErrf("--file is required"))
	}
	doc, err := loadAutomationFile(*filePath)
	if err != nil {
		die(err)
	}
	if err := validateAutomation(doc); err != nil {
		die(err)
	}
	result := buildAutomationResult("plan", doc, resolveAutomationSteps(cfg, doc))
	emitAutomationResult(result, *jsonOut)
}

func cmdAutomationInit(args []string) {
	fs := flag.NewFlagSet("automation init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	preset := fs.String("preset", "", "preset name: morning|focus|winddown|party|reset")
	name := fs.String("name", "", "override routine name")
	jsonOut := fs.Bool("json", false, "output JSON metadata")
	if err := fs.Parse(args); err != nil {
		die(usageErrf("usage: homepodctl automation init --preset <name> [--name <string>] [--json]"))
	}
	if strings.TrimSpace(*preset) == "" {
		die(usageErrf("--preset is required"))
	}
	doc, err := automationPreset(*preset)
	if err != nil {
		die(err)
	}
	if strings.TrimSpace(*name) != "" {
		doc.Name = strings.TrimSpace(*name)
	}
	b, err := yaml.Marshal(doc)
	if err != nil {
		die(fmt.Errorf("encode preset: %w", err))
	}
	if *jsonOut {
		writeJSON(automationInitResult{Preset: strings.TrimSpace(*preset), Name: doc.Name, Content: string(b)})
		return
	}
	fmt.Print(string(b))
}

func buildAutomationResult(mode string, doc *automationFile, steps []automationStepResult) automationCommandResult {
	started := time.Now().UTC()
	ended := started
	return automationCommandResult{
		Name:       doc.Name,
		Version:    doc.Version,
		Mode:       mode,
		OK:         true,
		StartedAt:  started.Format(time.RFC3339),
		EndedAt:    ended.Format(time.RFC3339),
		DurationMS: ended.Sub(started).Milliseconds(),
		Steps:      steps,
	}
}

func emitAutomationResult(result automationCommandResult, jsonOut bool) {
	if jsonOut {
		writeJSON(result)
		return
	}
	fmt.Printf("automation name=%q mode=%s ok=%t steps=%d\n", result.Name, result.Mode, result.OK, len(result.Steps))
	for _, st := range result.Steps {
		fmt.Printf("%d/%d %s ok=%t\n", st.Index+1, len(result.Steps), st.Type, st.OK)
	}
}

func automationPreset(name string) (automationFile, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "morning":
		return automationFile{
			Version:  "1",
			Name:     "morning",
			Defaults: automationDefaults{Backend: "airplay", Rooms: []string{"Bedroom"}, Volume: intPtr(30), Shuffle: boolPtr(false)},
			Steps:    []automationStep{{Type: "out.set", Rooms: []string{"Bedroom"}}, {Type: "play", Query: "Morning Mix"}, {Type: "volume.set", Value: intPtr(30)}, {Type: "wait", State: "playing", Timeout: "20s"}},
		}, nil
	case "focus":
		return automationFile{
			Version:  "1",
			Name:     "focus",
			Defaults: automationDefaults{Backend: "airplay", Rooms: []string{"Office"}, Volume: intPtr(25), Shuffle: boolPtr(false)},
			Steps:    []automationStep{{Type: "out.set", Rooms: []string{"Office"}}, {Type: "play", Query: "Deep Focus"}, {Type: "volume.set", Value: intPtr(25)}, {Type: "wait", State: "playing", Timeout: "20s"}},
		}, nil
	case "winddown":
		return automationFile{
			Version:  "1",
			Name:     "winddown",
			Defaults: automationDefaults{Backend: "airplay", Rooms: []string{"Bedroom"}, Volume: intPtr(20), Shuffle: boolPtr(false)},
			Steps:    []automationStep{{Type: "out.set", Rooms: []string{"Bedroom"}}, {Type: "play", Query: "Evening Ambient"}, {Type: "volume.set", Value: intPtr(20)}, {Type: "wait", State: "playing", Timeout: "20s"}},
		}, nil
	case "party":
		return automationFile{
			Version:  "1",
			Name:     "party",
			Defaults: automationDefaults{Backend: "airplay", Rooms: []string{"Living Room", "Kitchen"}, Volume: intPtr(55), Shuffle: boolPtr(true)},
			Steps:    []automationStep{{Type: "out.set", Rooms: []string{"Living Room", "Kitchen"}}, {Type: "play", Query: "Party Mix"}, {Type: "volume.set", Value: intPtr(55)}, {Type: "wait", State: "playing", Timeout: "30s"}},
		}, nil
	case "reset":
		return automationFile{
			Version:  "1",
			Name:     "reset",
			Defaults: automationDefaults{Backend: "airplay", Rooms: []string{"Bedroom"}, Volume: intPtr(25)},
			Steps:    []automationStep{{Type: "transport", Action: "stop"}, {Type: "out.set", Rooms: []string{"Bedroom"}}, {Type: "volume.set", Value: intPtr(25)}},
		}, nil
	default:
		return automationFile{}, usageErrf("unknown preset %q (expected morning, focus, winddown, party, reset)", name)
	}
}

func intPtr(v int) *int { return &v }

func boolPtr(v bool) *bool { return &v }
