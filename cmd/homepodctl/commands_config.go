package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/agisilaos/homepodctl/internal/native"
)

type configValidateResult struct {
	OK     bool     `json:"ok"`
	Path   string   `json:"path"`
	Errors []string `json:"errors,omitempty"`
}

func cmdConfig(args []string) {
	if len(args) == 0 {
		die(usageErrf("usage: homepodctl config <validate|get|set> [args]"))
	}
	switch args[0] {
	case "validate":
		cmdConfigValidate(args[1:])
	case "get":
		cmdConfigGet(args[1:])
	case "set":
		cmdConfigSet(args[1:])
	default:
		die(usageErrf("unknown config subcommand: %q", args[0]))
	}
}

func cmdConfigValidate(args []string) {
	flags := parseFlagOnlyArgs("config validate", args)
	jsonOut := flags.boolDefault("json", false)
	cfg, err := loadConfigOptional()
	if err != nil {
		die(err)
	}
	path, _ := configPath()
	issues := validateConfigValues(cfg)
	res := configValidateResult{
		OK:     len(issues) == 0,
		Path:   path,
		Errors: issues,
	}
	if !res.OK {
		defer preserveOutputStatus(exitConfig)
	}
	if jsonOut {
		writeJSON(res)
	} else if res.OK {
		if !quiet {
			fmt.Fprintf(checkedOutput{os.Stdout}, "config ok: %s\n", res.Path)
		}
	} else {
		fmt.Fprintf(checkedOutput{os.Stdout}, "config invalid: %s\n", res.Path)
		for _, issue := range res.Errors {
			fmt.Fprintf(checkedOutput{os.Stdout}, "- %s\n", issue)
		}
	}
	if !res.OK {
		exitCode(exitConfig)
	}
}

func cmdConfigGet(args []string) {
	flags, pos, err := parseArgs("config get", args)
	if err != nil {
		die(err)
	}
	jsonOut, _, err := parseOutputFlags(flags)
	if err != nil {
		die(err)
	}
	if len(pos) != 1 {
		die(usageErrf("usage: homepodctl config get <path> [--json]"))
	}
	key := strings.TrimSpace(pos[0])
	cfg, err := loadConfigOptional()
	if err != nil {
		die(err)
	}
	value, err := getConfigPathValue(cfg, key)
	if err != nil {
		die(err)
	}
	if jsonOut {
		writeJSON(map[string]any{"path": key, "value": value})
		return
	}
	switch v := value.(type) {
	case []string:
		fmt.Fprintln(checkedOutput{os.Stdout}, strings.Join(v, "\t"))
	default:
		fmt.Fprintf(checkedOutput{os.Stdout}, "%v\n", v)
	}
}

func cmdConfigSet(args []string) {
	_, pos, err := parseArgs("config set", args)
	if err != nil {
		die(err)
	}
	if len(pos) < 2 {
		die(usageErrf("usage: homepodctl config set <path> <value...>"))
	}
	key := strings.TrimSpace(pos[0])
	values := pos[1:]

	cfg, err := loadConfigOptional()
	if err != nil {
		die(err)
	}
	if err := setConfigPathValue(cfg, key, values); err != nil {
		die(err)
	}
	issues := validateConfigValues(cfg)
	if len(issues) > 0 {
		die(usageErrf("updated config is invalid: %s", strings.Join(issues, "; ")))
	}
	if err := native.SaveConfig(cfg); err != nil {
		die(err)
	}
	if !quiet {
		path, _ := configPath()
		fmt.Fprintf(checkedOutput{os.Stdout}, "Updated %s (%s)\n", path, key)
	}
}
