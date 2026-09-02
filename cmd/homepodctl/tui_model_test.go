package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
	tea "github.com/charmbracelet/bubbletea"
)

type fakeTUIPlaybackService struct {
	snapshot    music.PlaybackSnapshot
	snapshotErr error
	actions     []string
	route       []string
	volumeRoom  string
	volume      int
	actionErr   error
}

func (f *fakeTUIPlaybackService) Snapshot(context.Context) (music.PlaybackSnapshot, error) {
	return f.snapshot, f.snapshotErr
}

func (f *fakeTUIPlaybackService) SetRoute(_ context.Context, rooms []string) error {
	f.actions = append(f.actions, "route")
	f.route = append([]string(nil), rooms...)
	return f.actionErr
}

func (f *fakeTUIPlaybackService) SetVolume(_ context.Context, room string, value int) error {
	f.actions = append(f.actions, "volume")
	f.volumeRoom, f.volume = room, value
	return f.actionErr
}

func (f *fakeTUIPlaybackService) PlayPause(context.Context) error {
	f.actions = append(f.actions, "playpause")
	return f.actionErr
}

func (f *fakeTUIPlaybackService) Stop(context.Context) error {
	f.actions = append(f.actions, "stop")
	return f.actionErr
}

func (f *fakeTUIPlaybackService) Next(context.Context) error {
	f.actions = append(f.actions, "next")
	return f.actionErr
}

func (f *fakeTUIPlaybackService) Previous(context.Context) error {
	f.actions = append(f.actions, "previous")
	return f.actionErr
}

func sampleTUISnapshot() music.PlaybackSnapshot {
	bedroom := music.AirPlayDevice{Name: "Bedroom", Kind: "HomePod mini", Available: true, Volume: 25, PersistentID: "B1"}
	living := music.AirPlayDevice{Name: "Living Room", Kind: "HomePod mini", Available: true, Selected: true, Active: true, Volume: 42, PersistentID: "L1"}
	return music.PlaybackSnapshot{
		NowPlaying: music.NowPlaying{
			PlayerState:     "playing",
			PlayerPositionS: 138,
			ShuffleEnabled:  true,
			SongRepeat:      "all",
			PlaylistName:    "Chill Mix",
			Track: music.NowPlayingTrack{
				Name:      "Chihiro",
				Artist:    "Billie Eilish",
				Album:     "HIT ME HARD AND SOFT",
				DurationS: 303,
			},
			Outputs: []music.AirPlayDevice{living},
		},
		Devices: []music.AirPlayDevice{living, bedroom},
	}
}

func readyTUIModel(service *fakeTUIPlaybackService) tuiModel {
	m := newTUIModel(context.Background(), service, tuiOptions{
		refresh:       time.Second,
		actionTimeout: time.Second,
		noColor:       true,
	})
	updated, _ := m.Update(tuiSnapshotMsg{snapshot: sampleTUISnapshot(), at: time.Now(), duration: time.Millisecond})
	return updated.(tuiModel)
}

func TestTUIModelSnapshotSortsRoomsAndPreservesFocus(t *testing.T) {
	service := &fakeTUIPlaybackService{}
	m := readyTUIModel(service)
	if got := m.snapshot.Devices[0].Name; got != "Bedroom" {
		t.Fatalf("first Room=%q, want Bedroom", got)
	}
	if m.focusKey != "id:B1" {
		t.Fatalf("focus=%q, want Bedroom persistent ID", m.focusKey)
	}

	m.focusKey = "id:L1"
	snapshot := sampleTUISnapshot()
	snapshot.Devices = append([]music.AirPlayDevice(nil), snapshot.Devices...)
	updated, _ := m.Update(tuiSnapshotMsg{snapshot: snapshot, at: time.Now()})
	m = updated.(tuiModel)
	if m.focusKey != "id:L1" {
		t.Fatalf("focus=%q, want Living Room preserved", m.focusKey)
	}
}

