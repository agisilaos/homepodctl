package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

type setupResult struct {
	OK            bool                  `json:"ok"`
	ConfigPath    string                `json:"configPath"`
	ConfigUpdated bool                  `json:"configUpdated"`
	Defaults      native.DefaultsConfig `json:"defaults"`
	Doctor        doctorReport          `json:"doctor"`
	Devices       []music.AirPlayDevice `json:"devices,omitempty"`
	DeviceError   string                `json:"deviceError,omitempty"`
	Next          []string              `json:"next"`
}

func cmdSetup(ctx context.Context, args []string) {
	flags, positionals, err := parseArgs(args)
	if err != nil {
		die(usageErrf("usage: homepodctl setup [--backend airplay|native] [--room <name> ...] [--json] [--no-input]"))
	}
	if len(positionals) != 0 {
		die(usageErrf("usage: homepodctl setup [--backend airplay|native] [--room <name> ...] [--json] [--no-input]"))
	}
	jsonOut, _, err := flags.boolStrict("json")
	if err != nil {
		die(err)
	}
	if _, _, err := flags.boolStrict("no-input"); err != nil {
		die(err)
	}

	path, err := initConfig()
	if err != nil {
		die(err)
	}
	cfg, err := loadConfigOptional()
	if err != nil {
		die(err)
	}

	configUpdated := false
	if backend := strings.TrimSpace(flags.string("backend")); backend != "" {
		if backend != "airplay" && backend != "native" {
			die(usageErrf("unknown backend: %q", backend))
		}
		cfg.Defaults.Backend = backend
		configUpdated = true
	}
	if rooms := flags.strings("room"); len(rooms) > 0 {
		cfg.Defaults.Rooms = append([]string(nil), rooms...)
		configUpdated = true
	}
	if issues := validateConfigValues(cfg); len(issues) > 0 {
		die(usageErrf("setup produced invalid config: %s", strings.Join(issues, "; ")))
	}
	if configUpdated {
		if err := saveConfig(cfg); err != nil {
			die(err)
		}
	}

	doctor := runDoctorChecks(ctx)
	devices, devErr := listAirPlayDevices(ctx)
	if devErr == nil {
		for i := range devices {
			devices[i].NetworkAddress = ""
		}
	}

	res := setupResult{
		OK:            doctor.OK && devErr == nil,
		ConfigPath:    path,
		ConfigUpdated: configUpdated,
		Defaults:      cfg.Defaults,
		Doctor:        doctor,
		Devices:       devices,
		Next:          setupNextSteps(cfg),
	}
	if devErr != nil {
		res.DeviceError = formatError(devErr)
	}

	if jsonOut {
		writeJSON(res)
		return
	}
	if quiet {
		return
	}
	fmt.Printf("setup ok=%t config=%s updated=%t\n", res.OK, res.ConfigPath, res.ConfigUpdated)
	printDoctorReport(doctor, false)
	if devErr != nil {
		fmt.Printf("devices error=%q\n", res.DeviceError)
	} else {
		printDevicesTable(os.Stdout, devices, false)
	}
	fmt.Println("next:")
	for _, step := range res.Next {
		fmt.Printf("- %s\n", step)
	}
}

func saveConfig(cfg *native.Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	return nil
}

func setupNextSteps(cfg *native.Config) []string {
	steps := []string{
		"homepodctl status",
		"homepodctl devices",
	}
	if len(cfg.Defaults.Rooms) > 0 {
		steps = append(steps, "homepodctl play chill")
	} else {
		steps = append(steps, "homepodctl out set --room \"Bedroom\"")
	}
	steps = append(steps, "homepodctl doctor --json")
	return steps
}
