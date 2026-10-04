//go:build darwin || linux

package native

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestFailedSavePreservesPreviousFile(t *testing.T) {
	path := os.Getenv("AUDIT_STATE_PATH")
	if path != "" {
		var original syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
			t.Fatal(err)
		}
		limited := original
		limited.Cur = 128
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
				t.Error(err)
			}
		}()
		cfg := &Config{Defaults: DefaultsConfig{Backend: strings.Repeat("synthetic", 100)}}
		err := SaveConfig(cfg)
		if err == nil {
			t.Fatal("expected file-size failure")
		}
		fmt.Println("injected save error:", err)
		return
	}
	t.Setenv("HOME", t.TempDir())
	path, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	before := []byte(`{"defaults":{"backend":"airplay","rooms":["existing"]},"aliases":{}}`)
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestFailedSavePreservesPreviousFile$", "-test.v")
	cmd.Env = append(os.Environ(), "AUDIT_STATE_PATH="+path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child failed: %v %s", err, out)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("previous file destroyed after failed save: before=%d bytes after=%d bytes validJSON=%v; %s", len(before), len(after), json.Valid(after), out)
	}
	temps, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".config-*"))
	if err != nil || len(temps) != 0 {
		t.Fatalf("temporary files after failure: %v, %v", temps, err)
	}

	recovered, err := LoadConfigOptional()
	if err != nil || recovered == nil || recovered.Defaults.Backend != "airplay" {
		t.Fatalf("reload after failed save: %+v, %v", recovered, err)
	}
	recovered.Defaults.Backend = "native"
	if err := SaveConfig(recovered); err != nil {
		t.Fatal(err)
	}
	recovered, err = LoadConfigOptional()
	if err != nil || recovered == nil || recovered.Defaults.Backend != "native" {
		t.Fatalf("save/reload recovery: %+v, %v", recovered, err)
	}

}
