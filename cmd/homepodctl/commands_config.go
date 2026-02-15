package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	fs := flag.NewFlagSet("config validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		die(usageErrf("usage: homepodctl config validate [--json]"))
	}
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
	if *jsonOut {
		writeJSON(res)
		return
	}
	if res.OK {
		fmt.Printf("config ok: %s\n", res.Path)
		return
	}
	fmt.Printf("config invalid: %s\n", res.Path)
	for _, issue := range res.Errors {
		fmt.Printf("- %s\n", issue)
	}
	exitCode(exitUsage)
}

func cmdConfigGet(args []string) {
	flags, pos, err := parseArgs(args)
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
		fmt.Println(strings.Join(v, "\t"))
	default:
		fmt.Printf("%v\n", v)
	}
}

func cmdConfigSet(args []string) {
	fs := flag.NewFlagSet("config set", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		die(usageErrf("usage: homepodctl config set <path> <value...>"))
	}
	if fs.NArg() < 2 {
		die(usageErrf("usage: homepodctl config set <path> <value...>"))
	}
	key := strings.TrimSpace(fs.Arg(0))
	values := fs.Args()[1:]

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
	path, err := configPath()
	if err != nil {
		die(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		die(err)
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		die(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		die(err)
	}
	fmt.Printf("Updated %s (%s)\n", path, key)
}
