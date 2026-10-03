package main

import (
	"fmt"
	"io"
)

// checkedOutput uses the CLI's existing fatal-error boundary. Stop immediately
// on failed delivery so streaming commands do not keep polling a closed sink.
type checkedOutput struct{ io.Writer }

func (w checkedOutput) Write(p []byte) (int, error) {
	n, err := (strictOutput{w.Writer}).Write(p)
	if err != nil {
		die(&stdoutError{cause: err, code: exitGeneric})
	}
	return n, nil
}

type stdoutError struct {
	cause error
	code  int
}

func (e *stdoutError) Error() string {
	return fmt.Sprintf("stdout write failed: %v; any completed changes remain applied; inspect state before retrying", e.cause)
}
func (e *stdoutError) Unwrap() error { return e.cause }

// A known validation failure keeps its status even if its report cannot print.
func preserveOutputStatus(code int) {
	r := recover()
	if r == nil {
		return
	}
	if fatal, ok := r.(cliFatal); ok {
		if output, ok := fatal.err.(*stdoutError); ok {
			output.code = code
		}
	}
	panic(r)
}

// Buffered formatters must receive ordinary errors: tabwriter wraps foreign panics.
type strictOutput struct{ io.Writer }

func (w strictOutput) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}
