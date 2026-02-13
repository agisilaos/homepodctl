package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGoldenHelpRun(t *testing.T) {
	got := captureStdout(t, func() { cmdHelp([]string{"run"}) })
	assertGolden(t, "help_run.txt", got)
}

func TestGoldenHelpAutomation(t *testing.T) {
	got := captureStdout(t, func() { cmdHelp([]string{"automation"}) })
	assertGolden(t, "help_automation.txt", got)
}

func TestGoldenCompletionBash(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, err := completionScript("bash")
	if err != nil {
		t.Fatalf("completionScript(bash): %v", err)
	}
	assertGolden(t, "completion_bash.txt", got)
}

func TestGoldenCompletionZsh(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, err := completionScript("zsh")
	if err != nil {
		t.Fatalf("completionScript(zsh): %v", err)
	}
	assertGolden(t, "completion_zsh.txt", got)
}

func TestGoldenCompletionFish(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, err := completionScript("fish")
	if err != nil {
		t.Fatalf("completionScript(fish): %v", err)
	}
	assertGolden(t, "completion_fish.txt", got)
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	wantBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	want := string(wantBytes)
	if got != want {
		t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}
