package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/agisilaos/homepodctl/internal/native"
)

func cmdCompletion(args []string) {
	if len(args) == 0 {
		die(usageErrf("usage: homepodctl completion <bash|zsh|fish>\n       homepodctl completion install <bash|zsh|fish> [--path <file-or-dir>]"))
	}
	if args[0] == "install" {
		cmdCompletionInstall(args[1:])
		return
	}
	_, pos, err := parseArgs("completion", args)
	if err != nil {
		die(err)
	}
	args = pos
	if len(args) != 1 {
		die(usageErrf("usage: homepodctl completion <bash|zsh|fish>\n       homepodctl completion install <bash|zsh|fish> [--path <file-or-dir>]"))
	}
	shell := strings.ToLower(strings.TrimSpace(args[0]))
	script, err := completionScript(shell)
	if err != nil {
		die(err)
	}
	fmt.Print(script)
}

func cmdCompletionInstall(args []string) {
	flags, pos, err := parseArgs("completion install", args)
	if err != nil {
		die(err)
	}
	if len(pos) != 1 || strings.TrimSpace(pos[0]) == "" {
		die(usageErrf("usage: homepodctl completion install <bash|zsh|fish> [--path <file-or-dir>]"))
	}
	shell := strings.ToLower(strings.TrimSpace(pos[0]))
	path := strings.TrimSpace(flags.string("path"))
	installedPath, err := installCompletion(shell, path)
	if err != nil {
		die(err)
	}
	if !quiet {
		fmt.Printf("Installed %s completion: %s\n", shell, installedPath)
	}
}

func completionInstallPath(shell string, override string) (string, error) {
	name, err := completionFileName(shell)
	if err != nil {
		return "", err
	}
	target := strings.TrimSpace(override)
	if target != "" {
		target = expandHomePath(target)
		base := filepath.Base(target)
		info, statErr := os.Stat(target)
		if statErr == nil && info.IsDir() {
			return filepath.Join(target, name), nil
		}
		if strings.HasSuffix(target, string(os.PathSeparator)) {
			return filepath.Join(target, name), nil
		}
		if statErr != nil && os.IsNotExist(statErr) && filepath.Ext(target) == "" && base != name {
			return filepath.Join(target, name), nil
		}
		return target, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch shell {
	case "bash":
		return filepath.Join(home, ".local", "share", "bash-completion", "completions", name), nil
	case "zsh":
		return filepath.Join(home, ".zsh", "completions", name), nil
	case "fish":
		return filepath.Join(home, ".config", "fish", "completions", name), nil
	default:
		return "", usageErrf("unknown shell %q (expected bash, zsh, or fish)", shell)
	}
}

func completionFileName(shell string) (string, error) {
	switch shell {
	case "bash":
		return "homepodctl", nil
	case "zsh":
		return "_homepodctl", nil
	case "fish":
		return "homepodctl.fish", nil
	default:
		return "", usageErrf("unknown shell %q (expected bash, zsh, or fish)", shell)
	}
}

func installCompletion(shell string, override string) (string, error) {
	target, err := completionInstallPath(shell, override)
	if err != nil {
		return "", err
	}
	script, err := completionScript(shell)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, []byte(script), 0o644); err != nil {
		return "", err
	}
	return target, nil
}

func expandHomePath(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
		return path
	}
	prefix := "~" + string(os.PathSeparator)
	if !strings.HasPrefix(path, prefix) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, prefix))
}

type completionValues struct {
	aliases   []string
	rooms     []string
	playlists []string
}

