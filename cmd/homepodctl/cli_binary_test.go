package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	cliBinaryOnce sync.Once
	cliBinaryPath string
	cliBinaryErr  error
)

func buildCLIBinary(t *testing.T) string {
	t.Helper()

	cliBinaryOnce.Do(func() {
		tmpDir, err := os.MkdirTemp("", "homepodctl-cli-test-*")
		if err != nil {
			cliBinaryErr = fmt.Errorf("create temp dir for cli binary: %w", err)
			return
		}

		bin := filepath.Join(tmpDir, "homepodctl")
		repoRoot := filepath.Clean(filepath.Join("..", ".."))
		build := exec.Command("go", "build", "-o", bin, "./cmd/homepodctl")
		build.Dir = repoRoot
		if out, err := build.CombinedOutput(); err != nil {
			cliBinaryErr = fmt.Errorf("build cli: %w: %s", err, string(out))
			return
		}
		cliBinaryPath = bin
	})

	if cliBinaryErr != nil {
		t.Fatalf("%v", cliBinaryErr)
	}
	if cliBinaryPath == "" {
		t.Fatalf("shared cli binary path is empty")
	}
	return cliBinaryPath
}
