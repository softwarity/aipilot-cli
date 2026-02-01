package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"
)

// showPairingQR displays a pairing QR code
func (d *Daemon) showPairingQR(asImage bool) {
	if d.relayClient == nil || d.pcConfig == nil {
		fmt.Printf("%sError: Cannot create pairing QR%s\n", red, reset)
		return
	}

	// Initialize pairing on relay
	fmt.Printf("%sCreating pairing code...%s\n", dim, reset)
	pairingResp, err := d.relayClient.InitPairing()
	if err != nil {
		fmt.Printf("%sError: %v%s\n", red, err, reset)
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
		fmt.Printf("%sError creating QR: %v%s\n", red, err, reset)
		return
	}

	fmt.Printf("\n%sScan to pair a new mobile device:%s\n\n", bold, reset)
	printQRCodeString(string(qrJSON), asImage)
	fmt.Printf("\n  PC: %s\n", d.pcConfig.PCName)
	fmt.Printf("  Expires: %s\n\n", pairingResp.ExpiresAt)

	// Start background polling for pairing completion
	go d.pollPairingCompletion(pairingResp.Token)
}

// pollPairingCompletion polls for pairing completion in background
func (d *Daemon) pollPairingCompletion(token string) {
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
				mobile := PairedMobile{
					ID:        status.MobileID,
					Name:      status.MobileName,
					PublicKey: status.PublicKey,
					PairedAt:  time.Now().Format(time.RFC3339),
				}
				d.pcConfig.addPairedMobile(mobile)
				savePCConfig(d.pcConfig)

				// If there's an active session, recreate it with tokens for all mobiles
				// This uses the same approach as initial session creation
				d.mu.RLock()
				oldSessionID := d.session
				d.mu.RUnlock()

				sessionRecreated := false
				if oldSessionID != "" {
					sessionRecreated = d.recreateSession()
				}

				// Single line notification that doesn't disrupt the terminal
				if sessionRecreated {
					fmt.Printf("\n%s✓ Paired: %s (session ready)%s\n", green, mobile.Name, reset)
				} else {
					fmt.Printf("\n%s✓ Paired: %s%s\n", green, mobile.Name, reset)
				}
				// Send a newline to refresh the prompt
				if d.ptmx != nil {
					d.ptmx.Write([]byte("\n"))
				}
				return

			case "expired":
				return
			}
		}
	}
}

// recreateSession deletes the current session and creates a new one with tokens for all mobiles
// This ensures the same flow as initial session creation after pairing
func (d *Daemon) recreateSession() bool {
	d.mu.Lock()
	oldSession := d.session
	workDir := d.workDir
	agentType := d.agentType
	d.mu.Unlock()

	if oldSession == "" {
		return false
	}

	// Delete old session on relay
	if err := d.relayClient.DeleteSession(oldSession); err != nil {
		fmt.Printf("%sWarning: could not delete old session: %v%s\n", yellow, err, reset)
		// Continue anyway - create new session
	}

	// Create new session with tokens for all paired mobiles (same as initial creation)
	displayName := filepath.Base(workDir)
	sshInfo := DetectSSHInfo()
	sessionResp, err := d.relayClient.CreateSession(string(agentType), workDir, displayName, sshInfo)
	if err != nil {
		fmt.Printf("%sWarning: could not create new session: %v%s\n", yellow, err, reset)
		return false
	}

	// Update daemon state
	d.mu.Lock()
	d.session = sessionResp.SessionID
	d.token = sessionResp.Token
	d.mu.Unlock()

	// Reinitialize encryption with new token
	if err := d.initEncryption(); err != nil {
		fmt.Printf("%sWarning: could not reinit encryption: %v%s\n", yellow, err, reset)
		return false
	}

	// Save new session locally
	saveSession(workDir, &SessionData{
		Session:   sessionResp.SessionID,
		Token:     sessionResp.Token,
		Relay:     d.relay,
		Command:   d.command,
		WorkDir:   workDir,
		CreatedAt: time.Now().Format(time.RFC3339),
	})

	// Close WebSocket to force reconnect with new session ID
	d.mu.Lock()
	if d.wsConn != nil {
		d.wsConn.Close()
	}
	d.mu.Unlock()

	return true
}
