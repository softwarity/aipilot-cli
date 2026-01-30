package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/creack/pty"
)

// printStatus shows the current status
func (d *Daemon) printStatus() {
	d.mu.RLock()
	defer d.mu.RUnlock()

	fmt.Printf("\n%s=== AIPilot Status ===%s\n", bold, reset)
	fmt.Printf("  Session:  %s\n", d.session[:8]+"...")
	fmt.Printf("  Command:  %s\n", d.command)
	fmt.Printf("  WorkDir:  %s\n", d.workDir)
	fmt.Printf("  Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)

	// Relay status
	if d.relayConnected {
		fmt.Printf("  Relay:    %s✓ Connected%s\n", green, reset)
	} else {
		fmt.Printf("  Relay:    %s✗ Disconnected%s\n", red, reset)
	}

	// Mobile status
	if d.mobileConnected {
		fmt.Printf("  Mobile:   %s✓ Connected%s\n", green, reset)
	} else {
		fmt.Printf("  Mobile:   %s⋯ Waiting%s\n", yellow, reset)
	}

	// Agent status
	if d.running {
		fmt.Printf("  Agent:    %s✓ Running%s\n", green, reset)
	} else {
		fmt.Printf("  Agent:    %s✗ Stopped%s\n", red, reset)
	}
	fmt.Println()
}

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
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	timeout := time.After(5 * time.Minute)

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
					func() string { if sessionID != "" { return sessionID[:8] + "..." } else { return "none" } }(),
					func() string { if sessionToken != "" { return "present" } else { return "none" } }(),
					func() string { if mobile.PublicKey != "" { return mobile.PublicKey[:16] + "..." } else { return "none" } }(),
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

// showMenu displays the AIPilot interactive menu
func (d *Daemon) showMenu() {
	fmt.Printf("\n%s=== AIPilot Menu (Ctrl+A) ===%s\n", bold, reset)
	fmt.Printf("  %s[1]%s Show pairing QR\n", cyan, reset)
	fmt.Printf("  %s[2]%s Open QR as image\n", cyan, reset)
	fmt.Printf("  %s[3]%s Connection status\n", cyan, reset)
	fmt.Printf("  %s[4]%s Disconnect mobile\n", cyan, reset)
	fmt.Printf("  %s[5]%s Purge all sessions\n", cyan, reset)
	fmt.Printf("  %s[q]%s Quit AIPilot\n", cyan, reset)
	fmt.Printf("  %s[Enter]%s Return to %s\n", cyan, reset, d.command)
	fmt.Print("\nChoice: ")

	var input string
	fmt.Scanln(&input)

	switch strings.ToLower(strings.TrimSpace(input)) {
	case "1":
		d.executeAIPilotCommand("qr")
	case "2":
		d.executeAIPilotCommand("qr-image")
	case "3":
		d.printStatus()
	case "4":
		d.disconnectMobile()
	case "5":
		d.purgeAllSessions()
	case "q":
		fmt.Printf("%sShutting down AIPilot...%s\n", yellow, reset)
		os.Exit(0)
	default:
		fmt.Println("Returning to", d.command)
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

// handleControlMessage processes control messages from mobile
func (d *Daemon) handleControlMessage(msg string) {
	parts := strings.SplitN(msg, ":", 2)
	if len(parts) < 1 {
		return
	}

	cmd := parts[0]
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	switch cmd {
	case "resize":
		d.handleResizeCommand(args)

	case "info-request":
		d.sendCLIInfo()

	case "mobile-info":
		// Mobile sent its info (we could log/display it)

	case "ssh-setup-key":
		keyParts := strings.SplitN(args, ":", 2)
		if len(keyParts) == 2 {
			username := keyParts[0]
			keyBase64 := keyParts[1]
			go d.installSSHKey(username, keyBase64)
		}

	case "ssh-detect":
		go d.detectSSHServer()

	case "file-upload":
		fileParts := strings.SplitN(args, ":", 2)
		if len(fileParts) == 2 {
			fileName := fileParts[0]
			fileBase64 := fileParts[1]
			go d.saveUploadedFile(fileName, fileBase64)
		} else {
			d.sendControlMessage("file-upload-result:error:Invalid file upload format")
		}

	case "file-upload-start":
		d.handleChunkedUploadStart(args)

	case "file-upload-chunk":
		d.handleChunkedUploadChunk(args)
	}
}

// handleResizeCommand handles terminal resize from mobile
func (d *Daemon) handleResizeCommand(args string) {
	dims := strings.Split(args, ",")
	if len(dims) == 2 {
		cols := 0
		rows := 0
		fmt.Sscanf(dims[0], "%d", &cols)
		fmt.Sscanf(dims[1], "%d", &rows)
		if cols > 0 && rows > 0 {
			d.mu.Lock()
			d.mobileCols = cols
			d.mobileRows = rows

			// Always apply resize to PTY when in mobile mode or switching to mobile
			if d.ptmx != nil {
				if d.currentClient == "mobile" {
					// Already in mobile mode - just apply the new size
					pty.Setsize(d.ptmx, &pty.Winsize{
						Cols: uint16(cols),
						Rows: uint16(rows),
					})
					d.mu.Unlock()
					// Send Ctrl+L to refresh display
					go func() {
						time.Sleep(50 * time.Millisecond)
						d.sendToPTY([]byte{0x0C})
					}()
				} else {
					// Not in mobile mode - switch to mobile
					d.mu.Unlock()
					d.switchToClient("mobile")
				}
			} else {
				d.mu.Unlock()
			}
		}
	}
}

// sendCLIInfo sends CLI information to mobile
func (d *Daemon) sendCLIInfo() {
	hostname, _ := os.Hostname()

	currentUser := "unknown"
	if u, err := user.Current(); err == nil {
		currentUser = u.Username
	}

	var ips []string
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					ips = append(ips, ipnet.IP.String())
				}
			}
		}
	}

	// Quick SSH check
	sshPort := 22
	sshRunning := false
	if conn, err := net.DialTimeout("tcp", "127.0.0.1:22", 500*time.Millisecond); err == nil {
		conn.Close()
		sshRunning = true
	} else if conn, err := net.DialTimeout("tcp", "127.0.0.1:2222", 500*time.Millisecond); err == nil {
		conn.Close()
		sshPort = 2222
		sshRunning = true
	}

	info := map[string]interface{}{
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"hostname":   hostname,
		"user":       currentUser,
		"cliVersion": Version,
		"workingDir": d.workDir,
		"agent":      d.command,
		"agentType":  string(d.agentType),
		"sshRunning": sshRunning,
		"sshPort":    sshPort,
		"ips":        ips,
	}

	infoJSON, err := json.Marshal(info)
	if err != nil {
		return
	}

	d.sendControlMessage("cli-info:" + string(infoJSON))
}

