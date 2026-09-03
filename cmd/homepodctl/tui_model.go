package main

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
	tea "github.com/charmbracelet/bubbletea"
)

type tuiPlaybackService interface {
	Snapshot(context.Context) (music.PlaybackSnapshot, error)
	SetRoute(context.Context, []string) error
	SetVolume(context.Context, string, int) error
	PlayPause(context.Context) error
	Stop(context.Context) error
	Next(context.Context) error
	Previous(context.Context) error
}

type tuiNoticeKind uint8

const (
	tuiNoticeInfo tuiNoticeKind = iota
	tuiNoticeWarning
	tuiNoticeError
)

type tuiModel struct {
	ctx     context.Context
	service tuiPlaybackService
	opts    tuiOptions

	width  int
	height int

	snapshot    *music.PlaybackSnapshot
	lastUpdated time.Time
	stale       bool
	refreshing  bool
	busy        bool
	showHelp    bool

	focusKey     string
	pending      map[string]bool
	pendingBase  string
	pendingEdit  bool
	confirmation *tuiConfirmation

	refreshGeneration uint64

	notice     string
	noticeKind tuiNoticeKind
	lastAction string
	lastTiming time.Duration
	lastError  string
}

type tuiRefreshTickMsg struct {
	generation uint64
}

type tuiSnapshotMsg struct {
	snapshot music.PlaybackSnapshot
	err      error
	at       time.Time
	duration time.Duration
}

type tuiActionMsg struct {
	action       string
	err          error
	duration     time.Duration
	at           time.Time
	confirmation *tuiConfirmation
	conflict     *music.PlaybackSnapshot
}

type tuiConfirmationKind uint8

const (
	tuiConfirmRoute tuiConfirmationKind = iota
	tuiConfirmVolume
)

type tuiConfirmation struct {
	kind           tuiConfirmationKind
	action         string
	expectedRoute  string
	deviceKey      string
	expectedVolume int
	deadline       time.Time
}

func newTUIModel(ctx context.Context, service tuiPlaybackService, opts tuiOptions) tuiModel {
	return tuiModel{
		ctx:        ctx,
		service:    service,
		opts:       opts,
		width:      80,
		height:     24,
		refreshing: true,
		pending:    make(map[string]bool),
	}
}

func (m tuiModel) Init() tea.Cmd {
	return m.snapshotCmd()
}

