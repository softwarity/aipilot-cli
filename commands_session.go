package main

import (
	"fmt"
	"os"
	"time"
)

// getAIPilotCommand checks if a line is an AIPilot command (prefixed with //)
func (d *Daemon) getAIPilotCommand(line string) string {
	switch line {
	case "//qr":
		return "qr"
	}
	return ""
}

// executeAIPilotCommand runs an AIPilot command
func (d *Daemon) executeAIPilotCommand(cmd string) {
	switch cmd {
	case "qr":
		d.showPairingQRInAltScreen()
	}
}

// showPairingQRInAltScreen shows QR in alt screen, exits on keypress OR pairing completion
func (d *Daemon) showPairingQRInAltScreen() {
	// Clear agent screen BEFORE switching to alt screen
	d.sendToPTY([]byte{0x03}) // Ctrl+C to cancel any input
	time.Sleep(20 * time.Millisecond)
	d.sendToPTY([]byte{0x0c}) // Ctrl+L to clear/redraw
	time.Sleep(50 * time.Millisecond)

	// Switch to alternate screen and clear
	fmt.Print(altScreenOn + clearScreen + cursorHome)

	// Channel to signal pairing completion
	pairingDone := make(chan bool, 1)

	// Show QR and start polling (passing the channel)
	d.showPairingQRWithCallback(false, func() {
		pairingDone <- true
	})

	// Wait for any key in a goroutine
	keyPressed := make(chan bool, 1)
	go func() {
		b := make([]byte, 1)
		os.Stdin.Read(b)
		keyPressed <- true
	}()

	// Wait for either keypress or pairing completion
	select {
	case <-keyPressed:
		// User pressed a key, exit
	case <-pairingDone:
		// Pairing completed, auto-exit
		time.Sleep(500 * time.Millisecond) // Brief pause to show success message
	}

	// Restore main screen
	fmt.Print(altScreenOff)
}

