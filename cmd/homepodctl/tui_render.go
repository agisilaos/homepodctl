package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/charmbracelet/lipgloss"
)

const (
	tuiColorMidnight     = "#12151c"
	tuiColorMidnightDeep = "#090b10"
	tuiColorFocus        = "#202b3b"
	tuiColorText         = "#f5f6f8"
	tuiColorMuted        = "#a4adba"
	tuiColorDim          = "#737d8b"
	tuiColorMusic        = "#fa2d65"
	tuiColorBlue         = "#55a8e8"
	tuiColorYellow       = "#f0cf4b"
	tuiColorOrange       = "#ec7b39"
	tuiColorGreen        = "#60d394"
)

func (m tuiModel) View() string {
	width := m.width
	if width <= 0 {
		width = 80
	}
	height := m.height
	if height <= 0 {
		height = 24
	}
	if width < 48 || height < 14 {
		return m.renderMinimumSize(width, height)
	}

	var lines []string
	lines = append(lines, m.renderTitle(width))
	if m.opts.nativeDefault {
		lines = append(lines, m.paint(tuiColorYellow, "! NATIVE DEFAULT · this TUI observes Music/AirPlay only"))
	}
	if m.showHelp {
		lines = append(lines, m.renderHelp(width)...)
		lines = append(lines, m.renderStatus(width))
		return m.renderCanvas(lines, width, height)
	}
	if m.snapshot == nil {
		lines = append(lines, "", m.paint(tuiColorMuted, "NOW PLAYING"))
		lines = append(lines, m.strong("Waiting for Music.app…"))
		if m.lastError != "" {
			lines = append(lines, m.paint(tuiColorOrange, m.lastError))
		}
		lines = append(lines, "", m.paint(tuiColorMuted, "ROOMS"), m.paint(tuiColorDim, "No current device snapshot"))
		lines = append(lines, "", m.renderNotice(width), m.renderStatus(width))
		return m.renderCanvas(lines, width, height)
	}

	lines = append(lines, "")
	lines = append(lines, m.renderNowPlaying(width)...)
	lines = append(lines, "")
	reservedRows := 2
	if m.pendingEdit {
		reservedRows++
	}
	lines = append(lines, m.renderRooms(width, height-len(lines)-reservedRows)...)
	if m.pendingEdit {
		lines = append(lines, m.renderPending(width))
	}
	lines = append(lines, m.renderNotice(width), m.renderStatus(width))
	return m.renderCanvas(lines, width, height)
}

func (m tuiModel) renderMinimumSize(width, height int) string {
	message := fmt.Sprintf("homepodctl tui needs at least 48×14 (current %d×%d)", width, height)
	return m.renderCanvas([]string{
		m.strong("♪ homepodctl"),
		"",
		m.paint(tuiColorYellow, clipRunes(message, maxInt(width, 1))),
		m.paint(tuiColorMuted, "Resize the terminal or press q to quit."),
	}, maxInt(width, 1), maxInt(height, 1))
}

func (m tuiModel) renderTitle(width int) string {
	left := m.paint(tuiColorMusic, "♪") + " " + m.strong("homepodctl") + " " + m.paint(tuiColorYellow, "PREVIEW")
	state := "AIRPLAY · LIVE"
	color := tuiColorGreen
	switch {
	case m.snapshot == nil:
		state, color = "DISCONNECTED", tuiColorOrange
	case m.stale:
		state, color = "AIRPLAY · STALE", tuiColorOrange
	case m.refreshing:
		state, color = "AIRPLAY · REFRESHING", tuiColorBlue
	case m.busy:
		state, color = "AIRPLAY · WORKING", tuiColorYellow
	}
	return joinTUISides(left, m.paint(color, "● "+state), width)
}