func (m tuiModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tuiRefreshTickMsg:
		if msg.generation != m.refreshGeneration {
			return m, nil
		}
		if m.busy || m.refreshing {
			return m, nil
		}
		m.refreshing = true
		return m, m.snapshotCmd()
	case tuiSnapshotMsg:
		m.refreshing = false
		m.lastAction = "Refresh"
		m.lastTiming = msg.duration
		if msg.err != nil {
			m.stale = m.snapshot != nil
			m.lastError = tuiErrorText(msg.err, m.opts.verbose)
			m.setNotice("Refresh failed: "+m.lastError, tuiNoticeError)
			nextRefresh := m.nextRefreshCmd()
			return m, nextRefresh
		}

		sort.SliceStable(msg.snapshot.Devices, func(i, j int) bool {
			return strings.ToLower(msg.snapshot.Devices[i].Name) < strings.ToLower(msg.snapshot.Devices[j].Name)
		})
		observedRoute := selectedRouteSignature(msg.snapshot.Devices)
		confirmed := m.confirmation != nil && m.confirmation.matches(msg.snapshot)
		confirmationExpired := m.confirmation != nil && !msg.at.Before(m.confirmation.deadline)
		if confirmed {
			action := m.confirmation.action
			if m.confirmation.kind == tuiConfirmRoute {
				m.pendingEdit = false
				m.pendingBase = ""
			}
			m.confirmation = nil
			if m.opts.quiet {
				m.notice = ""
			} else {
				m.setNotice(action+" completed", tuiNoticeInfo)
			}
		} else if confirmationExpired {
			action := m.confirmation.action
			kind := m.confirmation.kind
			m.confirmation = nil
			if kind == tuiConfirmRoute {
				m.pendingEdit = false
				m.pendingBase = ""
			}
			m.setNotice(action+" was not confirmed by Music", tuiNoticeError)
		} else if m.confirmation == nil && m.pendingEdit && observedRoute != m.pendingBase {
			m.setNotice("Route changed externally; pending edits were reset", tuiNoticeWarning)
			m.pendingEdit = false
			m.pendingBase = ""
		}
		m.snapshot = &msg.snapshot
		m.lastUpdated = msg.at
		m.stale = false
		m.lastError = ""
		if m.noticeKind == tuiNoticeError && strings.HasPrefix(m.notice, "Refresh failed:") {
			m.notice = ""
		}
		if !m.pendingEdit {
			m.resetPendingRoute()
		}
		m.restoreFocus()
		nextRefresh := m.nextRefreshCmd()
		return m, nextRefresh
	case tuiActionMsg:
		m.busy = false
		m.lastAction = msg.action
		m.lastTiming = msg.duration
		if msg.conflict != nil {
			m.acceptSnapshot(*msg.conflict, msg.at)
			m.confirmation = nil
			m.pendingEdit = false
			m.pendingBase = ""
			m.resetPendingRoute()
			m.setNotice("Route changed externally; pending edits were reset", tuiNoticeWarning)
		} else if msg.err != nil {
			m.lastError = tuiErrorText(msg.err, m.opts.verbose)
			m.setNotice(msg.action+" failed: "+m.lastError, tuiNoticeError)
		} else {
			m.lastError = ""
			if msg.confirmation != nil {
				m.confirmation = msg.confirmation
				m.confirmation.deadline = msg.at.Add(m.opts.actionTimeout)
				m.setNotice(msg.action+" requested; confirming with Music…", tuiNoticeInfo)
			} else if m.opts.quiet {
				m.notice = ""
			} else {
				m.setNotice(msg.action+" completed", tuiNoticeInfo)
			}
		}
		m.refreshing = true
		m.refreshGeneration++
		return m, m.snapshotCmd()
	case tea.KeyMsg:
		return m.handleKey(msg)
	default:
		return m, nil
	}
}

func (m tuiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	case "r":
		if m.busy || m.refreshing {
			return m, nil
		}
		m.refreshing = true
		m.refreshGeneration++
		return m, m.snapshotCmd()
	case "up", "k":
		m.moveFocus(-1)
		return m, nil
	case "down", "j":
		m.moveFocus(1)
		return m, nil
	}

	if !m.canMutate() {
		if isMutationKey(key) {
			m.setNotice("Controls are unavailable until Music state is current", tuiNoticeWarning)
		}
		return m, nil
	}

	switch key {
	case " ":
		if strings.EqualFold(m.snapshot.NowPlaying.PlayerState, "stopped") && strings.TrimSpace(m.snapshot.NowPlaying.Track.Name) == "" {
			m.setNotice("Nothing to resume; start a playlist with `homepodctl play`", tuiNoticeWarning)
			return m, nil
		}
		return m.startAction("Play/pause", m.service.PlayPause)
	case "s":
		return m.startAction("Stop", m.service.Stop)
	case "n":
		return m.startAction("Next track", m.service.Next)
	case "b":
		return m.startAction("Previous track", m.service.Previous)
	case "x":
		m.togglePendingRoom()
		return m, nil
	case "enter":
		return m.applyPendingRoute()
	case "+", "=":
		return m.adjustFocusedVolume(5)
	case "-":
		return m.adjustFocusedVolume(-5)
	default:
		return m, nil
	}
}

func isMutationKey(key string) bool {
	switch key {
	case " ", "s", "n", "b", "x", "enter", "+", "=", "-":
		return true
	default:
		return false
	}
}

func (m tuiModel) canMutate() bool {
	return m.snapshot != nil && !m.stale && !m.busy && !m.refreshing && m.confirmation == nil
}

func (m tuiModel) startAction(label string, action func(context.Context) error) (tea.Model, tea.Cmd) {
	m.busy = true
	m.lastAction = label
	m.refreshGeneration++
	return m, m.actionCmd(label, action)
}

