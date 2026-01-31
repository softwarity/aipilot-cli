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

	qrJSON, err := json.Marshal(qrData)
	if err != nil {
		fmt.Printf("%sError creating QR: %v%s\n", red, err, reset)
		return
	}

	fmt.Printf("\n%sScan to pair a new mobile device:%s\n\n", bold, reset)
	printQRCodeString(string(qrJSON), asImage)
	fmt.Printf("\n  PC: %s\n", d.pcConfig.PCName)
	fmt.Printf("  Expires: %s\n\n", pairingResp.ExpiresAt)
	fmt.Printf("%sAlready paired devices will see the session automatically.%s\n", dim, reset)
	fmt.Printf("%sPairing happens in background - check /cli-status for updates.%s\n\n", dim, reset)

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
				if err := savePCConfig(d.pcConfig); err == nil {
					fmt.Printf("\n%s✓ Device paired: %s%s\n", green, mobile.Name, reset)
				}

				// If there's an active session, send encrypted token for the mobile
				d.mu.RLock()
				sessionID := d.session
				sessionToken := d.token
				d.mu.RUnlock()

				fmt.Printf("%s  Session ID: %s, Token: %s, Mobile PublicKey: %s%s\n",
					dim,
					func() string {
						if sessionID != "" {
							return sessionID[:8] + "..."
						} else {
							return "none"
						}
					}(),
					func() string {
						if sessionToken != "" {
							return "present"
						} else {
							return "none"
						}
					}(),
					func() string {
						if mobile.PublicKey != "" {
							return mobile.PublicKey[:16] + "..."
						} else {
							return "none"
						}
					}(),
					reset)

				if sessionID != "" && sessionToken != "" && mobile.PublicKey != "" {
					// Encrypt session token for the mobile
					pcPrivateKey, err := GetPrivateKeyFromHex(d.pcConfig.PrivateKey)
					if err != nil {
						fmt.Printf("%sWarning: Could not get private key: %v%s\n", yellow, err, reset)
					} else {
						encryptedToken, err := EncryptForMobile(sessionToken, mobile.PublicKey, pcPrivateKey)
						if err != nil {
							fmt.Printf("%sWarning: Could not encrypt token: %v%s\n", yellow, err, reset)
						} else {
							// Send to relay
							fmt.Printf("%s  Sending encrypted token to relay...%s\n", dim, reset)
							if err := d.relayClient.AddSessionTokenForMobile(sessionID, mobile.ID, encryptedToken); err != nil {
								fmt.Printf("%sWarning: Could not send session token: %v%s\n", yellow, err, reset)
							} else {
								fmt.Printf("%s  ✓ Token sent to relay%s\n", green, reset)
							}
						}
					}
				} else {
					fmt.Printf("%s  No active session or missing data, token not sent%s\n", dim, reset)
				}
				return

			case "expired":
				return
			}
		}
	}
}
