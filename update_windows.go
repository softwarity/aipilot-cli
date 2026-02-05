//go:build windows

package main

import (
	"os"
	"os/exec"
)

// restartSelf starts a new process and exits the current one
func restartSelf(exePath string) {
	cmd := exec.Command(exePath, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Start()
	os.Exit(0)
}