func TestTUIModelStagesRouteAndRejectsEmptyRoute(t *testing.T) {
	service := &fakeTUIPlaybackService{snapshot: sampleTUISnapshot()}
	m := readyTUIModel(service)
	m.focusKey = "id:L1"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(tuiModel)
	if !m.pendingEdit || m.pending["id:L1"] {
		t.Fatalf("pending=%v edit=%t, want Living Room removed", m.pending, m.pendingEdit)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)
	if cmd != nil || !strings.Contains(m.notice, "at least one Room") {
		t.Fatalf("cmd=%v notice=%q, want empty-route rejection", cmd, m.notice)
	}

	m.focusKey = "id:B1"
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(tuiModel)
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)
	if cmd == nil || !m.busy {
		t.Fatalf("expected route action, busy=%t", m.busy)
	}
	msg := cmd().(tuiActionMsg)
	if msg.err != nil || strings.Join(service.route, ",") != "Bedroom" {
		t.Fatalf("route=%v err=%v, want Bedroom", service.route, msg.err)
	}
	updated, _ = m.Update(msg)
	m = updated.(tuiModel)
	if m.confirmation == nil || !m.pendingEdit || !strings.Contains(m.notice, "confirming") {
		t.Fatalf("confirmation=%v edit=%t notice=%q, want pending confirmation", m.confirmation, m.pendingEdit, m.notice)
	}

	updated, _ = m.Update(tuiSnapshotMsg{snapshot: sampleTUISnapshot(), at: time.Now()})
	m = updated.(tuiModel)
	if m.confirmation == nil || !m.pendingEdit {
		t.Fatal("old route snapshot prematurely confirmed the change")
	}
	confirmed := sampleTUISnapshot()
	for index := range confirmed.Devices {
		confirmed.Devices[index].Selected = confirmed.Devices[index].Name == "Bedroom"
	}
	updated, _ = m.Update(tuiSnapshotMsg{snapshot: confirmed, at: time.Now()})
	m = updated.(tuiModel)
	if m.confirmation != nil || m.pendingEdit || !strings.Contains(m.notice, "completed") {
		t.Fatalf("confirmation=%v edit=%t notice=%q, want confirmed route", m.confirmation, m.pendingEdit, m.notice)
	}
}

func TestTUIModelPreflightsRouteAgainstExternalChanges(t *testing.T) {
	service := &fakeTUIPlaybackService{snapshot: sampleTUISnapshot()}
	m := readyTUIModel(service)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(tuiModel)
	if !m.pendingEdit {
		t.Fatal("expected staged Bedroom addition")
	}
	for index := range service.snapshot.Devices {
		service.snapshot.Devices[index].Selected = service.snapshot.Devices[index].Name == "Bedroom"
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)
	message := cmd().(tuiActionMsg)
	if message.conflict == nil || len(service.route) != 0 {
		t.Fatalf("conflict=%v route=%v, want no route mutation", message.conflict, service.route)
	}
	updated, _ = m.Update(message)
	m = updated.(tuiModel)
	if m.pendingEdit || !strings.Contains(m.notice, "changed externally") || !m.pending["id:B1"] {
		t.Fatalf("pending=%v edit=%t notice=%q", m.pending, m.pendingEdit, m.notice)
	}
}

func TestTUIModelResetsPendingRouteAfterExternalChange(t *testing.T) {
	service := &fakeTUIPlaybackService{}
	m := readyTUIModel(service)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(tuiModel)
	if !m.pendingEdit {
		t.Fatal("expected pending route edit")
	}

	snapshot := sampleTUISnapshot()
	for index := range snapshot.Devices {
		snapshot.Devices[index].Selected = snapshot.Devices[index].Name == "Bedroom"
	}
	updated, _ = m.Update(tuiSnapshotMsg{snapshot: snapshot, at: time.Now()})
	m = updated.(tuiModel)
	if m.pendingEdit || !strings.Contains(m.notice, "changed externally") || !m.pending["id:B1"] {
		t.Fatalf("pending=%v edit=%t notice=%q", m.pending, m.pendingEdit, m.notice)
	}
}

