package main

import (
	"fmt"
	"os"
)

// getAIPilotCommand checks if a line is an AIPilot command (prefixed with //)
func (d *Daemon) getAIPilotCommand(line string) string {
	switch line {
	case "//qr":
		return "qr"
	case "//status":
		return "status"
	case "//quit":
		return "quit"
	}
	return ""
}

// executeAIPilotCommand runs an AIPilot command
func (d *Daemon) executeAIPilotCommand(cmd string) {
	fmt.Println()
	switch cmd {
	case "qr":
		d.showPairingQR(false)
	case "status":
		d.printStatus()
	case "quit":
		fmt.Printf("%sShutting down AIPilot...%s\n", yellow, reset)
		d.cleanup()
		os.Exit(0)
	}
}

