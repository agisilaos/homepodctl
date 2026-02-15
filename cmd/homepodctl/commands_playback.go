package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/agisilaos/homepodctl/internal/native"
)

func setVolumeForRooms(ctx context.Context, rooms []string, value int) error {
	for _, room := range rooms {
		if err := setDeviceVolume(ctx, room, value); err != nil {
			return err
		}
	}
	return nil
}

func resolveNativePlaylistShortcut(cfg *native.Config, room, playlist string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("native backend requires config")
	}
	if cfg.Native.Playlists == nil {
		return "", fmt.Errorf("no native mapping for room=%q playlist=%q", room, playlist)
	}
	m := cfg.Native.Playlists[room]
	shortcut := ""
	if m != nil {
		shortcut = m[playlist]
	}
	if strings.TrimSpace(shortcut) == "" {
		return "", fmt.Errorf("no native mapping for room=%q playlist=%q", room, playlist)
	}
	return shortcut, nil
}

func resolveNativeVolumeShortcut(cfg *native.Config, room string, value int) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("native backend requires config")
	}
	if cfg.Native.VolumeShortcuts == nil {
		return "", fmt.Errorf("no native volume mapping for room=%q value=%d", room, value)
	}
	m := cfg.Native.VolumeShortcuts[room]
	shortcut := ""
	if m != nil {
		shortcut = m[fmt.Sprint(value)]
	}
	if strings.TrimSpace(shortcut) == "" {
		return "", fmt.Errorf("no native volume mapping for room=%q value=%d", room, value)
	}
	return shortcut, nil
}

func runNativePlaylistShortcuts(ctx context.Context, cfg *native.Config, rooms []string, playlist string) error {
	for _, room := range rooms {
		shortcut, err := resolveNativePlaylistShortcut(cfg, room, playlist)
		if err != nil {
			return err
		}
		if err := runNativeShortcut(ctx, shortcut); err != nil {
			return err
		}
	}
	return nil
}

func runNativeVolumeShortcuts(ctx context.Context, cfg *native.Config, rooms []string, value int) error {
	for _, room := range rooms {
		shortcut, err := resolveNativeVolumeShortcut(cfg, room, value)
		if err != nil {
			return err
		}
		if err := runNativeShortcut(ctx, shortcut); err != nil {
			return err
		}
	}
	return nil
}

func validateAirplayVolumeSelection(volumeExplicit bool, volume int, rooms []string) error {
	if volumeExplicit && volume >= 0 && len(rooms) == 0 {
		return usageErrf("cannot set volume without rooms (pass --room <name> or select outputs first via `homepodctl out set`)")
	}
	return nil
}

func inferSelectedOutputs(ctx context.Context) []string {
	np, err := getNowPlaying(ctx)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var rooms []string
	for _, o := range np.Outputs {
		name := strings.TrimSpace(o.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		rooms = append(rooms, name)
	}
	return rooms
}
