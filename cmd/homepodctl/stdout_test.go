package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type shortOutput struct{ calls int }

func (w *shortOutput) Write(p []byte) (int, error) { w.calls++; return len(p) / 2, nil }
func TestCheckedOutputRejectsShortWrites(t *testing.T) {
	sink := &shortOutput{}
	defer func() {
		fatal, ok := recover().(cliFatal)
		if !ok || !errors.Is(fatal.err, io.ErrShortWrite) || classifyExitCode(fatal.err) != exitGeneric || sink.calls != 1 {
			t.Fatalf("fatal=%+v calls=%d", fatal, sink.calls)
		}
	}()
	_, _ = (checkedOutput{sink}).Write([]byte("hello"))
	t.Fatal("accepted short write")
}

func TestCLIStdoutFailures(t *testing.T) {
	cli := newCLIHarness(t)
	// Keep both HOME and XDG config isolated; no native commands are invoked.
	cli.env = append(cli.env, "XDG_CONFIG_HOME="+filepath.Join(cli.home, "config"))
	if res := cli.run(t, "config-init"); res.ExitCode != 0 {
		t.Fatal(res)
	}
	ro, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	run := func(args []string, want int) {
		t.Helper()
		cmd := exec.Command(cli.bin, args...)
		cmd.Env = cli.env
		cmd.Stdout = ro
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != want || !strings.Contains(stderr.String(), "stdout write failed") {
			t.Fatalf("%v err=%v stderr=%s", args, err, stderr.String())
		}
	}
	for _, args := range [][]string{{"version"}, {"aliases", "--json"}, {"aliases", "--plain"}, {"aliases"}, {"help", "play"}, {"completion", "bash"}, {"config", "set", "defaults.backend", "native"}} {
		run(args, 1)
	}
	res := cli.run(t, "config", "get", "defaults.backend")
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "native") {
		t.Fatalf("save was lost: %+v", res)
	}
	// The existing validation status must survive a lost JSON or text report.
	pathResult := cli.run(t, "config", "validate", "--json")
	var report configValidateResult
	if err := json.Unmarshal([]byte(pathResult.Stdout), &report); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report.Path, []byte(`{"defaults":{"backend":"invalid"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	run([]string{"config", "validate"}, 3)
	run([]string{"config", "validate", "--json"}, 3)
}

func TestTablesReturnDeliveryErrors(t *testing.T) {
	for _, plain := range []bool{false, true} {
		sink := &shortOutput{}
		err := printAliasesTable(sink, []aliasRow{{Name: "example", Backend: "native", Target: "sample"}}, plain)
		if !errors.Is(err, io.ErrShortWrite) || sink.calls != 1 {
			t.Fatalf("plain=%t err=%v calls=%d", plain, err, sink.calls)
		}
	}
}
