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
				mobile := PairedMobile{
					ID:        status.MobileID,
					Name:      status.MobileName,
					PublicKey: status.PublicKey,
					PairedAt:  time.Now().Format(time.RFC3339),
				}
				d.pcConfig.addPairedMobile(mobile)
				savePCConfig(d.pcConfig)

				// If there's an active session, send encrypted token for the mobile
				d.mu.RLock()
				sessionID := d.session
				sessionToken := d.token
				d.mu.RUnlock()

				tokenSent := false
				if sessionID != "" && sessionToken != "" && mobile.PublicKey != "" {
					// Encrypt session token for the mobile
					pcPrivateKey, err := GetPrivateKeyFromHex(d.pcConfig.PrivateKey)
					if err == nil {
						encryptedToken, err := EncryptForMobile(sessionToken, mobile.PublicKey, pcPrivateKey)
						if err == nil {
							// Send to relay silently
							if err := d.relayClient.AddSessionTokenForMobile(sessionID, mobile.ID, encryptedToken); err == nil {
								tokenSent = true
							}
						}
					}
				}

				// Single line notification that doesn't disrupt the terminal
				if tokenSent {
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
