package music

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseFloatLoose(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want float64
	}{
		{"", 0},
		{"0", 0},
		{"1", 1},
		{"  12.5 ", 12.5},
		{"264,0", 264},
		{"not-a-number", 0},
	}

	for _, tc := range cases {
		if got := parseFloatLoose(tc.in); got != tc.want {
			t.Fatalf("parseFloatLoose(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestCanonicalizeName(t *testing.T) {
	t.Parallel()

	// U+FE0F variation selector, plus zero-width joiner U+200D and weird spacing.
	in := "Example  Playlist \uFE0F \u200D🎶"
	got := canonicalizeName(in)
	want := "Example Playlist 🎶"
	if got != want {
		t.Fatalf("canonicalizeName(%q) = %q, want %q", in, got, want)
	}
}

func TestParseBool(t *testing.T) {
	t.Parallel()

	if !parseBool("true") || !parseBool(" yes ") || !parseBool("1") {
		t.Fatalf("expected truthy values to parse true")
	}
	if parseBool("false") || parseBool("") || parseBool("0") || parseBool("no") {
		t.Fatalf("expected falsy values to parse false")
	}
}

func TestPickBestPlaylist(t *testing.T) {
	t.Parallel()

	matches := []UserPlaylist{
		{PersistentID: "1", Name: "Chill"},
		{PersistentID: "2", Name: "Chill Vibes"},
		{PersistentID: "3", Name: "Super Chill Mix"},
		{PersistentID: "4", Name: "CHILL"}, // canonical exact match should still win
	}

	best, ok := PickBestPlaylist("chill", matches)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if best.Name != "Chill" && best.Name != "CHILL" {
		t.Fatalf("best = %q, want exact canonical match", best.Name)
	}

	best, ok = PickBestPlaylist("chill v", matches)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if best.Name != "Chill Vibes" {
		t.Fatalf("best = %q, want %q", best.Name, "Chill Vibes")
	}

	best, ok = PickBestPlaylist("spr chll", matches) // subsequence should match Super Chill Mix
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if best.Name != "Super Chill Mix" {
		t.Fatalf("best = %q, want %q", best.Name, "Super Chill Mix")
	}
}

func TestShouldRetryAppleScript(t *testing.T) {
	t.Parallel()

	if !shouldRetryAppleScript(errors.New("exit"), "AppleEvent timed out (-1712)") {
		t.Fatalf("expected timeout output to be retryable")
	}
	if shouldRetryAppleScript(context.DeadlineExceeded, "timed out") {
		t.Fatalf("context deadline should not be retried")
	}
	if shouldRetryAppleScript(errors.New("exit"), "Not authorised to send Apple events") {
		t.Fatalf("auth errors should not be retried")
	}
}

func BenchmarkCanonicalizeName(b *testing.B) {
	in := "Songs  I've been obsessed recently pt. 2 \uFE0F\u200D🎶"
	for i := 0; i < b.N; i++ {
		_ = canonicalizeName(in)
	}
}

func BenchmarkScoreMatchPlaylistSet(b *testing.B) {
	query := "chill morning"
	candidates := []string{
		"Chill Morning",
		"Morning Focus",
		"Deep Focus Mix",
		"Songs I've been obsessed recently pt. 2",
		"CHILL",
		"Chill Vibes",
		"Morning Chill Set",
		"Winddown",
		"Party Starters",
		"Jazz Study",
	}
	needle := canonicalizeName(query)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, c := range candidates {
			_ = scoreMatch(needle, canonicalizeName(c))
		}
	}
}

func TestRunAppleScript_RetriesTransientThenSucceeds(t *testing.T) {
	origExec := runAppleScriptExec
	origSleep := sleepWithContextFn
	t.Cleanup(func() {
		runAppleScriptExec = origExec
		sleepWithContextFn = origSleep
	})

	attempts := 0
	runAppleScriptExec = func(context.Context, string) ([]byte, error) {
		attempts++
		if attempts < 3 {
			return []byte("AppleEvent timed out (-1712)"), errors.New("boom")
		}
		return []byte("ok"), nil
	}
	sleepWithContextFn = func(context.Context, time.Duration) error { return nil }

	out, err := runAppleScript(context.Background(), `return "ok"`)
	if err != nil {
		t.Fatalf("runAppleScript: %v", err)
	}
	if out != "ok" {
		t.Fatalf("out=%q, want ok", out)
	}
	if attempts != 3 {
		t.Fatalf("attempts=%d, want 3", attempts)
	}
}

func TestRunAppleScript_FailFastOnPermanentError(t *testing.T) {
	origExec := runAppleScriptExec
	origSleep := sleepWithContextFn
	t.Cleanup(func() {
		runAppleScriptExec = origExec
		sleepWithContextFn = origSleep
	})

	attempts := 0
	runAppleScriptExec = func(context.Context, string) ([]byte, error) {
		attempts++
		return []byte("Not authorised to send Apple events"), errors.New("boom")
	}
	sleepWithContextFn = func(context.Context, time.Duration) error { return nil }

	_, err := runAppleScript(context.Background(), `return "nope"`)
	if err == nil {
		t.Fatalf("expected error")
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d, want 1", attempts)
	}
}

func TestListUserPlaylists_QueryAndLimit(t *testing.T) {
	origExec := runAppleScriptExec
	t.Cleanup(func() { runAppleScriptExec = origExec })

	runAppleScriptExec = func(context.Context, string) ([]byte, error) {
		return []byte(strings.Join([]string{
			"AA11\tFocus\ttrue\tfalse",
			"BB22\tDeep Focus\tfalse\tfalse",
			"CC33\tParty\tfalse\ttrue",
			"",
		}, "\n")), nil
	}

	got, err := ListUserPlaylists(context.Background(), "focus", 1)
	if err != nil {
		t.Fatalf("ListUserPlaylists: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got)=%d, want 1", len(got))
	}
	if got[0].PersistentID != "AA11" || got[0].Name != "Focus" || !got[0].Smart || got[0].Genius {
		t.Fatalf("unexpected playlist: %+v", got[0])
	}
}

func TestFindUserPlaylistPersistentIDByName(t *testing.T) {
	origExec := runAppleScriptExec
	t.Cleanup(func() { runAppleScriptExec = origExec })

	runAppleScriptExec = func(context.Context, string) ([]byte, error) {
		return []byte(strings.Join([]string{
			"P001\tFocus\tfalse\tfalse",
			"P002\tDeep Focus\tfalse\tfalse",
			"P003\tFocus Mix\tfalse\tfalse",
			"",
		}, "\n")), nil
	}

	id, err := FindUserPlaylistPersistentIDByName(context.Background(), " Focus ")
	if err != nil {
		t.Fatalf("exact match: %v", err)
	}
	if id != "P001" {
		t.Fatalf("id=%q, want P001", id)
	}

	_, err = FindUserPlaylistPersistentIDByName(context.Background(), "fo")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "ambiguous") {
		t.Fatalf("ambiguous query expected error, got %v", err)
	}

	_, err = FindUserPlaylistPersistentIDByName(context.Background(), "missing")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Fatalf("missing query expected not found, got %v", err)
	}
}

