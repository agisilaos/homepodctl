package music

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"unicode"
)

type AirPlayDevice struct {
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Available      bool   `json:"available"`
	Selected       bool   `json:"selected"`
	Active         bool   `json:"active"`
	Volume         int    `json:"volume"`
	NetworkAddress string `json:"networkAddress,omitempty"`
	PersistentID   string `json:"persistentID,omitempty"`
}

type UserPlaylist struct {
	PersistentID string `json:"persistentID"`
	Name         string `json:"name"`
	Smart        bool   `json:"smart"`
	Genius       bool   `json:"genius"`
}

type Status struct {
	PlayerState string `json:"playerState"`
	TrackName   string `json:"trackName,omitempty"`
	Artist      string `json:"artist,omitempty"`
	Album       string `json:"album,omitempty"`
}

type NowPlaying struct {
	PlayerState     string          `json:"playerState"`
	PlayerPositionS float64         `json:"playerPositionSeconds"`
	ShuffleEnabled  bool            `json:"shuffleEnabled"`
	SongRepeat      string          `json:"songRepeat"`
	PlaylistName    string          `json:"playlistName,omitempty"`
	PlaylistID      string          `json:"playlistPersistentID,omitempty"`
	Track           NowPlayingTrack `json:"track"`
	Outputs         []AirPlayDevice `json:"outputs"`
}

type NowPlayingTrack struct {
	Name         string  `json:"name,omitempty"`
	Artist       string  `json:"artist,omitempty"`
	Album        string  `json:"album,omitempty"`
	DurationS    float64 `json:"durationSeconds"`
	PersistentID string  `json:"persistentID,omitempty"`
}

func ListAirPlayDevices(ctx context.Context) ([]AirPlayDevice, error) {
	out, err := runAppleScript(ctx, `
tell application "Music"
	set out to ""
	repeat with d in (every AirPlay device)
		set out to out & (name of d) & tab & (kind of d as text) & tab & (available of d as text) & tab & (selected of d as text) & tab & (active of d as text) & tab & (sound volume of d as text) & tab & (network address of d as text) & tab & (persistent ID of d as text) & linefeed
	end repeat
	return out
end tell
`)
	if err != nil {
		return nil, err
	}
	var devices []AirPlayDevice
	for _, line := range splitNonEmptyLines(out) {
		parts := strings.Split(line, "\t")
		for len(parts) < 8 {
			parts = append(parts, "")
		}
		vol, _ := strconv.Atoi(strings.TrimSpace(parts[5]))
		devices = append(devices, AirPlayDevice{
			Name:           strings.TrimSpace(parts[0]),
			Kind:           strings.TrimSpace(parts[1]),
			Available:      parseBool(parts[2]),
			Selected:       parseBool(parts[3]),
			Active:         parseBool(parts[4]),
			Volume:         vol,
			NetworkAddress: strings.TrimSpace(parts[6]),
			PersistentID:   strings.TrimSpace(parts[7]),
		})
	}
	return devices, nil
}

func SetCurrentAirPlayDevices(ctx context.Context, deviceNames []string) error {
	if len(deviceNames) == 0 {
		return nil
	}
	var refs []string
	for _, name := range deviceNames {
		refs = append(refs, fmt.Sprintf(`AirPlay device %s`, quoteAppleScriptString(name)))
	}
	_, err := runAppleScript(ctx, fmt.Sprintf(`
tell application "Music"
	set current AirPlay devices to {%s}
end tell
`, strings.Join(refs, ", ")))
	return err
}

func SetAirPlayDeviceVolume(ctx context.Context, deviceName string, volume int) error {
	if volume < 0 || volume > 100 {
		return fmt.Errorf("volume must be 0-100")
	}
	_, err := runAppleScript(ctx, fmt.Sprintf(`
tell application "Music"
	set sound volume of (AirPlay device %s) to %d
end tell
`, quoteAppleScriptString(deviceName), volume))
	return err
}

func SetShuffleEnabled(ctx context.Context, enabled bool) error {
	val := "false"
	if enabled {
		val = "true"
	}
	_, err := runAppleScript(ctx, fmt.Sprintf(`
tell application "Music"
	set shuffle enabled to %s
end tell
`, val))
	return err
}

