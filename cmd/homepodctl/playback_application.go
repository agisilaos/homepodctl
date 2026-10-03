package main

import (
	"context"

	"github.com/agisilaos/homepodctl/internal/music"
)

// playbackApplication is the in-process boundary shared by ordinary commands
// and the interactive TUI. Function fields preserve the repository's focused
// test seams while keeping the consumer-facing behavior in one service.
type playbackApplication struct {
	snapshotFn   func(context.Context) (music.PlaybackSnapshot, error)
	nowPlayingFn func(context.Context) (music.NowPlaying, error)
	devicesFn    func(context.Context) ([]music.AirPlayDevice, error)
	setRouteFn   func(context.Context, []string) error
	setVolumeFn  func(context.Context, string, int) error
	playPauseFn  func(context.Context) error
	pauseFn      func(context.Context) error
	stopFn       func(context.Context) error
	nextFn       func(context.Context) error
	previousFn   func(context.Context) error
}

func newPlaybackApplication() *playbackApplication {
	return &playbackApplication{
		snapshotFn:   music.GetPlaybackSnapshot,
		nowPlayingFn: music.GetNowPlaying,
		devicesFn:    music.ListAirPlayDevices,
		setRouteFn:   music.SetCurrentAirPlayDevices,
		setVolumeFn:  music.SetAirPlayDeviceVolume,
		playPauseFn:  music.PlayPause,
		pauseFn:      music.Pause,
		stopFn:       music.Stop,
		nextFn:       music.NextTrack,
		previousFn:   music.PreviousTrack,
	}
}

func (p *playbackApplication) Snapshot(ctx context.Context) (music.PlaybackSnapshot, error) {
	return p.snapshotFn(ctx)
}

func (p *playbackApplication) NowPlaying(ctx context.Context) (music.NowPlaying, error) {
	return p.nowPlayingFn(ctx)
}

func (p *playbackApplication) Devices(ctx context.Context) ([]music.AirPlayDevice, error) {
	return p.devicesFn(ctx)
}

func (p *playbackApplication) SetRoute(ctx context.Context, rooms []string) error {
	return p.setRouteFn(ctx, rooms)
}

func (p *playbackApplication) SetVolume(ctx context.Context, room string, value int) error {
	return p.setVolumeFn(ctx, room, value)
}

func (p *playbackApplication) PlayPause(ctx context.Context) error {
	return p.playPauseFn(ctx)
}

func (p *playbackApplication) Pause(ctx context.Context) error {
	return p.pauseFn(ctx)
}

func (p *playbackApplication) Stop(ctx context.Context) error {
	return p.stopFn(ctx)
}

func (p *playbackApplication) Next(ctx context.Context) error {
	return p.nextFn(ctx)
}

func (p *playbackApplication) Previous(ctx context.Context) error {
	return p.previousFn(ctx)
}
