package main

import (
	"encoding/json"
	"fmt"
	"time"
)

// showPairingQR displays a pairing QR code
func (d *Daemon) showPairingQR(asImage bool) {
	d.showPairingQRWithCallback(asImage, nil)
}

// showPairingQRWithCallback displays a pairing QR code with optional completion callback
func (d *Daemon) showPairingQRWithCallback(asImage bool, onComplete func()) {
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
	fmt.Printf("  Expires: %s\n", pairingResp.ExpiresAt)
	fmt.Printf("\n%sPress any key to cancel...%s", dim, reset)

	// Start background polling for pairing completion
	go d.pollPairingCompletionWithCallback(pairingResp.Token, onComplete)
}

// pollPairingCompletion polls for pairing completion in background
func (d *Daemon) pollPairingCompletion(token string) {
	d.pollPairingCompletionWithCallback(token, nil)
}

// pollPairingCompletionWithCallback polls for pairing completion with optional callback
func (d *Daemon) pollPairingCompletionWithCallback(token string, onComplete func()) {
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
				// Check if mobile already exists with same public key (re-pairing scenario)
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
					// New mobile or reinstalled app: share existing session token
					// No need to recreate session, just add encrypted token for this mobile
					tokenShared = d.addTokenForMobile(mobile)
				}

				// Single line notification
				if samePublicKey {
					fmt.Printf("\n%s✓ Paired: %s (session unchanged)%s\n", green, mobile.Name, reset)
				} else if tokenShared {
					fmt.Printf("\n%s✓ Paired: %s (session shared)%s\n", green, mobile.Name, reset)
				} else {
					fmt.Printf("\n%s✓ Paired: %s%s\n", green, mobile.Name, reset)
				}

				// Call callback if provided (for alt screen auto-close)
				if onComplete != nil {
					onComplete()
				} else {
					// Only send newline if not in alt screen mode
					if d.ptmx != nil {
						d.ptmx.Write([]byte("\n"))
					}
				}
				return

			case "expired":
				return
			}
		}
	}
}

// addTokenForMobile encrypts session tokens for a new mobile and sends them
// to the relay for ALL sessions on this PC (not just the current one).
// Each AddSessionTokenForMobile call triggers a session_token_added notification.
func (d *Daemon) addTokenForMobile(mobile PairedMobile) bool {
	if mobile.PublicKey == "" {
		fmt.Printf("%s  no public key for %s%s\n", dim, mobile.ID[:8], reset)
		return false
	}

	pcPrivateKey, err := GetPrivateKeyFromHex(d.pcConfig.PrivateKey)
	if err != nil {
		fmt.Printf("%s  failed to get private key: %v%s\n", red, err, reset)
		return false
	}

	// Get ALL sessions for this PC (includes plaintext tokens via for_cli=true)
	sessions, err := d.relayClient.ListAllSessions()
	if err != nil {
		fmt.Printf("%s  failed to list sessions: %v%s\n", red, err, reset)
		return false
	}

	fmt.Printf("%s  found %d sessions for mobile %s%s\n", dim, len(sessions), mobile.ID[:8], reset)

	count := 0
	for _, sess := range sessions {
		if sess.Token == "" {
			fmt.Printf("%s  session %s has no token, skipping%s\n", dim, sess.ID[:8], reset)
			continue
		}
		encrypted, err := EncryptForMobile(sess.Token, mobile.PublicKey, pcPrivateKey)
		if err != nil {
			fmt.Printf("%s  encrypt failed for session %s: %v%s\n", red, sess.ID[:8], err, reset)
			continue
		}
		if err := d.relayClient.AddSessionTokenForMobile(sess.ID, mobile.ID, encrypted); err != nil {
			fmt.Printf("%s  failed to share session %s: %v%s\n", red, sess.ID[:8]+"...", err, reset)
			continue
		}
		fmt.Printf("%s  ✓ shared session %s%s\n", dim, sess.ID[:8], reset)
		count++
	}

	fmt.Printf("%s  shared %d/%d sessions%s\n", dim, count, len(sessions), reset)
	return count > 0
}
