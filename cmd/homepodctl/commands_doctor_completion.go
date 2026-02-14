package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/agisilaos/homepodctl/internal/native"
)

type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // pass|warn|fail
	Message string `json:"message"`
	Tip     string `json:"tip,omitempty"`
}

type doctorReport struct {
	OK        bool          `json:"ok"`
	CheckedAt string        `json:"checkedAt"`
	Checks    []doctorCheck `json:"checks"`
}

func cmdDoctor(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "output JSON")
	plain := fs.Bool("plain", false, "plain output")
	if err := fs.Parse(args); err != nil {
		os.Exit(exitUsage)
	}
	report := runDoctorChecks(ctx)
	if *jsonOut {
		writeJSON(report)
	} else {
		printDoctorReport(report, *plain)
	}
	if !report.OK {
		os.Exit(exitGeneric)
	}
}

func runDoctorChecks(ctx context.Context) doctorReport {
	report := doctorReport{
		OK:        true,
		CheckedAt: time.Now().Format(time.RFC3339),
	}
	add := func(c doctorCheck) {
		if c.Status == "fail" {
			report.OK = false
		}
		report.Checks = append(report.Checks, c)
	}

	if _, err := lookPath("osascript"); err != nil {
		add(doctorCheck{Name: "osascript", Status: "fail", Message: "osascript not found", Tip: "Install/restore macOS command-line tools."})
	} else {
		add(doctorCheck{Name: "osascript", Status: "pass", Message: "osascript available"})
	}
	if _, err := lookPath("shortcuts"); err != nil {
		add(doctorCheck{Name: "shortcuts", Status: "warn", Message: "shortcuts command not found", Tip: "Native backend requires the Shortcuts CLI."})
	} else {
		add(doctorCheck{Name: "shortcuts", Status: "pass", Message: "shortcuts available"})
	}

	path, err := configPath()
	if err != nil {
		add(doctorCheck{Name: "config-path", Status: "fail", Message: fmt.Sprintf("cannot resolve config path: %v", err)})
	} else {
		add(doctorCheck{Name: "config-path", Status: "pass", Message: path})
		cfg, cfgErr := loadConfigOptional()
		if cfgErr != nil {
			add(doctorCheck{Name: "config", Status: "fail", Message: cfgErr.Error(), Tip: "Fix JSON syntax or re-run `homepodctl config-init`."})
		} else if len(cfg.Aliases) == 0 {
			add(doctorCheck{Name: "config", Status: "warn", Message: "no aliases configured", Tip: "Run `homepodctl config-init` and edit defaults/aliases."})
		} else {
			add(doctorCheck{Name: "config", Status: "pass", Message: fmt.Sprintf("aliases=%d", len(cfg.Aliases))})
		}
	}

	backendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := getNowPlaying(backendCtx); err != nil {
		add(doctorCheck{
			Name:    "music-backend",
			Status:  "warn",
			Message: formatError(err),
			Tip:     "Open Music.app and grant Automation permissions if prompted.",
		})
	} else {
		add(doctorCheck{Name: "music-backend", Status: "pass", Message: "Music backend reachable"})
	}
	return report
}

func printDoctorReport(report doctorReport, plain bool) {
	if plain {
		fmt.Println("STATUS\tCHECK\tMESSAGE\tTIP")
		for _, c := range report.Checks {
			fmt.Printf("%s\t%s\t%s\t%s\n", c.Status, c.Name, c.Message, c.Tip)
		}
		return
	}
	fmt.Printf("doctor ok=%t checked_at=%s\n", report.OK, report.CheckedAt)
	for _, c := range report.Checks {
		if c.Tip != "" {
			fmt.Printf("%s\t%s\t%s (tip: %s)\n", c.Status, c.Name, c.Message, c.Tip)
			continue
		}
		fmt.Printf("%s\t%s\t%s\n", c.Status, c.Name, c.Message)
	}
}

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