func (m tuiModel) renderNowPlaying(width int) []string {
	now := m.snapshot.NowPlaying
	track := strings.TrimSpace(now.Track.Name)
	if track == "" {
		track = "Nothing playing"
	}
	state := strings.ToUpper(strings.TrimSpace(now.PlayerState))
	if state == "" {
		state = "UNKNOWN"
	}
	stateColor := tuiColorMuted
	if state == "PLAYING" {
		stateColor = tuiColorMusic
	}
	lines := []string{
		m.paint(tuiColorMuted, "NOW PLAYING"),
		joinTUISides(m.strong(clipRunes(track, maxInt(width-20, 12))), m.paint(stateColor, "▶ "+state), width),
	}
	meta := compactJoin(" · ", strings.TrimSpace(now.Track.Artist), strings.TrimSpace(now.Track.Album), playlistLabel(now.PlaylistName))
	if meta != "" {
		lines = append(lines, m.paint(tuiColorMuted, clipRunes(meta, width)))
	}
	lines = append(lines, m.renderProgress(now, width))
	shuffle := "SHUFFLE OFF"
	if now.ShuffleEnabled {
		shuffle = "SHUFFLE ON"
	}
	repeat := "REPEAT " + strings.ToUpper(strings.TrimSpace(now.SongRepeat))
	if strings.TrimSpace(now.SongRepeat) == "" {
		repeat = "REPEAT OFF"
	}
	lines = append(lines, m.paint(tuiColorMuted, shuffle+"   "+repeat))
	return lines
}

func playlistLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return "Playlist: " + name
}

func (m tuiModel) renderProgress(now music.NowPlaying, width int) string {
	position := formatClock(now.PlayerPositionS)
	duration := formatClock(now.Track.DurationS)
	barWidth := maxInt(width-len(position)-len(duration)-4, 10)
	ratio := 0.0
	if now.Track.DurationS > 0 {
		ratio = math.Max(0, math.Min(1, now.PlayerPositionS/now.Track.DurationS))
	}
	filled := int(math.Round(float64(barWidth) * ratio))
	bar := m.paint(tuiColorMusic, strings.Repeat("━", filled)) + m.paint(tuiColorDim, strings.Repeat("─", barWidth-filled))
	return m.paint(tuiColorMuted, position) + " " + bar + " " + m.paint(tuiColorMuted, duration)
}

func (m tuiModel) renderRooms(width, availableRows int) []string {
	selected := 0
	for _, device := range m.snapshot.Devices {
		if device.Selected {
			selected++
		}
	}
	if len(m.snapshot.Devices) == 0 {
		return []string{m.paint(tuiColorMuted, "ROOMS"), m.paint(tuiColorDim, "No AirPlay Rooms discovered")}
	}
	rowCapacity := maxInt(availableRows-2, 1)
	if availableRows < 3 {
		rowCapacity = 1
	}
	rowCapacity = minInt(rowCapacity, len(m.snapshot.Devices))
	start := m.focusIndex() - rowCapacity/2
	start = maxInt(0, minInt(start, len(m.snapshot.Devices)-rowCapacity))
	end := start + rowCapacity
	rangeLabel := ""
	if rowCapacity < len(m.snapshot.Devices) {
		rangeLabel = fmt.Sprintf(" · %d–%d/%d", start+1, end, len(m.snapshot.Devices))
	}
	heading := joinTUISides(m.paint(tuiColorMuted, "ROOMS"), m.paint(tuiColorBlue, fmt.Sprintf("CURRENT ROUTE · %d OUTPUTS%s", selected, rangeLabel)), width)
	lines := []string{heading}
	if availableRows >= 3 {
		lines = append(lines, m.renderRoomHeader(width))
	}
	for _, device := range m.snapshot.Devices[start:end] {
		lines = append(lines, m.renderRoom(device, width))
	}
	return lines
}

func (m tuiModel) renderRoomHeader(width int) string {
	if width < 64 {
		nameWidth := maxInt(width-36, 8)
		return m.paint(tuiColorDim, "  "+fitTUIColumn("ROOM", nameWidth)+" "+fitTUIColumn("KIND", 8)+" "+fitTUIColumn("ROUTE", 8)+" "+fitTUIColumn("AUDIO", 9)+" "+rightTUIColumn("VOL", 5))
	}
	nameWidth := maxInt(width-48, 14)
	return m.paint(tuiColorDim, "  "+fitTUIColumn("ROOM", nameWidth)+" "+fitTUIColumn("KIND", 14)+" "+fitTUIColumn("ROUTE", 9)+" "+fitTUIColumn("AUDIO", 11)+" "+rightTUIColumn("VOL", 5))
}

