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
	var shell string
	var path string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--path=") {
			path = strings.TrimSpace(strings.TrimPrefix(a, "--path="))
			continue
		}
		if a == "--path" {
			if i+1 >= len(args) {
				die(usageErrf("usage: homepodctl completion install <bash|zsh|fish> [--path <file-or-dir>]"))
			}
			i++
			path = strings.TrimSpace(args[i])
			continue
		}
		if strings.HasPrefix(a, "-") {
			die(usageErrf("unknown flag: %s", a))
		}
		if shell != "" {
			die(usageErrf("usage: homepodctl completion install <bash|zsh|fish> [--path <file-or-dir>]"))
		}
		shell = strings.ToLower(strings.TrimSpace(a))
	}
	if shell == "" {
		die(usageErrf("usage: homepodctl completion install <bash|zsh|fish> [--path <file-or-dir>]"))
	}
	installedPath, err := installCompletion(shell, path)
	if err != nil {
		die(err)
	}
	fmt.Printf("Installed %s completion: %s\n", shell, installedPath)
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

func completionData(cfg *native.Config) (aliases []string, rooms []string, playlists []string) {
	aliasSet := map[string]bool{}
	roomSet := map[string]bool{}
	playlistSet := map[string]bool{}
	if cfg == nil {
		return nil, nil, nil
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
	for a := range aliasSet {
		aliases = append(aliases, a)
	}
	for r := range roomSet {
		rooms = append(rooms, r)
	}
	for p := range playlistSet {
		playlists = append(playlists, p)
	}
	sort.Strings(aliases)
	sort.Strings(rooms)
	sort.Strings(playlists)
	return aliases, rooms, playlists
}

func joinBashWords(words []string) string {
	escaped := make([]string, 0, len(words))
	for _, w := range words {
		escaped = append(escaped, strings.ReplaceAll(w, " ", `\ `))
	}
	return strings.Join(escaped, " ")
}

func joinZshWords(words []string) string {
	quoted := make([]string, 0, len(words))
	for _, w := range words {
		quoted = append(quoted, "'"+strings.ReplaceAll(w, "'", `'\''`)+"'")
	}
	return strings.Join(quoted, " ")
}

func completionScript(shell string) (string, error) {
	cfg, _ := native.LoadConfigOptional()
	aliases, rooms, playlists := completionData(cfg)
	aliasBash := joinBashWords(aliases)
	roomBash := joinBashWords(rooms)
	playlistBash := joinBashWords(playlists)
	aliasZsh := joinZshWords(aliases)
	roomZsh := joinZshWords(rooms)
	playlistZsh := joinZshWords(playlists)

	switch shell {
	case "bash":
		return fmt.Sprintf(`# bash completion for homepodctl
_homepodctl_completion() {
  local cur prev
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"
  local aliases="%s"
  local rooms="%s"
  local playlists="%s"
  local presets="morning focus winddown party reset"
  local cmds="help version config automation plan schema completion doctor devices out playlists status now aliases run pause stop next prev play volume vol native-run config-init"
  if [[ $COMP_CWORD -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "$cmds --help --verbose" -- "$cur") )
    return 0
  fi
  if [[ "${COMP_WORDS[1]}" == "run" && $COMP_CWORD -eq 2 ]]; then
    COMPREPLY=( $(compgen -W "$aliases" -- "$cur") )
    return 0
  fi
  if [[ "$prev" == "--room" ]]; then
    COMPREPLY=( $(compgen -W "$rooms" -- "$cur") )
    return 0
  fi
  if [[ "$prev" == "--playlist" || ( "${COMP_WORDS[1]}" == "play" && $COMP_CWORD -eq 2 ) ]]; then
    COMPREPLY=( $(compgen -W "$playlists" -- "$cur") )
    return 0
  fi
  if [[ "$prev" == "--preset" ]]; then
    COMPREPLY=( $(compgen -W "$presets" -- "$cur") )
    return 0
  fi
  if [[ "${COMP_WORDS[1]}" == "out" && "${COMP_WORDS[2]}" == "set" ]]; then
    COMPREPLY=( $(compgen -W "$rooms" -- "$cur") )
    return 0
  fi
  COMPREPLY=( $(compgen -W "--json --plain --help --verbose --backend --room --playlist --playlist-id --shuffle --volume --watch --query --limit --shortcut --include-network --file --dry-run --no-input --preset --name" -- "$cur") )
}
complete -F _homepodctl_completion homepodctl
`, aliasBash, roomBash, playlistBash), nil
	case "zsh":
		return fmt.Sprintf(`#compdef homepodctl
_homepodctl() {
  local -a commands
  local -a opts
  local -a aliases
  local -a rooms
  local -a playlists
  local -a presets
  commands=(
    'help:Show help'
    'version:Show version'
    'config:Inspect/update config'
    'automation:Run automation routines'
    'plan:Preview command execution'
    'schema:Show JSON schemas'
    'completion:Generate shell completion'
    'doctor:Run diagnostics'
    'devices:List devices'
    'out:Manage outputs'
    'playlists:List playlists'
    'status:Show now playing'
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
    '--json[output JSON]'
    '--plain[plain output]'
    '--verbose[verbose diagnostics]'
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
    _describe -t aliases "alias" aliases
    return
  fi
  if [[ ${words[CURRENT-1]} == --room ]]; then
    _describe -t rooms "room" rooms
    return
  fi
  if [[ ${words[CURRENT-1]} == --playlist || ( ${words[2]} == play && $CURRENT -eq 3 ) ]]; then
    _describe -t playlists "playlist" playlists
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
`, aliasZsh, roomZsh, playlistZsh), nil
	case "fish":
		var fish strings.Builder
		fish.WriteString(`# fish completion for homepodctl
complete -c homepodctl -f -a "help version config automation plan schema completion doctor devices out playlists status now aliases run pause stop next prev play volume vol native-run config-init"
complete -c homepodctl -l json
complete -c homepodctl -l plain
complete -c homepodctl -l verbose
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
		for _, a := range aliases {
			fish.WriteString(fmt.Sprintf("complete -c homepodctl -n '__fish_seen_subcommand_from run' -a %q\n", a))
		}
		for _, r := range rooms {
			fish.WriteString(fmt.Sprintf("complete -c homepodctl -n '__fish_seen_argument --room' -a %q\n", r))
			fish.WriteString(fmt.Sprintf("complete -c homepodctl -n '__fish_seen_subcommand_from out; and __fish_seen_subcommand_from set' -a %q\n", r))
		}
		for _, p := range playlists {
			fish.WriteString(fmt.Sprintf("complete -c homepodctl -n '__fish_seen_subcommand_from play' -a %q\n", p))
			fish.WriteString(fmt.Sprintf("complete -c homepodctl -n '__fish_seen_argument --playlist' -a %q\n", p))
		}
		return fish.String(), nil
	default:
		return "", usageErrf("unknown shell %q (expected bash, zsh, or fish)", shell)
	}
}