func PlayUserPlaylistByPersistentID(ctx context.Context, persistentID string) error {
	persistentID = strings.TrimSpace(persistentID)
	if persistentID == "" {
		return fmt.Errorf("persistentID is required")
	}
	_, err := runAppleScript(ctx, fmt.Sprintf(`
tell application "Music"
	play (some user playlist whose persistent ID is %s)
end tell
`, quoteAppleScriptString(persistentID)))
	return err
}

func FindUserPlaylistPersistentIDByName(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("playlist name is required")
	}

	playlists, err := ListUserPlaylists(ctx, "", 0)
	if err != nil {
		return "", err
	}

	target := canonicalizeName(name)

	// Prefer an exact canonical match.
	for _, p := range playlists {
		if canonicalizeName(p.Name) == target {
			return p.PersistentID, nil
		}
	}

	// Fall back to a contains match (canonical, case-insensitive).
	var matches []UserPlaylist
	for _, p := range playlists {
		if strings.Contains(strings.ToLower(canonicalizeName(p.Name)), strings.ToLower(target)) {
			matches = append(matches, p)
		}
	}

	if len(matches) == 1 {
		return matches[0].PersistentID, nil
	}
	if len(matches) > 1 {
		var b strings.Builder
		fmt.Fprintf(&b, "playlist name %q is ambiguous; matches:\n", name)
		for _, m := range matches {
			fmt.Fprintf(&b, "  %s\t%s\n", m.PersistentID, m.Name)
		}
		fmt.Fprint(&b, "use --playlist-id to disambiguate")
		return "", fmt.Errorf("%s", b.String())
	}

	return "", fmt.Errorf("playlist not found: %q (tip: run `homepodctl playlists --query %q` and use --playlist-id)", name, name)
}

func FindUserPlaylistNameByPersistentID(ctx context.Context, persistentID string) (string, error) {
	out, err := runAppleScript(ctx, fmt.Sprintf(`
tell application "Music"
	return name of (some user playlist whose persistent ID is %s)
end tell
`, quoteAppleScriptString(persistentID)))
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("playlist not found for id: %q", persistentID)
	}
	return out, nil
}

func ListUserPlaylists(ctx context.Context, query string, limit int) ([]UserPlaylist, error) {
	query = strings.TrimSpace(query)
	needle := strings.ToLower(query)

	out, err := runAppleScript(ctx, `
tell application "Music"
	set out to ""
	repeat with p in (every user playlist)
		set out to out & (persistent ID of p) & tab & (name of p) & tab & (smart of p as text) & tab & (genius of p as text) & linefeed
	end repeat
	return out
end tell
`)
	if err != nil {
		return nil, err
	}

	var playlists []UserPlaylist
	for _, line := range splitNonEmptyLines(out) {
		parts := strings.Split(line, "\t")
		for len(parts) < 4 {
			parts = append(parts, "")
		}
		p := UserPlaylist{
			PersistentID: strings.TrimSpace(parts[0]),
			Name:         strings.TrimSpace(parts[1]),
			Smart:        parseBool(parts[2]),
			Genius:       parseBool(parts[3]),
		}
		if needle != "" && !strings.Contains(strings.ToLower(p.Name), needle) {
			continue
		}
		playlists = append(playlists, p)
		if limit > 0 && len(playlists) >= limit {
			break
		}
	}
	return playlists, nil
}

func SearchUserPlaylists(ctx context.Context, query string) ([]UserPlaylist, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	all, err := ListUserPlaylists(ctx, "", 0)
	if err != nil {
		return nil, err
	}

	target := canonicalizeName(query)
	targetLower := strings.ToLower(target)

	var exact, prefix, contains []UserPlaylist
	for _, p := range all {
		c := canonicalizeName(p.Name)
		cl := strings.ToLower(c)
		switch {
		case cl == targetLower:
			exact = append(exact, p)
		case strings.HasPrefix(cl, targetLower):
			prefix = append(prefix, p)
		case strings.Contains(cl, targetLower):
			contains = append(contains, p)
		}
	}

	out := make([]UserPlaylist, 0, len(exact)+len(prefix)+len(contains))
	out = append(out, exact...)
	out = append(out, prefix...)
	out = append(out, contains...)
	return out, nil
}

func Pause(ctx context.Context) error {
	_, err := runAppleScript(ctx, `
tell application "Music"
	pause
end tell
`)
	return err
}

func Stop(ctx context.Context) error {
	_, err := runAppleScript(ctx, `
tell application "Music"
	stop
end tell
`)
	return err
}