func (m tuiModel) renderRoom(device music.AirPlayDevice, width int) string {
	cursor := " "
	focused := deviceKey(device) == m.focusKey
	if focused {
		cursor = "›"
	}
	routeText, routeColor := m.roomRouteState(device)
	audioText, audioColor := roomAudioState(device)
	volume := "—"
	if device.Available {
		volume = fmt.Sprintf("%d%%", device.Volume)
	}
	var row string
	if width < 64 {
		nameWidth := maxInt(width-36, 8)
		kind := strings.TrimSpace(device.Kind)
		if kind == "" {
			kind = "unknown"
		}
		row = cursor + " " + fitTUIColumn(device.Name, nameWidth) + " " + fitTUIColumn(kind, 8) + " " + fitTUIColumn(m.paint(routeColor, routeText), 8) + " " + fitTUIColumn(m.paint(audioColor, audioText), 9) + " " + rightTUIColumn(volume, 5)
	} else {
		nameWidth := maxInt(width-48, 14)
		kind := strings.TrimSpace(device.Kind)
		if kind == "" {
			kind = "unknown"
		}
		row = cursor + " " + fitTUIColumn(device.Name, nameWidth) + " " + fitTUIColumn(kind, 14) + " " + fitTUIColumn(m.paint(routeColor, routeText), 9) + " " + fitTUIColumn(m.paint(audioColor, audioText), 11) + " " + rightTUIColumn(volume, 5)
	}
	if focused {
		row = m.focus(row)
	}
	return row
}

func (m tuiModel) roomRouteState(device music.AirPlayDevice) (string, string) {
	pending := m.pending[deviceKey(device)]
	if m.pendingEdit && pending != device.Selected {
		if pending {
			return "+ ADD", tuiColorYellow
		}
		return "− REMOVE", tuiColorYellow
	}
	if device.Selected {
		return "● ON", tuiColorBlue
	}
	return "− OFF", tuiColorMuted
}

func roomAudioState(device music.AirPlayDevice) (string, string) {
	switch {
	case !device.Available:
		return "! UNAVAIL", tuiColorOrange
	case device.Active:
		return "● PLAYING", tuiColorMusic
	default:
		return "○ IDLE", tuiColorMuted
	}
}

func (m tuiModel) renderPending(width int) string {
	var changes []string
	for _, device := range m.snapshot.Devices {
		pending := m.pending[deviceKey(device)]
		if pending == device.Selected {
			continue
		}
		prefix := "+ "
		if !pending {
			prefix = "− "
		}
		changes = append(changes, prefix+device.Name)
	}
	left := "◆ PENDING ROUTE  " + strings.Join(changes, ", ")
	return joinTUISides(m.paint(tuiColorYellow, clipRunes(left, maxInt(width-18, 12))), m.paint(tuiColorYellow, "ENTER TO APPLY"), width)
}

func (m tuiModel) renderNotice(width int) string {
	if strings.TrimSpace(m.notice) == "" {
		return ""
	}
	color := tuiColorMuted
	symbol := "·"
	switch m.noticeKind {
	case tuiNoticeWarning:
		color, symbol = tuiColorYellow, "!"
	case tuiNoticeError:
		color, symbol = tuiColorOrange, "!"
	}
	return m.paint(color, clipRunes(symbol+" "+m.notice, width))
}

