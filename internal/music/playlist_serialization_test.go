package music

import (
	"encoding/json"
	"os/exec"
	"reflect"
	"runtime"
	"testing"
)

// Exercise Foundation's real bridge with synthetic data, without contacting Music.
func TestPlaylistJSONSerialization(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Foundation requires macOS")
	}
	const name = " Focus\tMix\n東京 🎵\r\"\\ "
	script := playlistJSONHandler + "\nreturn playlistJSON({{\"A1\", " + quoteAppleScriptString(name) + ", true, false}, {\"B2\", \"\", false, true}})"
	raw, err := exec.Command("/usr/bin/osascript", "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("serialize: %v: %s", err, raw)
	}
	var got []UserPlaylist
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want := []UserPlaylist{{PersistentID: "A1", Name: name, Smart: true}, {PersistentID: "B2", Genius: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%+v want=%+v", got, want)
	}
	raw, err = exec.Command("/usr/bin/osascript", "-e", playlistJSONHandler+"\nreturn playlistJSON({})").CombinedOutput()
	if err != nil || string(raw) != "[]\n" {
		t.Fatalf("empty: %q err=%v", raw, err)
	}
}