func NextTrack(ctx context.Context) error {
	_, err := runAppleScript(ctx, `
tell application "Music"
	next track
end tell
`)
	return err
}

func PreviousTrack(ctx context.Context) error {
	_, err := runAppleScript(ctx, `
tell application "Music"
	previous track
end tell
`)
	return err
}

func GetStatus(ctx context.Context) (Status, error) {
	out, err := runAppleScript(ctx, `
tell application "Music"
	set ps to (player state as text)
	set tName to ""
	set tArtist to ""
	set tAlbum to ""
	try
		set tName to (name of current track as text)
		set tArtist to (artist of current track as text)
		set tAlbum to (album of current track as text)
	end try
	return ps & tab & tName & tab & tArtist & tab & tAlbum
end tell
`)
	if err != nil {
		return Status{}, err
	}
	parts := strings.Split(strings.TrimSpace(out), "\t")
	for len(parts) < 4 {
		parts = append(parts, "")
	}
	return Status{
		PlayerState: strings.TrimSpace(parts[0]),
		TrackName:   strings.TrimSpace(parts[1]),
		Artist:      strings.TrimSpace(parts[2]),
		Album:       strings.TrimSpace(parts[3]),
	}, nil
}

func GetNowPlaying(ctx context.Context) (NowPlaying, error) {
	out, err := runAppleScript(ctx, `
tell application "Music"
	set ps to (player state as text)
	set pos to (player position as text)
	set sh to (shuffle enabled as text)
	set rep to (song repeat as text)
	set pName to ""
	set pID to ""
	set tName to ""
	set tArtist to ""
	set tAlbum to ""
	set tDur to "0"
	set tPID to ""
	try
		set pName to (name of current playlist as text)
		set pID to (persistent ID of current playlist as text)
	end try
	try
		set tName to (name of current track as text)
		set tArtist to (artist of current track as text)
		set tAlbum to (album of current track as text)
		set tDur to (duration of current track as text)
		set tPID to (persistent ID of current track as text)
	end try
	return ps & tab & pos & tab & sh & tab & rep & tab & pName & tab & pID & tab & tName & tab & tArtist & tab & tAlbum & tab & tDur & tab & tPID
end tell
`)
	if err != nil {
		return NowPlaying{}, err
	}
	parts := strings.Split(strings.TrimSpace(out), "\t")
	for len(parts) < 11 {
		parts = append(parts, "")
	}
	np := NowPlaying{
		PlayerState:     strings.TrimSpace(parts[0]),
		PlayerPositionS: parseFloatLoose(parts[1]),
		ShuffleEnabled:  parseBool(parts[2]),
		SongRepeat:      strings.TrimSpace(parts[3]),
		PlaylistName:    strings.TrimSpace(parts[4]),
		PlaylistID:      strings.TrimSpace(parts[5]),
		Track: NowPlayingTrack{
			Name:         strings.TrimSpace(parts[6]),
			Artist:       strings.TrimSpace(parts[7]),
			Album:        strings.TrimSpace(parts[8]),
			DurationS:    parseFloatLoose(parts[9]),
			PersistentID: strings.TrimSpace(parts[10]),
		},
	}

	devs, err := ListAirPlayDevices(ctx)
	if err == nil {
		for _, d := range devs {
			if d.Selected {
				np.Outputs = append(np.Outputs, d)
			}
		}
	}
	return np, nil
}

func runAppleScript(ctx context.Context, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "osascript")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("osascript failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func escapeAppleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func quoteAppleScriptString(s string) string {
	return `"` + escapeAppleScriptString(s) + `"`
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "yes", "1":
		return true
	default:
		return false
	}
}

func splitNonEmptyLines(s string) []string {
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func parseFloatLoose(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	// Handle locales that use comma as decimal separator (e.g. "264,0").
	s = strings.ReplaceAll(s, ",", ".")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

func canonicalizeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	// Remove characters that commonly cause “looks identical” mismatches in terminals:
	// - Variation selectors (emoji style)
	// - Zero-width joiners
	// - Word joiners
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case 0xFE0E, 0xFE0F, 0x200D, 0x2060:
			continue
		default:
			if unicode.IsSpace(r) {
				b.WriteRune(' ')
			} else {
				b.WriteRune(r)
			}
		}
	}
	// Collapse whitespace runs.
	return strings.Join(strings.Fields(b.String()), " ")
}