func (m tuiModel) renderStatus(width int) string {
	left := "● Music connected"
	color := tuiColorGreen
	switch {
	case m.snapshot == nil:
		left, color = "! Music disconnected · retrying", tuiColorOrange
	case m.stale:
		left, color = "! Snapshot stale · retrying", tuiColorOrange
	case !m.lastUpdated.IsZero():
		left = "● Music connected · refreshed " + relativeRefreshAge(m.lastUpdated)
	}
	if m.opts.verbose && m.lastAction != "" {
		left += fmt.Sprintf(" · %s %s", m.lastAction, m.lastTiming.Round(time.Millisecond))
	}
	right := "space play/pause  x route  ± volume  ? help  q quit"
	if width < 110 {
		right = "space play/pause  ? help  q quit"
	}
	if width < 72 {
		right = "? help  q quit"
	}
	return joinTUISides(m.paint(color, left), m.paint(tuiColorMuted, right), width)
}

func (m tuiModel) renderHelp(width int) []string {
	entries := []string{
		"space  play/pause current Music session",
		"n / b  next / previous track",
		"s      stop playback",
		"↑↓ j/k focus a Room",
		"x      stage focused Room in/out of route",
		"enter  apply pending route",
		"+ / -  adjust focused Room volume by 5",
		"r      refresh now",
		"?      close help",
		"q      quit",
	}
	lines := []string{"", m.paint(tuiColorMuted, "KEYS")}
	for _, entry := range entries {
		lines = append(lines, clipRunes(entry, width))
	}
	lines = append(lines, "", m.paint(tuiColorMuted, "Colors: pink playback · blue focus/route · yellow pending · orange errors · green live"))
	return lines
}

func (m tuiModel) renderCanvas(lines []string, width, height int) string {
	if len(lines) > height {
		last := lines[len(lines)-1]
		lines = append(append([]string(nil), lines[:height-1]...), last)
	}
	for len(lines) < height {
		last := lines[len(lines)-1]
		lines[len(lines)-1] = ""
		lines = append(lines, last)
	}
	for index, line := range lines {
		line = clipStyledLine(line, width)
		if m.opts.noColor {
			lines[index] = padTUILine(line, width)
			continue
		}
		background := tuiColorMidnight
		if index == 0 || index == len(lines)-1 {
			background = tuiColorMidnightDeep
		}
		lines[index] = lipgloss.NewStyle().Width(width).Foreground(lipgloss.Color(tuiColorText)).Background(lipgloss.Color(background)).Render(line)
	}
	return strings.Join(lines, "\n")
}

func (m tuiModel) paint(color, value string) string {
	if m.opts.noColor {
		return value
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(value)
}

func (m tuiModel) strong(value string) string {
	if m.opts.noColor {
		return value
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(tuiColorText)).Bold(true).Render(value)
}

func (m tuiModel) focus(value string) string {
	if m.opts.noColor {
		return value
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(tuiColorText)).Background(lipgloss.Color(tuiColorFocus)).Render(value)
}

func joinTUISides(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return clipStyledLine(left, width)
	}
	return left + strings.Repeat(" ", gap) + right
}

func clipStyledLine(value string, width int) string {
	if width <= 0 || lipgloss.Width(value) <= width {
		return value
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(value)
}

func padTUILine(value string, width int) string {
	gap := width - lipgloss.Width(value)
	if gap <= 0 {
		return value
	}
	return value + strings.Repeat(" ", gap)
}

func clipRunes(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	var clipped strings.Builder
	used := 0
	for _, r := range value {
		runeWidth := lipgloss.Width(string(r))
		if used+runeWidth > width-1 {
			break
		}
		clipped.WriteRune(r)
		used += runeWidth
	}
	return clipped.String() + "…"
}

func fitTUIColumn(value string, width int) string {
	value = clipStyledLine(value, width)
	return value + strings.Repeat(" ", maxInt(width-lipgloss.Width(value), 0))
}

func rightTUIColumn(value string, width int) string {
	value = clipStyledLine(value, width)
	return strings.Repeat(" ", maxInt(width-lipgloss.Width(value), 0)) + value
}

func compactJoin(separator string, values ...string) string {
	var nonEmpty []string
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			nonEmpty = append(nonEmpty, value)
		}
	}
	return strings.Join(nonEmpty, separator)
}

func relativeRefreshAge(at time.Time) string {
	age := time.Since(at)
	if age < 0 || age < time.Second {
		return "just now"
	}
	return age.Round(time.Second).String() + " ago"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
