//go:build windows

package main

import (
	"os"
)

func setupResizeSignal() chan os.Signal {
	// Windows doesn't support SIGWINCH
	// Return a channel that will never receive
	return make(chan os.Signal, 1)
}
