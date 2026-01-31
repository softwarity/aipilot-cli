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
	case "//qr-image":
		return "qr-image"
	case "//status":
		return "status"
	case "//disconnect":
		return "disconnect"
	case "//purge":
		return "purge"
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
	case "qr-image":
		d.showPairingQR(true)
	case "status":
		d.printStatus()
	case "disconnect":
		d.disconnectMobile()
	case "purge":
		d.purgeAllSessions()
	case "quit":
		fmt.Printf("%sShutting down AIPilot...%s\n", yellow, reset)
		os.Exit(0)
	}
}

// disconnectMobile sends disconnect command to mobile
func (d *Daemon) disconnectMobile() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.mobileConnected {
		fmt.Printf("%sMobile not connected.%s\n", yellow, reset)
		return
	}

	if d.wsConn != nil {
		msg := Message{Type: "kick"}
		d.wsMu.Lock()
		d.wsConn.WriteJSON(msg)
		d.wsMu.Unlock()
		d.mobileConnected = false
		fmt.Printf("%sMobile disconnected.%s\n", green, reset)
	}
}

// purgeAllSessions removes all sessions from the relay
func (d *Daemon) purgeAllSessions() {
	if d.relayClient == nil {
		fmt.Printf("%sError: Not connected to relay%s\n", red, reset)
		return
	}

	fmt.Printf("%sPurging all sessions from relay...%s\n", dim, reset)
	count, err := d.relayClient.PurgeAllSessions()
	if err != nil {
		fmt.Printf("%sError: %v%s\n", red, err, reset)
		return
	}

	if count == 0 {
		fmt.Printf("%sNo sessions to purge.%s\n", yellow, reset)
	} else {
		fmt.Printf("%s✓ Purged %d session(s).%s\n", green, count, reset)
	}
}
