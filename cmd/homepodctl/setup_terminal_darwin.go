package main

import (
	"os"
	"syscall"
	"unsafe"
)

var setupInputIsTerminal = func() bool {
	var state syscall.Termios
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), syscall.TIOCGETA, uintptr(unsafe.Pointer(&state)))
	return err == 0
}
