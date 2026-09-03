package main

import (
	"context"
	"fmt"
	"os"
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

type setupOptions struct {
	backend string
	rooms   []string
	jsonOut bool
}

func parseSetupOptions(args []string) (setupOptions, error) {
	flags, positionals, err := parseArgs("setup", args)
	if err != nil {
		return setupOptions{}, err
	}
	if len(positionals) != 0 {
		return setupOptions{}, usageErrf("usage: homepodctl setup [--backend airplay|native] [--room <name> ...] [--json] [--no-input]")
	}
	jsonOut, _, err := flags.boolStrict("json")
	if err != nil {
		return setupOptions{}, err
	}
	if _, _, err := flags.boolStrict("no-input"); err != nil {
		return setupOptions{}, err
	}
	opts := setupOptions{
		backend: strings.TrimSpace(flags.string("backend")),
		rooms:   flags.strings("room"),
		jsonOut: jsonOut,
	}
	if opts.backend != "" && opts.backend != "airplay" && opts.backend != "native" {
		return setupOptions{}, usageErrf("unknown backend: %q", opts.backend)
	}
	var issues []string
	for i, room := range opts.rooms {
		if strings.TrimSpace(room) == "" {
			issues = append(issues, fmt.Sprintf("defaults.rooms[%d] must be non-empty", i))
		}
	}
	if len(issues) > 0 {
		return setupOptions{}, usageErrf("setup produced invalid config: %s", strings.Join(issues, "; "))
	}
	return opts, nil
}

func cmdSetup(ctx context.Context, args []string) {
	opts, err := parseSetupOptions(args)
	if err != nil {
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
	if opts.backend != "" {
		cfg.Defaults.Backend = opts.backend
		configUpdated = true
	}
	if len(opts.rooms) > 0 {
		cfg.Defaults.Rooms = append([]string(nil), opts.rooms...)
		configUpdated = true
	}
	if issues := validateConfigValues(cfg); len(issues) > 0 {
		die(usageErrf("setup produced invalid config: %s", strings.Join(issues, "; ")))
	}
	if configUpdated {
		if err := native.SaveConfig(cfg); err != nil {
			die(err)
		}
	}

	doctor := runDoctorChecks(ctx)
	devices, devErr := playbackApp.Devices(ctx)
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

	if opts.jsonOut {
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
