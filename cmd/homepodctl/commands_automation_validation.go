package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func loadAutomationFile(path string) (*automationFile, error) {
	b, err := readAutomationInput(path)
	if err != nil {
		return nil, err
	}
	doc, err := parseAutomationBytes(b)
	if err != nil {
		return nil, err
	}
	return doc, nil
}

func readAutomationInput(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return b, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read automation file %q: %w", path, err)
	}
	return b, nil
}

func parseAutomationBytes(b []byte) (*automationFile, error) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil, automationValidationErrf("automation file is empty")
	}
	var doc automationFile
	if b[0] == '{' {
		if err := json.Unmarshal(b, &doc); err != nil {
			return nil, automationValidationErrf("invalid automation JSON: %v", err)
		}
		return &doc, nil
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, automationValidationErrf("invalid automation YAML: %v", err)
	}
	return &doc, nil
}

func validateAutomation(doc *automationFile) error {
	if doc == nil {
		return automationValidationErrf("automation file is required")
	}
	if strings.TrimSpace(doc.Version) != "1" {
		return automationValidationErrf("version: expected \"1\"")
	}
	if strings.TrimSpace(doc.Name) == "" {
		return automationValidationErrf("name: required")
	}
	if err := validateAutomationDefaults("defaults", doc.Defaults); err != nil {
		return err
	}
	if len(doc.Steps) == 0 {
		return automationValidationErrf("steps: must contain at least one step")
	}
	for i, st := range doc.Steps {
		if err := validateAutomationStep(i, st); err != nil {
			return err
		}
	}
	return nil
}

func validateAutomationDefaults(path string, d automationDefaults) error {
	if d.Backend != "" && d.Backend != "airplay" && d.Backend != "native" {
		return automationValidationErrf("%s.backend: expected airplay or native", path)
	}
	if d.Volume != nil && (*d.Volume < 0 || *d.Volume > 100) {
		return automationValidationErrf("%s.volume: expected 0..100", path)
	}
	for i, r := range d.Rooms {
		if strings.TrimSpace(r) == "" {
			return automationValidationErrf("%s.rooms[%d]: must be non-empty", path, i)
		}
	}
	return nil
}

func validateAutomationStep(i int, st automationStep) error {
	path := fmt.Sprintf("steps[%d]", i)
	t := strings.TrimSpace(st.Type)
	if t == "" {
		return automationValidationErrf("%s.type: required", path)
	}
	switch t {
	case "out.set":
		if len(st.Rooms) == 0 {
			return automationValidationErrf("%s.rooms: required for out.set", path)
		}
		for j, r := range st.Rooms {
			if strings.TrimSpace(r) == "" {
				return automationValidationErrf("%s.rooms[%d]: must be non-empty", path, j)
			}
		}
	case "play":
		hasQ := strings.TrimSpace(st.Query) != ""
		hasID := strings.TrimSpace(st.PlaylistID) != ""
		if hasQ == hasID {
			return automationValidationErrf("%s: play requires exactly one of query or playlistId", path)
		}
	case "volume.set":
		if st.Value == nil {
			return automationValidationErrf("%s.value: required for volume.set", path)
		}
		if *st.Value < 0 || *st.Value > 100 {
			return automationValidationErrf("%s.value: expected 0..100", path)
		}
	case "wait":
		s := strings.TrimSpace(st.State)
		if s != "playing" && s != "paused" && s != "stopped" {
			return automationValidationErrf("%s.state: expected playing|paused|stopped", path)
		}
		if strings.TrimSpace(st.Timeout) == "" {
			return automationValidationErrf("%s.timeout: required", path)
		}
		d, err := time.ParseDuration(st.Timeout)
		if err != nil {
			return automationValidationErrf("%s.timeout: invalid duration", path)
		}
		if d < time.Second || d > 10*time.Minute {
			return automationValidationErrf("%s.timeout: expected between 1s and 10m", path)
		}
	case "transport":
		if strings.TrimSpace(st.Action) != "stop" {
			return automationValidationErrf("%s.action: only \"stop\" is supported in v1", path)
		}
	default:
		return automationValidationErrf("%s.type: unsupported step type %q", path, st.Type)
	}
	return nil
}
