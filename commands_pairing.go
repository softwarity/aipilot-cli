package main

import (
	"encoding/json"
	"fmt"
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

				// Single line notification that doesn't disrupt the terminal
				if samePublicKey {
					fmt.Printf("\n%s✓ Paired: %s (session unchanged)%s\n", green, mobile.Name, reset)
				} else if tokenShared {
					fmt.Printf("\n%s✓ Paired: %s (session shared)%s\n", green, mobile.Name, reset)
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

// addTokenForMobile encrypts the existing session token for a new mobile
// and sends it to the relay.
func (d *Daemon) addTokenForMobile(mobile PairedMobile) bool {
	d.mu.RLock()
	sessionID := d.session
	sessionToken := d.token
	d.mu.RUnlock()

	if sessionID == "" || sessionToken == "" {
		return false
	}

	if mobile.PublicKey == "" {
		return false
	}

	// Get PC private key for encryption
	pcPrivateKey, err := GetPrivateKeyFromHex(d.pcConfig.PrivateKey)
	if err != nil {
		return false
	}

	// Encrypt existing token for the new mobile
	encryptedToken, err := EncryptForMobile(sessionToken, mobile.PublicKey, pcPrivateKey)
	if err != nil {
		return false
	}

	// Add token to existing session on relay
	if err := d.relayClient.AddSessionTokenForMobile(sessionID, mobile.ID, encryptedToken); err != nil {
		return false
	}

	return true
}
