package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/agisilaos/homepodctl/internal/music"
	"github.com/agisilaos/homepodctl/internal/native"
)

func printNowPlaying(np music.NowPlaying) {
	pos := formatClock(np.PlayerPositionS)
	dur := ""
	if np.Track.DurationS > 0 {
		dur = "/" + formatClock(np.Track.DurationS)
	}
	sh := "off"
	if np.ShuffleEnabled {
		sh = "on"
	}
	fmt.Printf("state=%s pos=%s%s shuffle=%s repeat=%s\n", np.PlayerState, pos, dur, sh, np.SongRepeat)
	if np.PlaylistName != "" {
		fmt.Printf("playlist=%q\n", np.PlaylistName)
	}
	if np.Track.Name != "" {
		fmt.Printf("track=%q artist=%q album=%q\n", np.Track.Name, np.Track.Artist, np.Track.Album)
	}
	if len(np.Outputs) > 0 {
		var parts []string
		for _, o := range np.Outputs {
			parts = append(parts, fmt.Sprintf("%s(vol=%d)", o.Name, o.Volume))
		}
		fmt.Printf("outputs=%s\n", strings.Join(parts, ", "))
	}
}

func printNowPlayingPlain(np music.NowPlaying) {
	var outputNames []string
	for _, o := range np.Outputs {
		outputNames = append(outputNames, o.Name)
	}
	fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\n",
		np.PlayerState,
		np.Track.Name,
		np.Track.Artist,
		np.Track.Album,
		np.PlaylistName,
		strings.Join(outputNames, ","),
	)
}

func formatClock(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	s := int(seconds + 0.5)
	h := s / 3600
	m := (s % 3600) / 60
	sec := s % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%d:%02d", m, sec)
}

func choosePlaylist(matches []music.UserPlaylist) (music.UserPlaylist, error) {
	if len(matches) == 1 {
		return matches[0], nil
	}
	fmt.Fprintln(os.Stderr, "Multiple playlists match. Choose one:")
	for i, p := range matches {
		fmt.Fprintf(os.Stderr, "  %d) %s\t%s\n", i+1, p.PersistentID, p.Name)
	}
	fmt.Fprint(os.Stderr, "Enter number: ")
	var n int
	if _, err := fmt.Fscan(os.Stdin, &n); err != nil {
		return music.UserPlaylist{}, fmt.Errorf("read selection: %w", err)
	}
	if n < 1 || n > len(matches) {
		return music.UserPlaylist{}, fmt.Errorf("invalid selection %d", n)
	}
	return matches[n-1], nil
}

func printDevicesTable(w io.Writer, devs []music.AirPlayDevice, plain bool) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !plain {
		fmt.Fprintln(tw, "NAME\tKIND\tAVAILABLE\tSELECTED\tVOLUME")
	}
	for _, d := range devs {
		kind := d.Kind
		if kind == "" {
			kind = "unknown"
		}
		fmt.Fprintf(tw, "%s\t%s\t%t\t%t\t%d\n", d.Name, kind, d.Available, d.Selected, d.Volume)
	}
	_ = tw.Flush()
}

type aliasRow struct {
	Name    string   `json:"name"`
	Backend string   `json:"backend"`
	Rooms   []string `json:"rooms"`
	Target  string   `json:"target"`
}

func buildAliasRows(cfg *native.Config) []aliasRow {
	names := make([]string, 0, len(cfg.Aliases))
	for name := range cfg.Aliases {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([]aliasRow, 0, len(names))
	for _, name := range names {
		a := cfg.Aliases[name]
		backend := a.Backend
		if backend == "" {
			backend = cfg.Defaults.Backend
		}
		rooms := append([]string(nil), a.Rooms...)
		if len(rooms) == 0 {
			rooms = append(rooms, cfg.Defaults.Rooms...)
		}
		target := a.Playlist
		if target == "" {
			target = a.PlaylistID
		}
		if a.Shortcut != "" {
			target = "shortcut:" + a.Shortcut
		}
		rows = append(rows, aliasRow{
			Name:    name,
			Backend: backend,
			Rooms:   rooms,
			Target:  target,
		})
	}
	return rows
}

func printAliasesTable(w io.Writer, rows []aliasRow, plain bool) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !plain {
		fmt.Fprintln(tw, "NAME\tBACKEND\tROOMS\tTARGET")
	}
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", row.Name, row.Backend, strings.Join(row.Rooms, ","), row.Target)
	}
	_ = tw.Flush()
}