func (m tuiModel) applyPendingRoute() (tea.Model, tea.Cmd) {
	if !m.pendingEdit {
		m.setNotice("No pending route changes", tuiNoticeInfo)
		return m, nil
	}
	var rooms []string
	for _, device := range m.snapshot.Devices {
		if m.pending[deviceKey(device)] {
			rooms = append(rooms, device.Name)
		}
	}
	if len(rooms) == 0 {
		m.setNotice("A playback route requires at least one Room", tuiNoticeWarning)
		return m, nil
	}
	m.busy = true
	m.lastAction = "Apply route"
	m.refreshGeneration++
	return m, m.routeActionCmd(rooms, m.pendingBase, pendingRouteSignature(m.pending, m.snapshot.Devices))
}

func (m tuiModel) adjustFocusedVolume(delta int) (tea.Model, tea.Cmd) {
	device, ok := m.focusedDevice()
	if !ok {
		m.setNotice("No Room is available for volume control", tuiNoticeWarning)
		return m, nil
	}
	if !device.Available {
		m.setNotice(device.Name+" is unavailable", tuiNoticeWarning)
		return m, nil
	}
	value := device.Volume + delta
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	if value == device.Volume {
		m.setNotice(device.Name+" volume is already at its limit", tuiNoticeInfo)
		return m, nil
	}
	label := "Set " + device.Name + " volume"
	m.busy = true
	m.lastAction = label
	m.refreshGeneration++
	confirmation := &tuiConfirmation{
		kind:           tuiConfirmVolume,
		action:         label,
		deviceKey:      deviceKey(device),
		expectedVolume: value,
	}
	return m, m.confirmedActionCmd(label, confirmation, func(ctx context.Context) error {
		return m.service.SetVolume(ctx, device.Name, value)
	})
}

func (m *tuiModel) togglePendingRoom() {
	device, ok := m.focusedDevice()
	if !ok {
		m.setNotice("No Room is available for route editing", tuiNoticeWarning)
		return
	}
	if !device.Available {
		m.setNotice(device.Name+" is unavailable", tuiNoticeWarning)
		return
	}
	if !m.pendingEdit {
		m.pendingBase = selectedRouteSignature(m.snapshot.Devices)
	}
	m.pending[deviceKey(device)] = !m.pending[deviceKey(device)]
	m.pendingEdit = pendingRouteDiffers(m.pending, m.snapshot.Devices)
	if !m.pendingEdit {
		m.pendingBase = ""
		m.setNotice("Pending route matches the current route", tuiNoticeInfo)
	}
}

func (m *tuiModel) moveFocus(delta int) {
	if m.snapshot == nil || len(m.snapshot.Devices) == 0 {
		return
	}
	index := m.focusIndex()
	index = (index + delta + len(m.snapshot.Devices)) % len(m.snapshot.Devices)
	m.focusKey = deviceKey(m.snapshot.Devices[index])
}

func (m *tuiModel) restoreFocus() {
	if m.snapshot == nil || len(m.snapshot.Devices) == 0 {
		m.focusKey = ""
		return
	}
	for _, device := range m.snapshot.Devices {
		if deviceKey(device) == m.focusKey {
			return
		}
	}
	m.focusKey = deviceKey(m.snapshot.Devices[0])
}

func (m tuiModel) focusIndex() int {
	if m.snapshot == nil {
		return 0
	}
	for index, device := range m.snapshot.Devices {
		if deviceKey(device) == m.focusKey {
			return index
		}
	}
	return 0
}

func (m tuiModel) focusedDevice() (music.AirPlayDevice, bool) {
	if m.snapshot == nil || len(m.snapshot.Devices) == 0 {
		return music.AirPlayDevice{}, false
	}
	return m.snapshot.Devices[m.focusIndex()], true
}

func (m *tuiModel) resetPendingRoute() {
	m.pending = make(map[string]bool, len(m.snapshot.Devices))
	for _, device := range m.snapshot.Devices {
		m.pending[deviceKey(device)] = device.Selected
	}
}

func (m *tuiModel) setNotice(message string, kind tuiNoticeKind) {
	m.notice, m.noticeKind = message, kind
}

func (m *tuiModel) acceptSnapshot(snapshot music.PlaybackSnapshot, at time.Time) {
	sort.SliceStable(snapshot.Devices, func(i, j int) bool {
		return strings.ToLower(snapshot.Devices[i].Name) < strings.ToLower(snapshot.Devices[j].Name)
	})
	m.snapshot = &snapshot
	m.lastUpdated = at
	m.stale = false
	m.lastError = ""
	m.restoreFocus()
}

