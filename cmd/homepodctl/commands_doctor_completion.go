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

// Bash completion values stay in arrays and go straight to COMPREPLY. Using
// compgen -W here would parse and expand config-derived values a second time.
func renderBashCompletion(values completionValues) string {
	return fmt.Sprintf(`# bash completion for homepodctl
_homepodctl_complete_values() {
  local current="$1"
  shift
  local candidate
  COMPREPLY=()
  for candidate in "$@"; do
    if [[ "$candidate" == "$current"* ]]; then
      COMPREPLY+=("$candidate")
    fi
  done
}

_homepodctl_completion() {
  local cur prev
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"
  local -a aliases=(%s)
  local -a rooms=(%s)
  local -a playlists=(%s)
  local -a presets=('morning' 'focus' 'winddown' 'party' 'reset')
  local -a commands=('help' 'version' 'config' 'automation' 'plan' 'schema' 'completion' 'setup' 'doctor' 'devices' 'out' 'playlists' 'status' 'now' 'aliases' 'run' 'pause' 'stop' 'next' 'prev' 'play' 'volume' 'vol' 'native-run' 'config-init' '--help' '--version' '--verbose' '--quiet')
  local -a options=('--json' '--plain' '--help' '--version' '--verbose' '--quiet' '--backend' '--room' '--playlist' '--playlist-id' '--shuffle' '--volume' '--watch' '--query' '--limit' '--shortcut' '--include-network' '--file' '--dry-run' '--no-input' '--preset' '--name')
  if [[ $COMP_CWORD -eq 1 ]]; then
    _homepodctl_complete_values "$cur" "${commands[@]}"
    return 0
  fi
  if [[ "${COMP_WORDS[1]}" == "run" && $COMP_CWORD -eq 2 ]]; then
    _homepodctl_complete_values "$cur" "${aliases[@]}"
    return 0
  fi
  if [[ "$prev" == "--room" ]]; then
    _homepodctl_complete_values "$cur" "${rooms[@]}"
    return 0
  fi
  if [[ "$prev" == "--playlist" || ( "${COMP_WORDS[1]}" == "play" && $COMP_CWORD -eq 2 ) ]]; then
    _homepodctl_complete_values "$cur" "${playlists[@]}"
    return 0
  fi
  if [[ "$prev" == "--preset" ]]; then
    _homepodctl_complete_values "$cur" "${presets[@]}"
    return 0
  fi
  if [[ "${COMP_WORDS[1]}" == "out" && "${COMP_WORDS[2]}" == "set" ]]; then
    _homepodctl_complete_values "$cur" "${rooms[@]}"
    return 0
  fi
  _homepodctl_complete_values "$cur" "${options[@]}"
}
complete -F _homepodctl_completion homepodctl
`, bashArrayLiteral(values.aliases), bashArrayLiteral(values.rooms), bashArrayLiteral(values.playlists))
}

func zshArrayLiteral(values []string) string {
	literals := make([]string, 0, len(values))
	for _, value := range values {
		literals = append(literals, "'"+strings.ReplaceAll(value, "'", `'\''`)+"'")
	}
	return strings.Join(literals, " ")
}

