//go:build !darwin

package main

// The executable only supports macOS; keep package tests buildable elsewhere.
var setupInputIsTerminal = func() bool { return false }