func TestSearchUserPlaylists_Ranking(t *testing.T) {
	origExec := runAppleScriptExec
	t.Cleanup(func() { runAppleScriptExec = origExec })

	runAppleScriptExec = func(context.Context, string) ([]byte, error) {
		return []byte(strings.Join([]string{
			"P001\tChill\tfalse\tfalse",
			"P002\tMorning Chill\tfalse\tfalse",
			"P003\tSuper Chill Mix\tfalse\tfalse",
			"P004\tParty\tfalse\tfalse",
			"",
		}, "\n")), nil
	}

	got, err := SearchUserPlaylists(context.Background(), "chill")
	if err != nil {
		t.Fatalf("SearchUserPlaylists: %v", err)
	}
	if len(got) < 3 {
		t.Fatalf("len(got)=%d, want >=3", len(got))
	}
	if got[0].Name != "Chill" {
		t.Fatalf("top result=%q, want Chill", got[0].Name)
	}
}

func TestListAirPlayDevices_ParsesFields(t *testing.T) {
	origExec := runAppleScriptExec
	t.Cleanup(func() { runAppleScriptExec = origExec })

	runAppleScriptExec = func(context.Context, string) ([]byte, error) {
		return []byte(strings.Join([]string{
			"Bedroom\tHomePod\ttrue\ttrue\ttrue\t35\t192.168.1.12\tPID1",
			"Kitchen\tApple TV\tfalse\tfalse\tfalse\tnot-a-number\t\t",
			"",
		}, "\n")), nil
	}

	got, err := ListAirPlayDevices(context.Background())
	if err != nil {
		t.Fatalf("ListAirPlayDevices: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got)=%d, want 2", len(got))
	}
	if !got[0].Available || !got[0].Selected || got[0].Volume != 35 {
		t.Fatalf("unexpected first device: %+v", got[0])
	}
	if got[1].Volume != 0 || got[1].NetworkAddress != "" || got[1].PersistentID != "" {
		t.Fatalf("unexpected second device parsing: %+v", got[1])
	}
}

func TestGetNowPlaying_SelectedOutputsAndDeviceFailure(t *testing.T) {
	origExec := runAppleScriptExec
	t.Cleanup(func() { runAppleScriptExec = origExec })

	calls := 0
	runAppleScriptExec = func(_ context.Context, script string) ([]byte, error) {
		calls++
		if strings.Contains(script, "set ps to (player state as text)") {
			return []byte("playing\t12.5\ttrue\tall\tFocus\tPL123\tTrack\tArtist\tAlbum\t240.0\tT123"), nil
		}
		if strings.Contains(script, "every AirPlay device") {
			return []byte(strings.Join([]string{
				"Bedroom\tHomePod\ttrue\ttrue\ttrue\t35\t\tB1",
				"Kitchen\tHomePod\ttrue\tfalse\tfalse\t30\t\tK1",
			}, "\n")), nil
		}
		t.Fatalf("unexpected script call: %s", script)
		return nil, nil
	}

	np, err := GetNowPlaying(context.Background())
	if err != nil {
		t.Fatalf("GetNowPlaying: %v", err)
	}
	if np.PlayerState != "playing" || np.Track.Name != "Track" || np.Track.DurationS != 240 {
		t.Fatalf("unexpected now playing payload: %+v", np)
	}
	if len(np.Outputs) != 1 || np.Outputs[0].Name != "Bedroom" {
		t.Fatalf("selected outputs=%+v, want only Bedroom", np.Outputs)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}

	runAppleScriptExec = func(_ context.Context, script string) ([]byte, error) {
		if strings.Contains(script, "set ps to (player state as text)") {
			return []byte("paused\t0\tfalse\toff\t\t\t\t\t\t0\t"), nil
		}
		if strings.Contains(script, "every AirPlay device") {
			return nil, errors.New("boom")
		}
		return nil, errors.New("unexpected script")
	}

	np, err = GetNowPlaying(context.Background())
	if err != nil {
		t.Fatalf("GetNowPlaying with device error: %v", err)
	}
	if len(np.Outputs) != 0 {
		t.Fatalf("outputs=%v, want empty when device listing fails", np.Outputs)
	}
}
