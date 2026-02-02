package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/skip2/go-qrcode"
)

// printRaw prints text with \n converted to \r\n for raw terminal mode
func printRaw(format string, args ...interface{}) {
	text := fmt.Sprintf(format, args...)
	text = strings.ReplaceAll(text, "\n", "\r\n")
	fmt.Print(text)
}

// getAIPilotCommand checks if a line is an AIPilot command
func (d *Daemon) getAIPilotCommand(line string) string {
	switch line {
	case "/qr":
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

// showPairingQRInAltScreen shows QR in alt screen, exits on ESC/Ctrl+C or pairing completion
func (d *Daemon) showPairingQRInAltScreen() {
	// Clear agent screen BEFORE switching to alt screen
	d.sendToPTY([]byte{0x03}) // Ctrl+C to cancel any input
	time.Sleep(20 * time.Millisecond)
	d.sendToPTY([]byte{0x0c}) // Ctrl+L to clear/redraw
	time.Sleep(50 * time.Millisecond)

	// Switch to alternate screen, clear, and hide cursor
	fmt.Print(altScreenOn + clearScreen + cursorHome + hideCursor)

	// Channel to signal pairing completion
	pairingDone := make(chan bool, 1)

	// Show QR in raw mode (using \r\n)
	d.showPairingQRRaw(func() {
		pairingDone <- true
	})

	// Show exit hint
	printRaw("\n%sPress ESC or Ctrl+C to close%s\n", dim, reset)

	// Read keys in a goroutine, only exit on ESC or Ctrl+C
	// Use atomic flag to signal goroutine to stop
	var shouldExit int32
	exitRequested := make(chan bool, 1)
	go func() {
		b := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(b)
			if err != nil || n == 0 {
				return
			}

			// Check if we should exit (pairing completed or screen closed)
			if atomic.LoadInt32(&shouldExit) != 0 {
				// Forward this key to PTY instead of discarding
				d.sendToPTY(b[:n])
				return
			}

			// ESC (0x1b) or Ctrl+C (0x03) to exit
			if b[0] == 0x1b || b[0] == 0x03 {
				exitRequested <- true
				return
			}
			// Ignore all other keys while in QR screen
		}
	}()

	// Wait for exit key or pairing completion
	select {
	case <-exitRequested:
		// User pressed ESC or Ctrl+C
	case <-pairingDone:
		// Pairing completed, auto-exit
		time.Sleep(500 * time.Millisecond) // Brief pause to show success message
	}

	// Signal goroutine to stop intercepting keys
	atomic.StoreInt32(&shouldExit, 1)

	// Restore main screen and show cursor
	fmt.Print(showCursor + altScreenOff)
}

// showPairingQRRaw displays pairing QR in raw terminal mode (uses \r\n)
func (d *Daemon) showPairingQRRaw(onComplete func()) {
	if d.relayClient == nil || d.pcConfig == nil {
		printRaw("%sError: Cannot create pairing QR%s\n", red, reset)
		return
	}

	// Initialize pairing on relay
	printRaw("%sCreating pairing code...%s\n", dim, reset)
	pairingResp, err := d.relayClient.InitPairing()
	if err != nil {
		printRaw("%sError: %v%s\n", red, err, reset)
		return
	}

	// Create QR data
	qrData := PairingQRData{
		Type:      "pairing",
		Relay:     d.relay,
		Token:     pairingResp.Token,
		PCID:      d.pcConfig.PCID,
		PCName:    d.pcConfig.PCName,
		PublicKey: d.pcConfig.PublicKey,
	}

	// Include session info if we have an active session
	d.mu.RLock()
	sessionID := d.session
	workDir := d.workDir
	agentType := d.agentType
	d.mu.RUnlock()

	if sessionID != "" {
		qrData.SessionID = sessionID
		qrData.WorkingDir = workDir
		qrData.AgentType = string(agentType)

		// Add SSH info
		sshInfo := DetectSSHInfo()
		if sshInfo != nil && sshInfo.Available {
			qrData.SSHAvailable = true
			qrData.SSHPort = sshInfo.Port
			qrData.Hostname = sshInfo.Hostname
			qrData.Username = sshInfo.Username
		}
	}

	qrJSON, err := json.Marshal(qrData)
	if err != nil {
		printRaw("%sError creating QR: %v%s\n", red, err, reset)
		return
	}

	printRaw("\n%sScan to pair a new mobile device:%s\n\n", bold, reset)

	// Generate and print QR code with \r\n
	qr, err := qrcode.New(string(qrJSON), qrcode.Medium)
	if err != nil {
		printRaw("%sError generating QR code: %v%s\n", red, err, reset)
		return
	}
	qrStr := qr.ToSmallString(false)
	printRaw("%s", qrStr)

	printRaw("\n  PC: %s\n", d.pcConfig.PCName)
	printRaw("  Expires: %s\n", pairingResp.ExpiresAt)

	// Start background polling for pairing completion
	go d.pollPairingCompletionRaw(pairingResp.Token, onComplete)
}

// pollPairingCompletionRaw polls for pairing completion with raw mode output
func (d *Daemon) pollPairingCompletionRaw(token string, onComplete func()) {
	ticker := time.NewTicker(PairingPollInterval)
	defer ticker.Stop()
	timeout := time.After(PairingTimeout)

	for {
		select {
		case <-timeout:
			return // Silently timeout
		case <-ticker.C:
			status, err := d.relayClient.CheckPairingStatus(token)
			if err != nil {
				continue
			}

			switch status.Status {
			case "completed":
				existingMobile := d.pcConfig.getPairedMobile(status.MobileID)
				samePublicKey := existingMobile != nil && existingMobile.PublicKey == status.PublicKey

				mobile := PairedMobile{
					ID:        status.MobileID,
					Name:      status.MobileName,
					PublicKey: status.PublicKey,
					PairedAt:  time.Now().Format(time.RFC3339),
				}
				d.pcConfig.addPairedMobile(mobile)
				savePCConfig(d.pcConfig)

				d.mu.RLock()
				oldSessionID := d.session
				d.mu.RUnlock()

				tokenShared := false
				if oldSessionID != "" && !samePublicKey {
					tokenShared = d.addTokenForMobile(mobile)
				}

				// Single line notification
				if samePublicKey {
					printRaw("\n%s✓ Paired: %s (session unchanged)%s\n", green, mobile.Name, reset)
				} else if tokenShared {
					printRaw("\n%s✓ Paired: %s (session shared)%s\n", green, mobile.Name, reset)
				} else {
					printRaw("\n%s✓ Paired: %s%s\n", green, mobile.Name, reset)
				}

				if onComplete != nil {
					onComplete()
				}
				return

			case "expired":
				return
			}
		}
	}
}

