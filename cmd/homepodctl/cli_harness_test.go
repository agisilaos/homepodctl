package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

type cliResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type cliHarness struct {
	bin  string
	home string
	env  []string
}

func newCLIHarness(t *testing.T) *cliHarness {
	t.Helper()
	cli := &cliHarness{bin: buildCLIBinary(t), home: t.TempDir()}
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "HOME=") || strings.HasPrefix(entry, "HOMEPODCTL_VERBOSE=") {
			continue
		}
		cli.env = append(cli.env, entry)
	}
	cli.env = append(cli.env, "HOME="+cli.home)
	return cli
}

func (cli *cliHarness) run(t *testing.T, args ...string) cliResult {
	t.Helper()
	cmd := exec.Command(cli.bin, args...)
	cmd.Env = cli.env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := cliResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run %q: %v\nstdout:\n%s\nstderr:\n%s", cmd.Args, err, result.Stdout, result.Stderr)
		}
		result.ExitCode = exitErr.ExitCode()
	}
	t.Logf("run %q exit=%d\nstdout:\n%s\nstderr:\n%s", cmd.Args, result.ExitCode, result.Stdout, result.Stderr)
	return result
}