func renderZshCompletion(values completionValues) string {
	return fmt.Sprintf(`#compdef homepodctl
_homepodctl() {
  local -a commands
  local -a opts
  local -a aliases
  local -a rooms
  local -a playlists
  local -a presets
  local -a expl
  commands=(
    'help:Show help'
    'version:Show version'
    'config:Inspect/update config'
    'automation:Run automation routines'
    'plan:Preview command execution'
    'schema:Show JSON schemas'
    'completion:Generate shell completion'
    'setup:Onboard and verify environment'
    'doctor:Run diagnostics'
    'devices:List devices'
    'out:Manage outputs'
    'playlists:List playlists'
    'status:Show playback, route, and backend status'
    'now:Alias of status'
    'aliases:List aliases'
    'run:Run alias'
    'pause:Pause playback'
    'stop:Stop playback'
    'next:Next track'
    'prev:Previous track'
    'play:Play playlist'
    'volume:Set volume'
    'vol:Set volume'
    'native-run:Run shortcut'
    'config-init:Write starter config'
  )
  aliases=(%s)
  rooms=(%s)
  playlists=(%s)
  presets=('morning' 'focus' 'winddown' 'party' 'reset')
  opts=(
    '--version[show version]'
    '--json[output JSON]'
    '--plain[plain output]'
    '--verbose[verbose diagnostics]'
    '--quiet[suppress non-essential success output]'
    '--dry-run[preview without side effects]'
    '--backend[backend]:backend:(airplay native)'
    '--room[room name]'
    '--playlist[playlist name]'
    '--playlist-id[playlist ID]'
    '--shuffle[shuffle toggle]'
    '--volume[volume 0-100]'
    '--watch[poll interval]'
    '--query[playlist filter]'
    '--limit[max results]'
    '--shortcut[shortcut name]'
    '--include-network[include network address]'
    '--file[input file]'
    '--no-input[non-interactive mode]'
    '--preset[preset name]'
    '--name[routine name]'
  )
  if [[ $CURRENT -eq 3 && ${words[2]} == run ]]; then
    _description aliases expl "alias"
    compadd "$expl[@]" -a aliases
    return
  fi
  if [[ ${words[CURRENT-1]} == --room ]]; then
    _description rooms expl "room"
    compadd "$expl[@]" -a rooms
    return
  fi
  if [[ ${words[CURRENT-1]} == --playlist || ( ${words[2]} == play && $CURRENT -eq 3 ) ]]; then
    _description playlists expl "playlist"
    compadd "$expl[@]" -a playlists
    return
  fi
  if [[ ${words[CURRENT-1]} == --preset ]]; then
    _describe -t presets "preset" presets
    return
  fi
  _arguments $opts '*::command:->command'
  case $state in
    command) _describe -t commands "homepodctl command" commands ;;
  esac
}
_homepodctl "$@"
`, zshArrayLiteral(values.aliases), zshArrayLiteral(values.rooms), zshArrayLiteral(values.playlists))
}

func fishStringLiteral(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `\'`)
	return "'" + value + "'"
}

// fishCompletionArguments quotes twice because complete -a parses and expands
// its argument again when Fish computes completion candidates.
func fishCompletionArguments(values []string) string {
	literals := make([]string, 0, len(values))
	for _, value := range values {
		literals = append(literals, fishStringLiteral(value))
	}
	return fishStringLiteral(strings.Join(literals, " "))
}

func appendFishCompletion(fish *strings.Builder, condition string, values []string) {
	if len(values) == 0 {
		return
	}
	fish.WriteString("complete -c homepodctl -n ")
	fish.WriteString(fishStringLiteral(condition))
	fish.WriteString(" -a ")
	fish.WriteString(fishCompletionArguments(values))
	fish.WriteByte('\n')
}

func renderFishCompletion(values completionValues) string {
	var fish strings.Builder
	fish.WriteString(`# fish completion for homepodctl
complete -c homepodctl -f -a "help version config automation plan schema completion setup doctor devices out playlists status now aliases run pause stop next prev play volume vol native-run config-init"
complete -c homepodctl -l version
complete -c homepodctl -l json
complete -c homepodctl -l plain
complete -c homepodctl -l verbose
complete -c homepodctl -l quiet
complete -c homepodctl -l backend
complete -c homepodctl -l room
complete -c homepodctl -l playlist
complete -c homepodctl -l playlist-id
complete -c homepodctl -l shuffle
complete -c homepodctl -l volume
complete -c homepodctl -l watch
complete -c homepodctl -l query
complete -c homepodctl -l limit
complete -c homepodctl -l shortcut
complete -c homepodctl -l include-network
complete -c homepodctl -l file
complete -c homepodctl -l dry-run
complete -c homepodctl -l no-input
complete -c homepodctl -l preset
complete -c homepodctl -l name
complete -c homepodctl -n '__fish_seen_argument --preset' -a "morning focus winddown party reset"
`)
	appendFishCompletion(&fish, "__fish_seen_subcommand_from run", values.aliases)
	appendFishCompletion(&fish, "__fish_seen_argument --room", values.rooms)
	appendFishCompletion(&fish, "__fish_seen_subcommand_from out; and __fish_seen_subcommand_from set", values.rooms)
	appendFishCompletion(&fish, "__fish_seen_subcommand_from play", values.playlists)
	appendFishCompletion(&fish, "__fish_seen_argument --playlist", values.playlists)
	return fish.String()
}