// handleChunkedUploadStart handles start of chunked upload
func (d *Daemon) handleChunkedUploadStart(args string) {
	startParts := strings.SplitN(args, ":", 4)
	if len(startParts) == 4 {
		uploadId := startParts[0]
		fileName := startParts[1]
		totalChunks := 0
		totalSize := int64(0)
		fmt.Sscanf(startParts[2], "%d", &totalChunks)
		fmt.Sscanf(startParts[3], "%d", &totalSize)

		d.uploadMu.Lock()
		if d.chunkedUploads == nil {
			d.chunkedUploads = make(map[string]*ChunkedUpload)
		}
		d.chunkedUploads[uploadId] = &ChunkedUpload{
			FileName:    fileName,
			TotalChunks: totalChunks,
			TotalSize:   totalSize,
			Chunks:      make(map[int][]byte),
			ReceivedAt:  time.Now(),
		}
		d.uploadMu.Unlock()

		d.sendControlMessage(fmt.Sprintf("file-upload-ack:%s:started", uploadId))
	}
}

// handleChunkedUploadChunk handles a chunk of upload
func (d *Daemon) handleChunkedUploadChunk(args string) {
	chunkParts := strings.SplitN(args, ":", 3)
	if len(chunkParts) == 3 {
		uploadId := chunkParts[0]
		chunkIndex := 0
		fmt.Sscanf(chunkParts[1], "%d", &chunkIndex)
		chunkBase64 := chunkParts[2]

		chunkData, err := base64.StdEncoding.DecodeString(chunkBase64)
		if err != nil {
			d.sendControlMessage(fmt.Sprintf("file-upload-result:error:Invalid chunk data for %s", uploadId))
			return
		}

		d.uploadMu.Lock()
		upload, exists := d.chunkedUploads[uploadId]
		if !exists {
			d.uploadMu.Unlock()
			d.sendControlMessage(fmt.Sprintf("file-upload-result:error:Unknown upload %s", uploadId))
			return
		}

		upload.Chunks[chunkIndex] = chunkData

		// Check if all chunks received
		if len(upload.Chunks) == upload.TotalChunks {
			var fullData []byte
			for i := 0; i < upload.TotalChunks; i++ {
				if chunk, ok := upload.Chunks[i]; ok {
					fullData = append(fullData, chunk...)
				} else {
					d.uploadMu.Unlock()
					d.sendControlMessage(fmt.Sprintf("file-upload-result:error:Missing chunk %d for %s", i, uploadId))
					return
				}
			}

			fileName := upload.FileName
			delete(d.chunkedUploads, uploadId)
			d.uploadMu.Unlock()

			go d.saveUploadedFileBytes(fileName, fullData)
		} else {
			d.uploadMu.Unlock()
			d.sendControlMessage(fmt.Sprintf("file-upload-ack:%s:%d", uploadId, chunkIndex))
		}
	}
}

// saveUploadedFile saves a base64-encoded file to /tmp
func (d *Daemon) saveUploadedFile(fileName, fileBase64 string) {
	fileData, err := base64.StdEncoding.DecodeString(fileBase64)
	if err != nil {
		d.sendControlMessage("file-upload-result:error:Invalid file encoding")
		return
	}

	d.saveUploadedFileBytes(fileName, fileData)
}

// saveUploadedFileBytes saves file bytes to /tmp
func (d *Daemon) saveUploadedFileBytes(fileName string, fileData []byte) {
	fileName = filepath.Base(fileName)
	if fileName == "" || fileName == "." || fileName == ".." {
		d.sendControlMessage("file-upload-result:error:Invalid filename")
		return
	}

	timestamp := time.Now().UnixMilli()
	remotePath := filepath.Join(os.TempDir(), fmt.Sprintf("aipilot_%d_%s", timestamp, fileName))

	if err := os.WriteFile(remotePath, fileData, 0644); err != nil {
		d.sendControlMessage(fmt.Sprintf("file-upload-result:error:Failed to write file: %v", err))
		return
	}

	d.sendControlMessage(fmt.Sprintf("file-upload-result:success:%s", remotePath))
}
