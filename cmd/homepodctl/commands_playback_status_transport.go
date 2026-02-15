package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
)

type statusTrack struct {
	Name   string `json:"name,omitempty"`
	Artist string `json:"artist,omitempty"`
	Album  string `json:"album,omitempty"`
}

type statusOutput struct {
	DeviceName string `json:"deviceName"`
	Room       string `json:"room"`
	Volume     int    `json:"volume"`
	Kind       string `json:"kind,omitempty"`
}

type statusConnection struct {
	Music      string `json:"music"`      // connected|unreachable|missing|error
	Automation string `json:"automation"` // granted|denied|unknown
	Message    string `json:"message,omitempty"`
}

type statusResult struct {
	OK         bool             `json:"ok"`
	Player     string           `json:"player"`
	Track      *statusTrack     `json:"track,omitempty"`
	Volume     *int             `json:"volume,omitempty"`
	Outputs    []statusOutput   `json:"outputs,omitempty"`
	Route      []string         `json:"route,omitempty"`
	Connection statusConnection `json:"connection"`
}

func collectStatus(ctx context.Context) (statusResult, error) {
	if _, err := lookPath("osascript"); err != nil {
		return statusResult{
			OK:     false,
			Player: "unknown",
			Connection: statusConnection{
				Music:      "missing",
				Automation: "unknown",
				Message:    "osascript not found",
			},
		}, err
	}

	np, err := getNowPlaying(ctx)
	if err != nil {
		connection := inferStatusConnection(err)
		return statusResult{
			OK:         false,
			Player:     "unknown",
			Connection: connection,
		}, err
	}

	outs := make([]statusOutput, 0, len(np.Outputs))
	route := make([]string, 0, len(np.Outputs))
	totalVolume := 0
	for _, o := range np.Outputs {
		outs = append(outs, statusOutput{
			DeviceName: o.Name,
			Room:       o.Name,
			Volume:     o.Volume,
			Kind:       strings.TrimSpace(o.Kind),
		})
		route = append(route, o.Name)
		totalVolume += o.Volume
	}
	var volume *int
	if len(np.Outputs) > 0 {
		avg := totalVolume / len(np.Outputs)
		volume = &avg
	}

	var track *statusTrack
	if strings.TrimSpace(np.Track.Name) != "" || strings.TrimSpace(np.Track.Artist) != "" || strings.TrimSpace(np.Track.Album) != "" {
		track = &statusTrack{
			Name:   np.Track.Name,
			Artist: np.Track.Artist,
			Album:  np.Track.Album,
		}
	}

	return statusResult{
		OK:      true,
		Player:  strings.TrimSpace(np.PlayerState),
		Track:   track,
		Volume:  volume,
		Outputs: outs,
		Route:   route,
		Connection: statusConnection{
			Music:      "connected",
			Automation: "granted",
		},
	}, nil
}

func inferStatusConnection(err error) statusConnection {
	c := statusConnection{
		Music:      "error",
		Automation: "unknown",
		Message:    formatError(err),
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		c.Music = "unreachable"
		return c
	}
	var scriptErr *music.ScriptError
	if !errors.As(err, &scriptErr) {
		return c
	}
	output := strings.ToLower(scriptErr.Output)
	switch {
	case strings.Contains(output, "not authorised"),
		strings.Contains(output, "not authorized"),
		strings.Contains(output, "not permitted"):
		c.Music = "connected"
		c.Automation = "denied"
	case strings.Contains(output, "connection invalid"),
		strings.Contains(output, "can't get application \"music\""),
		strings.Contains(output, "application isn’t running"),
		strings.Contains(output, "application isn't running"):
		c.Music = "unreachable"
	default:
		c.Music = "error"
	}
	return c
}

func printStatus(res statusResult) {
	fmt.Printf("ok=%t player=%s", res.OK, res.Player)
	if res.Track != nil && strings.TrimSpace(res.Track.Name) != "" {
		fmt.Printf(" track=%q", res.Track.Name)
	}
	if res.Track != nil && strings.TrimSpace(res.Track.Artist) != "" {
		fmt.Printf(" artist=%q", res.Track.Artist)
	}
	fmt.Println()
	if len(res.Outputs) > 0 {
		parts := make([]string, 0, len(res.Outputs))
		for _, o := range res.Outputs {
			parts = append(parts, fmt.Sprintf("%s(vol=%d)", o.DeviceName, o.Volume))
		}
		fmt.Printf("outputs=%s\n", strings.Join(parts, ", "))
	}
	if len(res.Route) > 0 {
		fmt.Printf("route=%s\n", strings.Join(res.Route, ", "))
	}
	if res.Volume != nil {
		fmt.Printf("volume=%d\n", *res.Volume)
	}
	fmt.Printf("music=%s automation=%s\n", res.Connection.Music, res.Connection.Automation)
	if strings.TrimSpace(res.Connection.Message) != "" {
		fmt.Printf("message=%q\n", res.Connection.Message)
	}
}

func printStatusPlain(res statusResult) {
	track := ""
	artist := ""
	album := ""
	if res.Track != nil {
		track = res.Track.Name
		artist = res.Track.Artist
		album = res.Track.Album
	}
	volume := ""
	if res.Volume != nil {
		volume = fmt.Sprintf("%d", *res.Volume)
	}
	outputs := make([]string, 0, len(res.Outputs))
	for _, o := range res.Outputs {
		outputs = append(outputs, fmt.Sprintf("%s=%d", o.DeviceName, o.Volume))
	}
	fmt.Printf("%t\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
		res.OK,
		res.Player,
		track,
		artist,
		album,
		volume,
		strings.Join(res.Route, ","),
		res.Connection.Music,
		res.Connection.Automation,
	)
	if len(outputs) > 0 {
		fmt.Printf("outputs\t%s\n", strings.Join(outputs, ","))
	}
}

func cmdStatus(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOut := fs.Bool("json", false, "output JSON")
	plain := fs.Bool("plain", false, "plain output")
	watch := fs.Duration("watch", 0, "poll interval (e.g. 1s); 0 prints once")
	if err := fs.Parse(args); err != nil {
		exitCode(exitUsage)
	}
	debugf("status: json=%t plain=%t watch=%s", *jsonOut, *plain, watch.String())
	printOnce := func() error {
		res, err := collectStatus(ctx)
		if *jsonOut {
			writeJSON(res)
		} else if *plain {
			printStatusPlain(res)
		} else {
			printStatus(res)
		}
		return err
	}
	if err := runStatusLoop(ctx, *watch, printOnce); err != nil {
		die(err)
	}
}

func runStatusLoop(ctx context.Context, watch time.Duration, printOnce func() error) error {
	if watch <= 0 {
		return printOnce()
	}
	ticker := newStatusTicker(watch)
	defer ticker.Stop()
	for {
		if err := printOnce(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.Chan():
		}
	}
}

func cmdTransport(ctx context.Context, args []string, action string, fn func(context.Context) error) {
	flags, positionals, err := parseArgs(args)
	if err != nil {
		die(err)
	}
	if len(positionals) != 0 {
		die(usageErrf("usage: homepodctl %s [--json] [--plain]", action))
	}
	jsonOut, plainOut, err := parseOutputFlags(flags)
	if err != nil {
		die(err)
	}
	if err := fn(ctx); err != nil {
		die(err)
	}
	if np, err := getNowPlaying(ctx); err == nil {
		writeActionOutput(action, jsonOut, plainOut, actionOutput{NowPlaying: &np})
		return
	}
	writeActionOutput(action, jsonOut, plainOut, actionOutput{})
}
