package main

import (
	"slices"
	"sort"
	"strings"
)

// Completion adds presentation and positional hints to the parser's flag
// specifications. Shells receive these same flat contexts; they do not own
// command, flag, or preset inventories.
type completionContext struct {
	name          string
	options       []string
	values        []string
	positionFlags []string
	booleans      []string
	terminal      []string
	children      []string
	positional    string
	start         int
	repeat        bool
	stop          bool
	legacy        bool
	literal       bool
}

type completionVocabulary struct {
	contexts []completionContext
	groups   map[string][]string
}

var completionValueGroups = map[string]string{
	"--room": "rooms", "--playlist": "playlists", "--preset": "presets", "--backend": "backends",
}

var completionDescriptions = map[string]string{
	"help": "Show help", "version": "Show version", "config": "Inspect/update config",
	"automation": "Run automation routines", "plan": "Preview command execution",
	"schema": "Show JSON schemas", "completion": "Generate shell completion",
	"setup": "Onboard and verify environment", "doctor": "Run diagnostics",
	"devices": "List devices", "out": "Manage outputs", "playlists": "List playlists",
	"status": "Show playback, route, and backend status", "now": "Alias of status",
	"tui":     "Interactive Music/AirPlay dashboard (Preview)",
	"aliases": "List aliases", "run": "Run alias", "pause": "Pause playback",
	"stop": "Stop playback", "next": "Next track", "prev": "Previous track",
	"play": "Play playlist", "volume": "Set volume", "vol": "Set volume",
	"native-run": "Run shortcut", "config-init": "Write starter config",
	"--help": "Show help", "-h": "Show help", "--version": "Show version",
	"--verbose": "Verbose diagnostics", "-v": "Verbose diagnostics",
	"--quiet": "Suppress non-essential success output", "-q": "Suppress non-essential success output",
	"--json": "Output JSON", "--plain": "Plain output", "--dry-run": "Preview without side effects",
	"--backend": "Playback backend", "--room": "Room name", "--playlist": "Playlist name",
	"--playlist-id": "Playlist ID", "--shuffle": "Shuffle toggle", "--choose": "Choose among playlist matches",
	"--volume": "Volume 0-100", "--value": "Volume 0-100", "--watch": "Poll interval",
	"--query": "Playlist filter", "--limit": "Maximum results", "--shortcut": "Shortcut name",
	"--refresh":         "TUI refresh interval",
	"--include-network": "Include network address", "--file": "Input file", "-f": "Input file",
	"--no-input": "Non-interactive mode", "--preset": "Preset name", "--name": "Routine name",
	"--path": "Completion installation path",
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func newCompletionVocabulary(values completionValues) completionVocabulary {
	v := completionVocabulary{groups: map[string][]string{
		"aliases": values.aliases, "rooms": values.rooms, "playlists": values.playlists,
		"presets": automationPresetNames(), "backends": {"airplay", "native"},
		"shells": {"bash", "zsh", "fish"}, "booleans": {"true", "false"},
	}}
	paths := make(map[string]bool)
	for name := range commandFlagSpecs {
		paths[name] = true
		if parent, _, nested := strings.Cut(name, " "); nested {
			paths[parent] = true
		}
	}
	for alias := range commandFlagAliases {
		paths[alias] = true
	}
	for name := range paths {
		if !strings.Contains(name, " ") {
			v.groups["commands"] = append(v.groups["commands"], name)
		}
	}
	sort.Strings(v.groups["commands"])
	for command, child := range planTargetSubcommands {
		paths["plan "+command] = true
		if child != "" {
			paths["plan "+command+" "+child] = true
		}
	}
	paths["root"] = true
	for _, name := range sortedKeys(paths) {
		leaf := strings.TrimPrefix(name, "plan ")
		spec, isLeaf := commandFlagSpecs[leaf]
		if _, alias := commandFlagAliases[leaf]; alias {
			spec, isLeaf = flagsForCommand(leaf), true
		}
		c := completionContext{name: name, legacy: spec.legacySyntax, literal: spec.literalTail}
		for _, flag := range sortedKeys(spec.flags) {
			option := "--" + flag
			if flag == "f" {
				option = "-f"
			}
			c.options = append(c.options, option)
			if spec.flags[flag] == valueFlag {
				c.values = append(c.values, option)
			} else {
				c.booleans = append(c.booleans, option)
				v.addInlineGroup(option, "booleans")
			}
		}
		if isLeaf && !strings.HasPrefix(name, "plan") {
			c.options = append(c.options, "--help", "-h")
			c.terminal = []string{"--help", "-h"}
		}
		for path := range paths {
			if child, ok := strings.CutPrefix(path, name+" "); ok && !strings.Contains(child, " ") {
				c.children = append(c.children, child)
			}
		}
		if name == "root" {
			c.options = sortedKeys(globalFlagNames)
			c.children = v.groups["commands"]
			for option, meaning := range globalFlagNames {
				if meaning == "help" || meaning == "version" {
					c.terminal = append(c.terminal, option)
				}
			}
		}
		// Plan's JSON option applies before and after the target. It is parsed
		// separately from the target's flags, including its legacy = syntax.
		if strings.HasPrefix(name, "plan ") && !slices.Contains(c.options, "--json") {
			c.options = append(c.options, "--json")
			c.booleans = append(c.booleans, "--json")
		}
		switch leaf {
		case "help":
			c.positional, c.stop = "commands", true
		case "run":
			c.positional = "aliases"
		case "play":
			c.positional = "playlists"
		case "out set":
			c.positional, c.repeat = "rooms", true
		case "volume", "vol":
			c.positional, c.start, c.repeat = "rooms", 1, true
			c.positionFlags = []string{"--value", "--volume"}
		case "completion", "completion install":
			c.positional = "shells"
		}
		sort.Strings(c.options)
		sort.Strings(c.children)
		sort.Strings(c.terminal)
		v.groups["options:"+name] = c.options
		if len(c.children) > 0 {
			v.groups["children:"+name] = append(append([]string(nil), c.children...), v.groups[c.positional]...)
		}
		v.contexts = append(v.contexts, c)
	}
	for option, group := range completionValueGroups {
		v.addInlineGroup(option, group)
	}
	return v
}

func (v completionVocabulary) addInlineGroup(option, group string) {
	candidates := make([]string, 0, len(v.groups[group]))
	for _, value := range v.groups[group] {
		candidates = append(candidates, option+"="+value)
	}
	v.groups["inline:"+option] = candidates
}
