package music

import (
	"context"
	"errors"
	"testing"
)

func TestMutationsNeverRetryAmbiguousFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"next", NextTrack}, {"previous", PreviousTrack}, {"toggle", PlayPause},
		{"playlist", func(ctx context.Context) error { return PlayUserPlaylistByPersistentID(ctx, "ABC") }}, {"route", func(ctx context.Context) error { return SetCurrentAirPlayDevices(ctx, []string{"Room"}) }},
		{"volume", func(ctx context.Context) error { return SetAirPlayDeviceVolume(ctx, "Room", 20) }}, {"shuffle", func(ctx context.Context) error { return SetShuffleEnabled(ctx, true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := runAppleScriptExec
			defer func() { runAppleScriptExec = old }()
			calls := 0
			runAppleScriptExec = func(context.Context, string) ([]byte, error) {
				calls++
				return []byte("AppleEvent timed out (-1712)"), errors.New("exit 1")
			}
			if err := tc.run(context.Background()); err == nil {
				t.Fatal("expected failure")
			}
			if calls != 1 {
				t.Fatalf("mutation attempts=%d, want 1", calls)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			calls = 0
			if err := tc.run(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel error=%v", err)
			}
			if calls != 0 {
				t.Fatalf("cancelled mutation dispatched %d times", calls)
			}
		})
	}
}