func TestTUIModelStaleSnapshotDisablesMutations(t *testing.T) {
	service := &fakeTUIPlaybackService{}
	m := readyTUIModel(service)
	updated, _ := m.Update(tuiSnapshotMsg{err: errors.New("boom"), at: time.Now()})
	m = updated.(tuiModel)
	if !m.stale || m.snapshot == nil {
		t.Fatalf("stale=%t snapshot=%v, want retained stale snapshot", m.stale, m.snapshot)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(tuiModel)
	if cmd != nil || len(service.actions) != 0 || !strings.Contains(m.notice, "unavailable") {
		t.Fatalf("cmd=%v actions=%v notice=%q", cmd, service.actions, m.notice)
	}
}

func TestTUIModelVolumeActionUsesFocusedRoomAndRefreshes(t *testing.T) {
	service := &fakeTUIPlaybackService{snapshot: sampleTUISnapshot()}
	m := readyTUIModel(service)
	m.focusKey = "id:L1"

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	m = updated.(tuiModel)
	if cmd == nil || !m.busy {
		t.Fatal("expected busy volume action")
	}
	message := cmd()
	if service.volumeRoom != "Living Room" || service.volume != 47 {
		t.Fatalf("volume target=%q value=%d", service.volumeRoom, service.volume)
	}
	updated, refreshCmd := m.Update(message)
	m = updated.(tuiModel)
	if refreshCmd == nil || !m.refreshing || m.busy {
		t.Fatalf("refreshing=%t busy=%t cmd=%v", m.refreshing, m.busy, refreshCmd)
	}
	if m.confirmation == nil || !strings.Contains(m.notice, "confirming") {
		t.Fatalf("confirmation=%v notice=%q", m.confirmation, m.notice)
	}
	updated, _ = m.Update(tuiSnapshotMsg{snapshot: sampleTUISnapshot(), at: time.Now()})
	m = updated.(tuiModel)
	updated, blockedCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(tuiModel)
	if blockedCmd != nil || !strings.Contains(m.notice, "unavailable") {
		t.Fatalf("cmd=%v notice=%q, want mutation blocked during confirmation", blockedCmd, m.notice)
	}

	confirmed := sampleTUISnapshot()
	for index := range confirmed.Devices {
		if confirmed.Devices[index].Name == "Living Room" {
			confirmed.Devices[index].Volume = 47
		}
	}
	updated, _ = m.Update(tuiSnapshotMsg{snapshot: confirmed, at: time.Now()})
	m = updated.(tuiModel)
	if m.confirmation != nil || !strings.Contains(m.notice, "completed") {
		t.Fatalf("confirmation=%v notice=%q, want confirmed volume", m.confirmation, m.notice)
	}
}

func TestTUIModelVolumeConfirmationExpiryPreservesPendingRoute(t *testing.T) {
	m := readyTUIModel(&fakeTUIPlaybackService{})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(tuiModel)
	if !m.pendingEdit {
		t.Fatal("expected pending route edit")
	}
	deadline := time.Now()
	m.confirmation = &tuiConfirmation{
		kind:           tuiConfirmVolume,
		action:         "Set Bedroom volume",
		deviceKey:      "id:B1",
		expectedVolume: 30,
		deadline:       deadline,
	}

	updated, _ = m.Update(tuiSnapshotMsg{snapshot: sampleTUISnapshot(), at: deadline.Add(time.Millisecond)})
	m = updated.(tuiModel)
	if m.confirmation != nil || !m.pendingEdit || !strings.Contains(m.notice, "not confirmed") {
		t.Fatalf("confirmation=%v edit=%t notice=%q", m.confirmation, m.pendingEdit, m.notice)
	}
}

func TestTUIModelIgnoresSupersededRefreshTick(t *testing.T) {
	service := &fakeTUIPlaybackService{}
	m := readyTUIModel(service)
	oldGeneration := m.refreshGeneration

	updated, refreshCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(tuiModel)
	if refreshCmd == nil {
		t.Fatal("manual refresh did not start")
	}
	updated, _ = m.Update(tuiSnapshotMsg{snapshot: sampleTUISnapshot(), at: time.Now()})
	m = updated.(tuiModel)
	updated, cmd := m.Update(tuiRefreshTickMsg{generation: oldGeneration})
	m = updated.(tuiModel)
	if cmd != nil || m.refreshing {
		t.Fatalf("superseded tick started refresh: cmd=%v refreshing=%t", cmd, m.refreshing)
	}
}

func TestTUIModelSchedulesTickForReturnedGeneration(t *testing.T) {
	m := newTUIModel(context.Background(), &fakeTUIPlaybackService{}, tuiOptions{
		refresh:       time.Millisecond,
		actionTimeout: time.Second,
		noColor:       true,
	})
	updated, tickCmd := m.Update(tuiSnapshotMsg{snapshot: sampleTUISnapshot(), at: time.Now()})
	m = updated.(tuiModel)
	if tickCmd == nil {
		t.Fatal("snapshot did not schedule next refresh")
	}
	tick := tickCmd().(tuiRefreshTickMsg)
	if tick.generation != m.refreshGeneration {
		t.Fatalf("tick generation=%d model generation=%d", tick.generation, m.refreshGeneration)
	}
}

func TestTUIModelClearsRefreshFailureNoticeAfterRecovery(t *testing.T) {
	m := readyTUIModel(&fakeTUIPlaybackService{})
	updated, _ := m.Update(tuiSnapshotMsg{err: errors.New("boom"), at: time.Now()})
	m = updated.(tuiModel)
	if !strings.Contains(m.notice, "Refresh failed") {
		t.Fatalf("notice=%q, want refresh failure", m.notice)
	}

	updated, _ = m.Update(tuiSnapshotMsg{snapshot: sampleTUISnapshot(), at: time.Now()})
	m = updated.(tuiModel)
	if m.notice != "" || m.stale {
		t.Fatalf("notice=%q stale=%t, want clean recovery", m.notice, m.stale)
	}
}

func TestTUIModelDoesNotInventPlaylistForStoppedMusic(t *testing.T) {
	service := &fakeTUIPlaybackService{}
	m := readyTUIModel(service)
	m.snapshot.NowPlaying.PlayerState = "stopped"
	m.snapshot.NowPlaying.Track = music.NowPlayingTrack{}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(tuiModel)
	if cmd != nil || len(service.actions) != 0 || !strings.Contains(m.notice, "homepodctl play") {
		t.Fatalf("cmd=%v actions=%v notice=%q", cmd, service.actions, m.notice)
	}
}

func TestTUIViewNoColorAndMinimumSize(t *testing.T) {
	m := readyTUIModel(&fakeTUIPlaybackService{})
	m.width, m.height = 88, 24
	view := m.View()
	for _, want := range []string{"Chihiro", "Living Room", "● ON", "● PLAYING", "space play/pause"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "\x1b[") {
		t.Fatalf("NO_COLOR view contains ANSI escapes: %q", view)
	}

	m.width, m.height = 40, 10
	view = m.View()
	if !strings.Contains(view, "needs at least 48×14") {
		t.Fatalf("minimum-size view=%q", view)
	}
}

func TestTUIViewKeepsRedundantStateLabelsWhenColorEnabled(t *testing.T) {
	m := readyTUIModel(&fakeTUIPlaybackService{})
	m.opts.noColor = false
	m.width, m.height = 88, 24
	view := m.View()
	for _, label := range []string{"PLAYING", "ON", "LIVE"} {
		if !strings.Contains(view, label) {
			t.Fatalf("colored view missing text label %q", label)
		}
	}
}

func TestTUIViewCompactLayoutShowsRequiredStateAndFocusedRoom(t *testing.T) {
	m := readyTUIModel(&fakeTUIPlaybackService{})
	for index := 0; index < 5; index++ {
		m.snapshot.Devices = append(m.snapshot.Devices, music.AirPlayDevice{
			Name:         "Room " + string(rune('A'+index)),
			Kind:         "Speaker",
			Available:    true,
			Volume:       10 + index,
			PersistentID: "R" + string(rune('A'+index)),
		})
	}
	m.focusKey = "id:RE"
	m.width, m.height = 48, 14
	view := m.View()
	for _, want := range []string{"HIT ME", "Playlist", "SHUFFLE ON", "REPEAT ALL", "KIND", "AUDIO", "Room E", "IDLE", "14%"} {
		if !strings.Contains(view, want) {
			t.Fatalf("compact view missing %q:\n%s", want, view)
		}
	}
}
