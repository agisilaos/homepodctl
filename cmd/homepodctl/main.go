package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

var (
	version              = "dev"
	commit               = "none"
	date                 = "unknown"
	getNowPlaying        = music.GetNowPlaying
	searchPlaylists      = music.SearchUserPlaylists
	setCurrentOutputs    = music.SetCurrentAirPlayDevices
	setDeviceVolume      = music.SetAirPlayDeviceVolume
	setShuffle           = music.SetShuffleEnabled
	playPlaylistByID     = music.PlayUserPlaylistByPersistentID
	findPlaylistNameByID = music.FindUserPlaylistNameByPersistentID
	runNativeShortcut    = native.RunShortcut
	stopPlayback         = music.Stop
	lookPath             = exec.LookPath
	configPath           = native.ConfigPath
	loadConfigOptional   = native.LoadConfigOptional
	newStatusTicker      = func(d time.Duration) statusTicker { return realStatusTicker{ticker: time.NewTicker(d)} }
	sleepFn              = time.Sleep
	verbose              bool
	jsonErrorOut         bool
)

type statusTicker interface {
	Chan() <-chan time.Time
	Stop()
}

type realStatusTicker struct {
	ticker *time.Ticker
}

func (t realStatusTicker) Chan() <-chan time.Time {
	return t.ticker.C
}

func (t realStatusTicker) Stop() {
	t.ticker.Stop()
}

const (
	exitGeneric = 1
	exitUsage   = 2
	exitConfig  = 3
	exitBackend = 4
)

type globalOptions struct {
	help    bool
	verbose bool
}

func parseGlobalOptions(args []string) (globalOptions, string, []string, error) {
	opts := globalOptions{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return opts, "", nil, usageErrf("missing command")
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			return opts, a, args[i+1:], nil
		}
		switch a {
		case "-h", "--help":
			opts.help = true
		case "-v", "--verbose":
			opts.verbose = true
		default:
			return globalOptions{}, "", nil, usageErrf("unknown global flag: %s (tip: run `homepodctl --help`)", a)
		}
	}
	return opts, "", nil, nil
}

func main() {
	jsonErrorOut = wantsJSONErrors(os.Args[1:])
	if runtime.GOOS != "darwin" {
		die(errors.New("homepodctl only supports macOS (darwin)"))
	}

	opts, cmd, args, err := parseGlobalOptions(os.Args[1:])
	if err != nil {
		if !jsonErrorOut {
			usage()
		}
		die(err)
	}
	verbose = opts.verbose || envTruthy(os.Getenv("HOMEPODCTL_VERBOSE"))
	debugf("command=%q args=%q", cmd, args)

	if opts.help || cmd == "" {
		usage()
		if cmd == "" && !opts.help {
			os.Exit(exitUsage)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var cfg *native.Config
	loadCfg := func() *native.Config {
		if cfg != nil {
			return cfg
		}
		loadedCfg, cfgErr := native.LoadConfigOptional()
		if cfgErr != nil {
			die(cfgErr)
		}
		cfg = loadedCfg
		debugf("config: default_backend=%q default_rooms=%v aliases=%d", cfg.Defaults.Backend, cfg.Defaults.Rooms, len(cfg.Aliases))
		return cfg
	}

	switch cmd {
	case "help":
		cmdHelp(args)
	case "version":
		fmt.Printf("homepodctl %s (%s) %s\n", version, commit, date)
	case "automation":
		cmdAutomation(ctx, loadCfg(), args)
	case "config":
		cmdConfig(args)
	case "completion":
		cmdCompletion(args)
	case "doctor":
		cmdDoctor(ctx, args)
	case "plan":
		cmdPlan(args)
	case "schema":
		cmdSchema(args)
	case "devices":
		cmdDevices(ctx, args)
	case "playlists":
		cmdPlaylists(ctx, args)
	case "status":
		cmdStatus(ctx, args)
	case "now":
		cmdStatus(ctx, args)
	case "out":
		cmdOut(ctx, loadCfg(), args)
	case "aliases":
		cmdAliases(loadCfg(), args)
	case "run":
		cmdRun(ctx, loadCfg(), args)
	case "pause":
		cmdTransport(ctx, args, "pause", music.Pause)
	case "stop":
		cmdTransport(ctx, args, "stop", music.Stop)
	case "next":
		cmdTransport(ctx, args, "next", music.NextTrack)
	case "prev":
		cmdTransport(ctx, args, "prev", music.PreviousTrack)
	case "play":
		cmdPlay(ctx, loadCfg(), args)
	case "volume":
		cmdVolume(ctx, loadCfg(), "volume", args)
	case "vol":
		cmdVolume(ctx, loadCfg(), "vol", args)
	case "native-run":
		cmdNativeRun(ctx, args)
	case "config-init":
		cmdConfigInit()
	default:
		if !jsonErrorOut {
			usage()
		}
		die(usageErrf("unknown command: %q (run `homepodctl --help`)", cmd))
	}
}
