package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/skip2/go-qrcode"
)

// showPCStatus displays the PC configuration and paired mobiles
func showPCStatus(config *PCConfig) {
	fmt.Println()
	fmt.Printf("%s%s=== AIPilot PC Status ===%s\n", bold, cyan, reset)
	fmt.Println()
	fmt.Printf("  PC ID:      %s\n", config.PCID[:8]+"...")
	fmt.Printf("  PC Name:    %s\n", config.PCName)
	fmt.Printf("  Created:    %s\n", config.CreatedAt)
	fmt.Println()

	if len(config.PairedMobiles) == 0 {
		fmt.Printf("  %sNo paired mobile devices%s\n", dim, reset)
		fmt.Printf("\n  Run %saipilot-cli%s and type %s/qr%s to pair a mobile device.\n", cyan, reset, cyan, reset)
	} else {
		fmt.Printf("%s  Paired Mobiles:%s\n", bold, reset)
		for _, mobile := range config.PairedMobiles {
			fmt.Printf("    %s✓%s %s\n", green, reset, mobile.Name)
			fmt.Printf("      ID: %s\n", mobile.ID[:8]+"...")
			fmt.Printf("      Paired: %s\n", mobile.PairedAt)
		}
	}
	fmt.Println()
}

// handleUnpair removes a paired mobile device
func handleUnpair(config *PCConfig, client *RelayClient, mobileID string) error {
	// Find mobile by ID (can be partial match)
	var foundMobile *PairedMobile
	for _, m := range config.PairedMobiles {
		if m.ID == mobileID || m.ID[:8] == mobileID {
			foundMobile = &m
			break
		}
	}

	if foundMobile == nil {
		fmt.Printf("%sMobile device not found: %s%s\n", red, mobileID, reset)
		fmt.Println("\nPaired devices:")
		for _, m := range config.PairedMobiles {
			fmt.Printf("  - %s (ID: %s)\n", m.Name, m.ID[:8])
		}
		return nil
	}

	fmt.Printf("Unpairing %s (%s)...\n", foundMobile.Name, foundMobile.ID[:8])

	// Remove from relay
	if err := client.UnpairMobile(foundMobile.ID); err != nil {
		fmt.Printf("%sWarning: Could not notify relay: %v%s\n", yellow, err, reset)
		// Continue anyway - remove locally
	}

	// Remove locally
	config.removePairedMobile(foundMobile.ID)
	if err := savePCConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("%s✓ Successfully unpaired %s%s\n", green, foundMobile.Name, reset)
	return nil
}

// handlePairing initiates the pairing flow with a new mobile device
func handlePairing(config *PCConfig, client *RelayClient, relayURL string) error {
	fmt.Printf("%s%s=== Mobile Device Pairing ===%s\n", bold, cyan, reset)
	fmt.Println()

	// Initialize pairing on relay
	fmt.Printf("%sInitializing pairing...%s\n", dim, reset)
	pairingResp, err := client.InitPairing()
	if err != nil {
		return fmt.Errorf("failed to initialize pairing: %w", err)
	}

	// Create QR data (no session info during initial pairing)
	qrData := buildPairingQRData(config, relayURL, pairingResp.Token, nil)

	qrJSON, err := json.Marshal(qrData)
	if err != nil {
		return fmt.Errorf("failed to create QR data: %w", err)
	}

	// Display QR code
	fmt.Println()
	fmt.Printf("%sScan this QR code with the AIPilot mobile app:%s\n\n", bold, reset)
	printQRToTerminal(string(qrJSON))
	fmt.Println()
	fmt.Printf("  PC Name: %s\n", config.PCName)
	fmt.Printf("  Expires: %s\n", pairingResp.ExpiresAt)
	fmt.Println()
	fmt.Printf("%sWaiting for mobile to scan...%s\n", dim, reset)

	// Poll for pairing completion
	timeout := time.After(PairingTimeout)
	ticker := time.NewTicker(PairingPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			fmt.Printf("\n%sPairing timed out. Type /qr to retry.%s\n", red, reset)
			return nil
		case <-ticker.C:
			status, err := client.CheckPairingStatus(pairingResp.Token)
			if err != nil {
				// Silently retry on errors
				continue
			}

			switch status.Status {
			case "completed":
				// Pairing successful!
				mobile := PairedMobile{
					ID:        status.MobileID,
					Name:      status.MobileName,
					PublicKey: status.PublicKey,
					PairedAt:  time.Now().Format(time.RFC3339),
				}
				config.addPairedMobile(mobile)
				if err := savePCConfig(config); err != nil {
					return fmt.Errorf("failed to save config: %w", err)
				}

				fmt.Printf("\n%s✓ Paired: %s%s\n\n", green, mobile.Name, reset)
				return nil

			case "expired":
				fmt.Printf("\n%sPairing token expired. Type /qr to retry.%s\n", red, reset)
				return nil

			case "pending":
				// Still waiting, continue polling

			default:
				// Unknown status, log and continue
				fmt.Printf("%sUnknown pairing status: %s%s\n", yellow, status.Status, reset)
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

// printQRToTerminal prints a QR code to the terminal
func printQRToTerminal(data string) {
	qr, err := qrcode.New(data, qrcode.Medium)
	if err != nil {
		fmt.Printf("%sError generating QR code: %v%s\n", red, err, reset)
		return
	}
	fmt.Println(qr.ToSmallString(false))
}