func (m tuiModel) snapshotCmd() tea.Cmd {
	return func() tea.Msg {
		started := time.Now()
		ctx, cancel := context.WithTimeout(m.ctx, m.opts.actionTimeout)
		defer cancel()
		snapshot, err := m.service.Snapshot(ctx)
		finished := time.Now()
		return tuiSnapshotMsg{snapshot: snapshot, err: err, at: finished, duration: finished.Sub(started)}
	}
}

func (m tuiModel) actionCmd(label string, action func(context.Context) error) tea.Cmd {
	return m.confirmedActionCmd(label, nil, action)
}

func (m tuiModel) confirmedActionCmd(label string, confirmation *tuiConfirmation, action func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		started := time.Now()
		ctx, cancel := context.WithTimeout(m.ctx, m.opts.actionTimeout)
		defer cancel()
		err := action(ctx)
		finished := time.Now()
		return tuiActionMsg{action: label, err: err, duration: finished.Sub(started), at: finished, confirmation: confirmation}
	}
}

func (m tuiModel) routeActionCmd(rooms []string, baseRoute, expectedRoute string) tea.Cmd {
	return func() tea.Msg {
		started := time.Now()
		ctx, cancel := context.WithTimeout(m.ctx, m.opts.actionTimeout)
		defer cancel()
		snapshot, err := m.service.Snapshot(ctx)
		if err == nil && selectedRouteSignature(snapshot.Devices) != baseRoute {
			finished := time.Now()
			return tuiActionMsg{action: "Apply route", duration: finished.Sub(started), at: finished, conflict: &snapshot}
		}
		if err == nil {
			err = m.service.SetRoute(ctx, rooms)
		}
		finished := time.Now()
		return tuiActionMsg{
			action:   "Apply route",
			err:      err,
			duration: finished.Sub(started),
			at:       finished,
			confirmation: &tuiConfirmation{
				kind:          tuiConfirmRoute,
				action:        "Apply route",
				expectedRoute: expectedRoute,
			},
		}
	}
}

func (m *tuiModel) nextRefreshCmd() tea.Cmd {
	m.refreshGeneration++
	generation := m.refreshGeneration
	return tea.Tick(m.opts.refresh, func(time.Time) tea.Msg {
		return tuiRefreshTickMsg{generation: generation}
	})
}

func deviceKey(device music.AirPlayDevice) string {
	if id := strings.TrimSpace(device.PersistentID); id != "" {
		return "id:" + id
	}
	return "name:" + strings.ToLower(strings.TrimSpace(device.Name))
}

func selectedRouteSignature(devices []music.AirPlayDevice) string {
	var keys []string
	for _, device := range devices {
		if device.Selected {
			keys = append(keys, deviceKey(device))
		}
	}
	sort.Strings(keys)
	return strings.Join(keys, "\x00")
}

func pendingRouteDiffers(pending map[string]bool, devices []music.AirPlayDevice) bool {
	for _, device := range devices {
		if pending[deviceKey(device)] != device.Selected {
			return true
		}
	}
	return false
}

func pendingRouteSignature(pending map[string]bool, devices []music.AirPlayDevice) string {
	var keys []string
	for _, device := range devices {
		if pending[deviceKey(device)] {
			keys = append(keys, deviceKey(device))
		}
	}
	sort.Strings(keys)
	return strings.Join(keys, "\x00")
}

func (confirmation tuiConfirmation) matches(snapshot music.PlaybackSnapshot) bool {
	switch confirmation.kind {
	case tuiConfirmRoute:
		return selectedRouteSignature(snapshot.Devices) == confirmation.expectedRoute
	case tuiConfirmVolume:
		for _, device := range snapshot.Devices {
			if deviceKey(device) == confirmation.deviceKey {
				return device.Volume == confirmation.expectedVolume
			}
		}
	}
	return false
}

func tuiErrorText(err error, verboseOutput bool) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Music did not respond before the operation timed out"
	}
	if !verboseOutput {
		var scriptErr *music.ScriptError
		if errors.As(err, &scriptErr) {
			if friendly := friendlyScriptError(scriptErr.Output); friendly != "" {
				return friendly
			}
			return "Music backend command failed"
		}
	}
	return err.Error()
}
