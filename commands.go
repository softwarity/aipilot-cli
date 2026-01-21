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

	"github.com/skip2/go-qrcode"
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

// printQRCode displays the QR code
func (d *Daemon) printQRCode(asImage bool) {
	qrData := QRData{
		Relay:        d.relay,
		Session:      d.session,
		Token:        d.token,
		Command:      d.command,
		WorkingDir:   d.workDir,
		AgentType:    d.agentType,
		AgentVersion: getAgentVersion(d.command, d.agentType),
		OS:           runtime.GOOS,
		CLIVersion:   Version,
	}
	qrJSON, _ := json.Marshal(qrData)

	if asImage {
		tmpDir := os.TempDir()
		qrFile := filepath.Join(tmpDir, fmt.Sprintf("aipilot-qr-%s.png", d.session[:8]))
		if err := qrcode.WriteFile(string(qrJSON), qrcode.Medium, 300, qrFile); err != nil {
			fmt.Printf("%sError generating QR code: %v%s\n", red, err, reset)
			return
		}
		fmt.Printf("\nQR code saved to: %s\n", qrFile)
		if err := openFile(qrFile); err != nil {
			fmt.Println("Please open the file manually.")
		} else {
			fmt.Println("QR code image opened.")
		}
	} else {
		qr, err := qrcode.New(string(qrJSON), qrcode.Medium)
		if err != nil {
			fmt.Printf("%sError generating QR code: %v%s\n", red, err, reset)
			return
		}
		fmt.Println()
		fmt.Println(qr.ToSmallString(false))
	}
}

// getAIPilotCommand checks if a line is an AIPilot command
func (d *Daemon) getAIPilotCommand(line string) string {
	switch line {
	case "/qr":
		return "qr"
	case "/cli-status":
		return "status"
	case "/disconnect":
		return "disconnect"
	case "/quit":
		return "quit"
	}
	return ""
}

// executeAIPilotCommand runs an AIPilot command
func (d *Daemon) executeAIPilotCommand(cmd string) {
	fmt.Println()
	switch cmd {
	case "qr":
		d.printQRCode(false)
	case "qr-image":
		d.printQRCode(true)
	case "status":
		d.printStatus()
	case "disconnect":
		d.disconnectMobile()
	case "quit":
		fmt.Printf("%sShutting down AIPilot...%s\n", yellow, reset)
		os.Exit(0)
	}
}

// showMenu displays the AIPilot interactive menu
func (d *Daemon) showMenu() {
	fmt.Printf("\n%s=== AIPilot Menu (Ctrl+A) ===%s\n", bold, reset)
	fmt.Printf("  %s[1]%s Show QR code\n", cyan, reset)
	fmt.Printf("  %s[2]%s Open QR as image\n", cyan, reset)
	fmt.Printf("  %s[3]%s Connection status\n", cyan, reset)
	fmt.Printf("  %s[4]%s Disconnect mobile\n", cyan, reset)
	fmt.Printf("  %s[q]%s Quit AIPilot\n", cyan, reset)
	fmt.Printf("  %s[Enter]%s Return to %s\n", cyan, reset, d.command)
	fmt.Print("\nChoice: ")

	var input string
	fmt.Scanln(&input)

	switch strings.ToLower(strings.TrimSpace(input)) {
	case "1":
		d.printQRCode(false)
	case "2":
		d.printQRCode(true)
	case "3":
		d.printStatus()
	case "4":
		d.disconnectMobile()
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
			if d.ptmx != nil && d.currentClient != "mobile" {
				d.mu.Unlock()
				d.switchToClient("mobile")
				go func() {
					time.Sleep(50 * time.Millisecond)
					d.sendToPTY([]byte{0x0C}) // Ctrl+L
				}()
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
