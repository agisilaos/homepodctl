package native

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceConfigRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := replaceConfig(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if err := replaceConfig(path, []byte("next")); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	info, _ = os.Stat(path)
	if string(data) != "next" || info.Mode().Perm() != 0640 {
		t.Fatalf("data=%q mode=%v", data, info.Mode())
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".config-*"))
	if len(files) != 0 {
		t.Fatal(files)
	}
}
func TestReplaceConfigRefusesSymlinkAndDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, root} {
		if err := replaceConfig(path, []byte("new")); err == nil {
			t.Fatal("unsafe target accepted")
		}
	}
	data, _ := os.ReadFile(target)
	if string(data) != "original" {
		t.Fatal(string(data))
	}
}
