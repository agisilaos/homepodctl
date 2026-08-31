package main

import "strings"

type flagKind uint8

const (
	valueFlag flagKind = iota
	boolFlag
)

type commandFlagSpec struct {
	flags map[string]flagKind
	// Preserve the single-dash names, explicit empty values, and attached
	// short booleans accepted by the former flag.FlagSet handlers.
	legacySyntax bool
	// config set owns arbitrary value text after its first positional.
	literalTail bool
}

func flagSpec(values, booleans string) commandFlagSpec {
	spec := commandFlagSpec{flags: make(map[string]flagKind)}
	for _, name := range strings.Fields(values) {
		spec.flags[name] = valueFlag
	}
	for _, name := range strings.Fields(booleans) {
		spec.flags[name] = boolFlag
	}
	return spec
}

func legacyFlagSpec(values, booleans string) commandFlagSpec {
	spec := flagSpec(values, booleans)
	spec.legacySyntax = true
	return spec
}

var commandFlagSpecs = map[string]commandFlagSpec{
	"setup":               flagSpec("backend room", "json no-input"),
	"play":                flagSpec("backend room playlist playlist-id volume", "shuffle choose no-input json plain dry-run"),
	"run":                 flagSpec("", "json plain dry-run"),
	"doctor":              flagSpec("", "json plain"),
	"status":              flagSpec("watch", "json plain"),
	"pause":               flagSpec("", "json plain"),
	"volume":              flagSpec("backend room value volume", "json plain dry-run"),
	"out set":             flagSpec("backend room", "json plain dry-run"),
	"automation run":      flagSpec("file f", "json dry-run no-input"),
	"automation validate": flagSpec("file f", "json"),
	"automation plan":     flagSpec("file f", "json"),
	"automation init":     flagSpec("preset name", "json"),
	"config get":          flagSpec("", "json"),
	"schema":              flagSpec("", "json"),
	"plan":                legacyFlagSpec("", "json"),
	"devices":             legacyFlagSpec("", "json plain include-network"),
	"out list":            legacyFlagSpec("", "json plain include-network"),
	"playlists":           legacyFlagSpec("query limit", "json plain"),
	"aliases":             legacyFlagSpec("", "json plain"),
	"native-run":          legacyFlagSpec("shortcut", "json dry-run"),
	"config validate":     legacyFlagSpec("", "json"),
	"config set":          {literalTail: true, legacySyntax: true},
	"completion install":  legacyFlagSpec("path", ""),
	"completion":          flagSpec("", ""),
	"version":             flagSpec("", ""),
	"config-init":         flagSpec("", ""),
	"help":                flagSpec("", ""),
}

var commandFlagAliases = map[string]string{
	"now": "status", "stop": "pause", "next": "pause", "prev": "pause", "vol": "volume",
}

func flagsForCommand(command string) commandFlagSpec {
	if canonical, ok := commandFlagAliases[command]; ok {
		command = canonical
	}
	return commandFlagSpecs[command]
}

// A token retains how many arguments it owns, including a value that looks
// like another flag. Parsing and error-mode detection share this boundary.
type flagToken struct {
	name  string
	value string
	count int
}

func readFlag(spec commandFlagSpec, args []string) (flagToken, error) {
	arg := args[0]
	token := flagToken{count: 1}
	if arg == "-h" || arg == "--help" {
		token.name = "help"
		return token, nil
	}
	name := strings.TrimPrefix(arg, "--")
	if !strings.HasPrefix(arg, "--") {
		if spec.legacySyntax || arg == "-f" {
			name = strings.TrimPrefix(arg, "-")
		} else {
			return token, usageErrf("unknown flag: %s", arg)
		}
	}
	name, value, attached := strings.Cut(name, "=")
	kind, known := spec.flags[name]
	if !known || (name == "f" && arg != "-f") {
		return token, usageErrf("unknown flag: %s", arg)
	}
	token.name, token.value = name, value
	if kind == valueFlag {
		if !attached || (value == "" && !spec.legacySyntax) {
			if len(args) < 2 {
				return token, usageErrf("%s requires a value", arg)
			}
			token.value, token.count = args[1], 2
		}
		return token, nil
	}
	if spec.legacySyntax && attached {
		if b, ok := decodeBool(value, true); ok {
			if b {
				token.value = "true"
			} else {
				token.value = "false"
			}
			return token, nil
		}
		return token, usageErrf("invalid --%s %q (expected true/false)", name, value)
	}
	if value == "" && len(args) > 1 {
		if _, ok := parseBoolWord(args[1]); ok {
			token.value, token.count = args[1], 2
		}
	}
	if token.value == "" {
		token.value = "true"
	}
	return token, nil
}

func parseArgs(command string, args []string) (parsedArgs, []string, error) {
	spec := flagsForCommand(command)
	out := parsedArgs{kv: make(map[string][]string)}
	var positionals []string
	for i := 0; i < len(args); {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			if spec.literalTail {
				positionals = append(positionals, args[i:]...)
				break
			}
			positionals = append(positionals, arg)
			i++
			continue
		}
		token, err := readFlag(spec, args[i:])
		if err != nil {
			return parsedArgs{}, nil, usageErrf("%s: %s", command, err)
		}
		if token.name == "help" {
			usage()
			exitCode(0)
		}
		out.kv[token.name] = append(out.kv[token.name], token.value)
		i += token.count
	}
	return out, positionals, nil
}

func parseFlagOnlyArgs(command string, args []string) parsedArgs {
	flags, positionals, err := parseArgs(command, args)
	if err != nil {
		die(err)
	}
	if len(positionals) != 0 {
		die(usageErrf("%s does not accept positional arguments", command))
	}
	return flags
}

// Parent commands consume the subcommand before any of its flags.
func commandLeaf(command string, args []string) (string, []string) {
	switch command {
	case "out", "automation", "config":
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			return command + " " + args[0], args[1:]
		}
	case "completion":
		if len(args) > 0 && args[0] == "install" {
			return "completion install", args[1:]
		}
	}
	return command, args
}
