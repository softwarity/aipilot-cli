//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

func init() {
	// Prevent Ctrl+Z from suspending the CLI process.
	// The PTY child (agent) handles Ctrl+Z via raw mode byte 0x1A.
	signal.Ignore(syscall.SIGTSTP)
}

func setupResizeSignal() chan os.Signal {
	resizeChan := make(chan os.Signal, 1)
	signal.Notify(resizeChan, syscall.SIGWINCH)
	return resizeChan
}