func completionData(cfg *native.Config) completionValues {
	aliasSet := map[string]bool{}
	roomSet := map[string]bool{}
	playlistSet := map[string]bool{}
	if cfg == nil {
		return completionValues{}
	}
	for name, a := range cfg.Aliases {
		if strings.TrimSpace(name) != "" {
			aliasSet[name] = true
		}
		for _, room := range a.Rooms {
			room = strings.TrimSpace(room)
			if room != "" {
				roomSet[room] = true
			}
		}
		playlist := strings.TrimSpace(a.Playlist)
		if playlist != "" {
			playlistSet[playlist] = true
		}
	}
	for _, room := range cfg.Defaults.Rooms {
		room = strings.TrimSpace(room)
		if room != "" {
			roomSet[room] = true
		}
	}
	for room := range cfg.Native.Playlists {
		if strings.TrimSpace(room) != "" {
			roomSet[room] = true
		}
		for playlist := range cfg.Native.Playlists[room] {
			playlist = strings.TrimSpace(playlist)
			if playlist != "" {
				playlistSet[playlist] = true
			}
		}
	}
	for room := range cfg.Native.VolumeShortcuts {
		if strings.TrimSpace(room) != "" {
			roomSet[room] = true
		}
	}
	values := completionValues{
		aliases:   make([]string, 0, len(aliasSet)),
		rooms:     make([]string, 0, len(roomSet)),
		playlists: make([]string, 0, len(playlistSet)),
	}
	for alias := range aliasSet {
		values.aliases = append(values.aliases, alias)
	}
	for room := range roomSet {
		values.rooms = append(values.rooms, room)
	}
	for playlist := range playlistSet {
		values.playlists = append(values.playlists, playlist)
	}
	sort.Strings(values.aliases)
	sort.Strings(values.rooms)
	sort.Strings(values.playlists)
	return values
}

func validateCompletionCandidates(kind string, candidates []string) error {
	for _, candidate := range candidates {
		if strings.ContainsRune(candidate, '\x00') {
			return fmt.Errorf("%s completion candidate %q contains a NUL byte", kind, candidate)
		}
	}
	return nil
}

func (values completionValues) validate() error {
	if err := validateCompletionCandidates("alias", values.aliases); err != nil {
		return err
	}
	if err := validateCompletionCandidates("room", values.rooms); err != nil {
		return err
	}
	return validateCompletionCandidates("playlist", values.playlists)
}

func validateFishCompletionCandidates(kind string, candidates []string) error {
	for _, candidate := range candidates {
		if strings.ContainsRune(candidate, '\t') {
			return fmt.Errorf("%s completion candidate %q contains a tab, which Fish reserves for descriptions", kind, candidate)
		}
	}
	return nil
}

func (values completionValues) validateFish() error {
	if err := validateFishCompletionCandidates("alias", values.aliases); err != nil {
		return err
	}
	if err := validateFishCompletionCandidates("room", values.rooms); err != nil {
		return err
	}
	return validateFishCompletionCandidates("playlist", values.playlists)
}

func completionScript(shell string) (string, error) {
	cfg, err := native.LoadConfigOptional()
	if err != nil {
		return "", err
	}
	return renderCompletion(shell, completionData(cfg))
}

func renderCompletion(shell string, values completionValues) (string, error) {
	if err := values.validate(); err != nil {
		return "", err
	}

	switch shell {
	case "bash":
		return renderBashCompletion(values), nil
	case "zsh":
		return renderZshCompletion(values), nil
	case "fish":
		if err := values.validateFish(); err != nil {
			return "", err
		}
		return renderFishCompletion(values), nil
	default:
		return "", usageErrf("unknown shell %q (expected bash, zsh, or fish)", shell)
	}
}

func bashArrayLiteral(values []string) string {
	literals := make([]string, 0, len(values))
	for _, value := range values {
		literals = append(literals, "'"+strings.ReplaceAll(value, "'", `'\''`)+"'")
	}
	return strings.Join(literals, " ")
}

func zshArrayLiteral(values []string) string {
	literals := make([]string, 0, len(values))
	for _, value := range values {
		literals = append(literals, "'"+strings.ReplaceAll(value, "'", `'\''`)+"'")
	}
	return strings.Join(literals, " ")
}

func fishStringLiteral(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `\'`)
	return "'" + value + "'"
}

func appendFishCompletion(fish *strings.Builder, condition string, values []string) {
	if len(values) == 0 {
		return
	}
	fish.WriteString("complete -c homepodctl -n ")
	fish.WriteString(fishStringLiteral(condition))
	fish.WriteString(" -a ")
	// Quote both the static arguments and the expression that complete -a
	// evaluates later. NUL framing preserves newlines in returned candidates.
	fish.WriteString(fishStringLiteral("(_homepodctl_candidates " + fishArrayLiteral(values) + " | string split0)"))
	fish.WriteByte('\n')
}
