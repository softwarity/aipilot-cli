//go:build !windows

package main

import (
	"os"
	"syscall"
)

// restartSelf replaces the current process with the updated binary
func restartSelf(exePath string) {
	syscall.Exec(exePath, os.Args, os.Environ())
}
