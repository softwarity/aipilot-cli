//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

func setupResizeSignal() chan os.Signal {
	resizeChan := make(chan os.Signal, 1)
	signal.Notify(resizeChan, syscall.SIGWINCH)
	return resizeChan
}
