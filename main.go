package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/skip2/go-qrcode"
	"golang.org/x/term"
)

// ANSI escape codes for terminal control
const (
	clearLine  = "\033[K"
	moveUp     = "\033[1A"
	moveToCol0 = "\r"
	dim        = "\033[2m"
	reset      = "\033[0m"
	green      = "\033[32m"
	yellow     = "\033[33m"
	red        = "\033[31m"
	cyan       = "\033[36m"
	bold       = "\033[1m"
)

// ChunkedUpload tracks a file being uploaded in chunks
type ChunkedUpload struct {
	FileName    string
	TotalChunks int
	TotalSize   int64
	Chunks      map[int][]byte
	ReceivedAt  time.Time
}

// Daemon manages the multiplexer state
type Daemon struct {
	mu   sync.RWMutex
	wsMu sync.Mutex // Mutex for WebSocket writes (gorilla/websocket doesn't support concurrent writes)

	// Connection state
	wsConn          *websocket.Conn
	mobileConnected bool
	relayConnected  bool

	// PTY
	ptmx    *os.File
	cmd     *exec.Cmd
	running bool

	// Session info
	session   string
	token     string
	relay     string
	command   string
	workDir   string
	agentType AgentType

	// E2E Encryption
	aesGCM cipher.AEAD

	// Input buffer for command detection (PC stdin)
	inputBuffer string
	inputMu     sync.Mutex

	// Mobile input buffer for command detection
	mobileLineBuf string

	// Terminal state
	oldState *term.State
	stdinFd  int

	// Dynamic resize: track both client sizes
	pcCols, pcRows         int
	mobileCols, mobileRows int
	currentClient          string // "pc" or "mobile"

	// Debounce timer for PC switch (avoid losing first char)
	pcSwitchTimer *time.Timer

	// Chunked file uploads in progress
	chunkedUploads map[string]*ChunkedUpload
	uploadMu       sync.Mutex
}

// initEncryption derives AES-256-GCM key from token
func (d *Daemon) initEncryption() error {
	// Derive 32-byte key from token using SHA256
	hash := sha256.Sum256([]byte(d.token))

	block, err := aes.NewCipher(hash[:])
	if err != nil {
		return fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("failed to create GCM: %w", err)
	}

	d.aesGCM = gcm
	return nil
}

// encrypt encrypts data using AES-GCM
// Returns base64(nonce || ciphertext)
func (d *Daemon) encrypt(plaintext []byte) (string, error) {
	if d.aesGCM == nil {
		return "", fmt.Errorf("encryption not initialized")
	}

	// Generate random nonce
	nonce := make([]byte, d.aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and append to nonce
	ciphertext := d.aesGCM.Seal(nonce, nonce, plaintext, nil)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decrypts base64(nonce || ciphertext) using AES-GCM
func (d *Daemon) decrypt(encoded string) ([]byte, error) {
	if d.aesGCM == nil {
		return nil, fmt.Errorf("encryption not initialized")
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	nonceSize := d.aesGCM.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := d.aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plaintext, nil
}

func (d *Daemon) isMobileConnected() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.mobileConnected
}

func (d *Daemon) setMobileConnected(connected bool) {
	d.mu.Lock()
	d.mobileConnected = connected
	d.mu.Unlock()
}

func (d *Daemon) isRelayConnected() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.relayConnected
}

func (d *Daemon) setRelayConnected(connected bool) {
	d.mu.Lock()
	d.relayConnected = connected
	d.mu.Unlock()
}

func (d *Daemon) isRunning() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.running
}

// printStatus shows the current status (non-blocking, for /status command)
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
// Returns the command name if it is, empty string otherwise
// Uses /cli-status instead of /status to avoid conflict with Claude's /status
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
	fmt.Println() // New line after the cleared input
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

// handleControlMessage processes control messages from mobile
// Format: command:args (e.g., "resize:80,24")
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
		// Parse cols,rows
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
					pty.Setsize(d.ptmx, &pty.Winsize{
						Cols: uint16(cols),
						Rows: uint16(rows),
					})
					d.currentClient = "mobile"
					go func() {
						time.Sleep(50 * time.Millisecond)
						d.sendToPTY([]byte{0x0C}) // Ctrl+L
					}()
				}
				d.mu.Unlock()
			}
		}

	case "info-request":
		// Mobile requests CLI info - send it
		d.sendCLIInfo()

	case "mobile-info":
		// Mobile sent its info (we could log/display it)
		// For now, just acknowledge receipt

	case "ssh-setup-key":
		// Mobile sent SSH public key to install
		// Format: ssh-setup-key:username:base64-pubkey
		keyParts := strings.SplitN(args, ":", 2)
		if len(keyParts) == 2 {
			username := keyParts[0]
			keyBase64 := keyParts[1]
			go d.installSSHKey(username, keyBase64)
		}

	case "ssh-detect":
		// Mobile requests SSH server detection
		go d.detectSSHServer()

	case "file-upload":
		// Single-message file upload (small files)
		// Format: file-upload:<filename>:<base64-data>
		fileParts := strings.SplitN(args, ":", 2)
		if len(fileParts) == 2 {
			fileName := fileParts[0]
			fileBase64 := fileParts[1]
			go d.saveUploadedFile(fileName, fileBase64)
		} else {
			d.sendControlMessage("file-upload-result:error:Invalid file upload format")
		}

	case "file-upload-start":
		// Start chunked upload
		// Format: file-upload-start:<uploadId>:<filename>:<totalChunks>:<totalSize>
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

	case "file-upload-chunk":
		// Receive a chunk
		// Format: file-upload-chunk:<uploadId>:<chunkIndex>:<base64-data>
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
				// Reassemble file
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

				// Save the reassembled file
				go d.saveUploadedFileBytes(fileName, fullData)
			} else {
				d.uploadMu.Unlock()
				// Acknowledge chunk receipt
				d.sendControlMessage(fmt.Sprintf("file-upload-ack:%s:%d", uploadId, chunkIndex))
			}
		}
	}
}

// sendCLIInfo sends CLI information to mobile (including SSH detection)
func (d *Daemon) sendCLIInfo() {
	hostname, _ := os.Hostname()

	// Get current user
	currentUser := "unknown"
	if u, err := user.Current(); err == nil {
		currentUser = u.Username
	}

	// Get local IP addresses
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

	// Detect SSH server
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

// installSSHKey installs an SSH public key to authorized_keys
func (d *Daemon) installSSHKey(username, keyBase64 string) {
	// Decode base64 key
	keyBytes, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		d.sendControlMessage("ssh-setup-result:error:Invalid key encoding")
		return
	}
	publicKey := strings.TrimSpace(string(keyBytes))

	// Get home directory
	home, err := os.UserHomeDir()
	if err != nil {
		d.sendControlMessage("ssh-setup-result:error:Cannot find home directory")
		return
	}

	// Create .ssh directory if it doesn't exist
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		d.sendControlMessage("ssh-setup-result:error:Cannot create .ssh directory")
		return
	}

	// Path to authorized_keys
	authKeysPath := filepath.Join(sshDir, "authorized_keys")

	// Check if key already exists
	existingKeys, _ := os.ReadFile(authKeysPath)
	if strings.Contains(string(existingKeys), publicKey) {
		d.sendControlMessage("ssh-setup-result:success:Key already installed")
		return
	}

	// Append key to authorized_keys
	f, err := os.OpenFile(authKeysPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		d.sendControlMessage("ssh-setup-result:error:Cannot open authorized_keys")
		return
	}
	defer f.Close()

	// Add newline if file doesn't end with one
	if len(existingKeys) > 0 && existingKeys[len(existingKeys)-1] != '\n' {
		f.WriteString("\n")
	}

	// Write the key with a comment
	hostname, _ := os.Hostname()
	keyLine := fmt.Sprintf("%s aipilot-mobile@%s\n", publicKey, hostname)
	if _, err := f.WriteString(keyLine); err != nil {
		d.sendControlMessage("ssh-setup-result:error:Cannot write key")
		return
	}

	d.sendControlMessage("ssh-setup-result:success:Key installed successfully")

	// Log to terminal
	fmt.Printf("\n%s[AIPilot] SSH key installed for mobile access%s\n", green, reset)
}

// saveUploadedFile saves a base64-encoded file to /tmp
func (d *Daemon) saveUploadedFile(fileName, fileBase64 string) {
	// Decode base64 data
	fileData, err := base64.StdEncoding.DecodeString(fileBase64)
	if err != nil {
		d.sendControlMessage("file-upload-result:error:Invalid file encoding")
		return
	}

	d.saveUploadedFileBytes(fileName, fileData)
}

// saveUploadedFileBytes saves file bytes to /tmp
func (d *Daemon) saveUploadedFileBytes(fileName string, fileData []byte) {
	// Sanitize filename (remove path components for security)
	fileName = filepath.Base(fileName)
	if fileName == "" || fileName == "." || fileName == ".." {
		d.sendControlMessage("file-upload-result:error:Invalid filename")
		return
	}

	// Generate unique path in /tmp
	timestamp := time.Now().UnixMilli()
	remotePath := filepath.Join(os.TempDir(), fmt.Sprintf("aipilot_%d_%s", timestamp, fileName))

	// Write file
	if err := os.WriteFile(remotePath, fileData, 0644); err != nil {
		d.sendControlMessage(fmt.Sprintf("file-upload-result:error:Failed to write file: %v", err))
		return
	}

	// Success
	d.sendControlMessage(fmt.Sprintf("file-upload-result:success:%s", remotePath))
}

// detectSSHServer detects if SSH server is running and returns connection info
func (d *Daemon) detectSSHServer() {
	fmt.Printf("\n%s[AIPilot] Detecting SSH server...%s\n", cyan, reset)

	// Get current user
	currentUser := "unknown"
	if u, err := user.Current(); err == nil {
		currentUser = u.Username
	}

	// Get hostname
	hostname, _ := os.Hostname()

	// Get local IP addresses
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

	// Try to detect SSH port by checking common ports
	sshPort := 22
	sshRunning := false

	// Check if SSH is listening on port 22
	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", sshPort), time.Second); err == nil {
		conn.Close()
		sshRunning = true
	} else {
		// Try port 2222 as fallback
		if conn, err := net.DialTimeout("tcp", "127.0.0.1:2222", time.Second); err == nil {
			conn.Close()
			sshPort = 2222
			sshRunning = true
		}
	}

	// Build result
	result := map[string]interface{}{
		"running":  sshRunning,
		"port":     sshPort,
		"user":     currentUser,
		"hostname": hostname,
		"ips":      ips,
	}

	resultJSON, _ := json.Marshal(result)
	d.sendControlMessage("ssh-detect-result:" + string(resultJSON))

	if sshRunning {
		fmt.Printf("%s[AIPilot] SSH server detected on port %d%s\n", green, sshPort, reset)
		if len(ips) > 0 {
			fmt.Printf("%s[AIPilot] Available IPs: %s%s\n", green, strings.Join(ips, ", "), reset)
		}
	} else {
		fmt.Printf("%s[AIPilot] No SSH server detected%s\n", yellow, reset)
	}
}

// forceResize forces PTY to match the local terminal size
func (d *Daemon) forceResize() {
	width, height, err := term.GetSize(d.stdinFd)
	if err != nil {
		fmt.Printf("%sError getting terminal size: %v%s\n", red, err, reset)
		return
	}

	d.mu.RLock()
	ptmx := d.ptmx
	d.mu.RUnlock()

	if ptmx != nil {
		pty.Setsize(ptmx, &pty.Winsize{
			Cols: uint16(width),
			Rows: uint16(height),
		})
		fmt.Printf("%sResized to %dx%d%s\n", green, width, height, reset)
	}
}

// schedulePCSwitch schedules a debounced switch to PC mode
// This allows multiple characters to be typed before the resize happens
func (d *Daemon) schedulePCSwitch() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Already on PC, nothing to do
	if d.currentClient == "pc" {
		return
	}

	// Cancel existing timer if any
	if d.pcSwitchTimer != nil {
		d.pcSwitchTimer.Stop()
	}

	// Schedule switch after 100ms of no input
	d.pcSwitchTimer = time.AfterFunc(100*time.Millisecond, func() {
		d.switchToClient("pc")
	})
}

// switchToClient switches the PTY to the specified client's dimensions
// This is called when we detect input from a different client
func (d *Daemon) switchToClient(client string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Already on this client
	if d.currentClient == client {
		return
	}

	var cols, rows int
	if client == "mobile" {
		cols, rows = d.mobileCols, d.mobileRows
	} else {
		cols, rows = d.pcCols, d.pcRows
	}

	// Don't switch if we don't have dimensions for target client
	if cols <= 0 || rows <= 0 {
		return
	}

	// Apply resize
	if d.ptmx != nil {
		pty.Setsize(d.ptmx, &pty.Winsize{
			Cols: uint16(cols),
			Rows: uint16(rows),
		})
		d.currentClient = client

		// Send Ctrl+L to force screen redraw only when switching TO mobile
		// (PC terminal handles resize natively, and Ctrl+L would eat the first character)
		if client == "mobile" {
			go func() {
				time.Sleep(50 * time.Millisecond)
				d.sendToPTY([]byte{0x0C}) // Ctrl+L
			}()
		}

		// Notify mobile of mode change via control channel (after releasing lock)
		go func(c string) {
			d.sendControlMessage("mode:" + c)
		}(client)
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
		// Send disconnect notification
		msg := Message{Type: "kick"}
		d.wsMu.Lock()
		d.wsConn.WriteJSON(msg)
		d.wsMu.Unlock()
		d.mobileConnected = false
		fmt.Printf("%sMobile disconnected.%s\n", green, reset)
	}
}

// sendControlMessage sends a control message to mobile via the data channel
// Format: \x00CTRL:message
func (d *Daemon) sendControlMessage(msg string) {
	d.mu.RLock()
	conn := d.wsConn
	connected := d.mobileConnected && d.relayConnected
	d.mu.RUnlock()

	if connected && conn != nil {
		// Build control message: \x00CTRL:msg
		ctrlData := append([]byte{0x00}, []byte("CTRL:"+msg)...)

		// Encrypt
		encrypted, err := d.encrypt(ctrlData)
		if err != nil {
			encrypted = base64.StdEncoding.EncodeToString(ctrlData)
		}

		wsMsg := Message{
			Type:    "data",
			Payload: encrypted,
		}
		d.wsMu.Lock()
		conn.WriteJSON(wsMsg)
		d.wsMu.Unlock()
	}
}

// sendToMobile sends data to mobile via WebSocket
func (d *Daemon) sendToMobile(data []byte) {
	d.mu.RLock()
	conn := d.wsConn
	connected := d.mobileConnected && d.relayConnected
	d.mu.RUnlock()

	if connected && conn != nil {
		// Encrypt data before sending
		encrypted, err := d.encrypt(data)
		if err != nil {
			// Fallback to unencrypted if encryption fails (shouldn't happen)
			encrypted = base64.StdEncoding.EncodeToString(data)
		}

		msg := Message{
			Type:    "data",
			Payload: encrypted,
		}
		d.wsMu.Lock()
		conn.WriteJSON(msg)
		d.wsMu.Unlock()
	}
}

// sendToPTY sends data to the PTY (and thus to Claude)
func (d *Daemon) sendToPTY(data []byte) {
	d.mu.RLock()
	ptmx := d.ptmx
	d.mu.RUnlock()

	if ptmx != nil {
		ptmx.Write(data)
	}
}

// openFile opens a file or URL with the system's default application
func openFile(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// Version and RelayURL are set at build time via -ldflags
var (
	Version  = "dev"
	Build    = "20260120-1" // Increment this for each build
	RelayURL = "wss://aipilot-relay.francois-achache.workers.dev"
)

const defaultCommand = "claude"

// SessionData represents a saved session for persistence
type SessionData struct {
	Session   string `json:"session"`
	Token     string `json:"token"`
	Relay     string `json:"relay"`
	Command   string `json:"command"`
	WorkDir   string `json:"workdir"`
	CreatedAt string `json:"created_at"`
}

func getSessionFilePath(workDir string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	h := sha256.Sum256([]byte(workDir))
	hash := hex.EncodeToString(h[:8])
	return filepath.Join(home, ".aipilot", "sessions", fmt.Sprintf("%s.json", hash))
}

func loadSession(workDir string) (*SessionData, error) {
	path := getSessionFilePath(workDir)
	if path == "" {
		return nil, fmt.Errorf("cannot determine home directory")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var session SessionData
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}

	return &session, nil
}

func saveSession(workDir string, session *SessionData) error {
	path := getSessionFilePath(workDir)
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

func generateRandomToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// AgentType represents known AI agent types
type AgentType string

const (
	AgentClaude  AgentType = "claude"
	AgentGemini  AgentType = "gemini"
	AgentOpenAI  AgentType = "openai"
	AgentAider   AgentType = "aider"
	AgentGeneric AgentType = "generic"
)

// QRData is encoded in the QR code for mobile to scan
type QRData struct {
	Relay        string    `json:"r"`
	Session      string    `json:"s"`
	Token        string    `json:"t"`
	Command      string    `json:"c,omitempty"`
	WorkingDir   string    `json:"w,omitempty"`
	AgentType    AgentType `json:"a,omitempty"`
	AgentVersion string    `json:"av,omitempty"`
	OS           string    `json:"os,omitempty"`
	CLIVersion   string    `json:"cv,omitempty"`
}

// Message types for WebSocket communication
type Message struct {
	Type    string `json:"type"`
	Session string `json:"session,omitempty"`
	Token   string `json:"token,omitempty"`
	Role    string `json:"role,omitempty"`
	Payload string `json:"payload,omitempty"`
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
	Error   string `json:"error,omitempty"`
}

// AgentInfo contains information about a detected agent
type AgentInfo struct {
	Command  string
	Type     AgentType
	Version  string
	Available bool
}

// knownAgents lists all known AI terminal agents to detect
// Only includes actual CLI-based agents, not IDE extensions
var knownAgents = []struct {
	command     string
	agentType   AgentType
	displayName string
}{
	{"claude", AgentClaude, "Claude Code"},
	{"aider", AgentAider, "Aider"},
	{"gemini", AgentGemini, "Google Gemini CLI"},
}

func checkCommand(command string) (string, error) {
	path, err := exec.LookPath(command)
	if err != nil {
		return "", fmt.Errorf("command '%s' not found in PATH", command)
	}
	return path, nil
}

func detectAgentType(command string) AgentType {
	cmd := strings.ToLower(command)
	switch {
	case strings.Contains(cmd, "claude"):
		return AgentClaude
	case strings.Contains(cmd, "gemini"):
		return AgentGemini
	case strings.Contains(cmd, "gpt") || strings.Contains(cmd, "codex") || strings.Contains(cmd, "openai"):
		return AgentOpenAI
	case strings.Contains(cmd, "aider"):
		return AgentAider
	default:
		return AgentGeneric
	}
}

func getAgentVersion(command string, agentType AgentType) string {
	cmd := exec.Command(command, "--version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	version := strings.TrimSpace(strings.Split(string(output), "\n")[0])

	// Truncate if too long (some commands output weird stuff)
	if len(version) > 50 {
		version = version[:47] + "..."
	}

	// Skip if it looks like a prompt or error
	if strings.Contains(strings.ToLower(version), "install") ||
		strings.Contains(strings.ToLower(version), "error") ||
		strings.Contains(version, "?") {
		return ""
	}

	return version
}

// detectAvailableAgents scans for installed AI agents
func detectAvailableAgents() []AgentInfo {
	var available []AgentInfo

	for _, agent := range knownAgents {
		if _, err := exec.LookPath(agent.command); err == nil {
			version := getAgentVersion(agent.command, agent.agentType)
			available = append(available, AgentInfo{
				Command:   agent.command,
				Type:      agent.agentType,
				Version:   version,
				Available: true,
			})
		}
	}

	return available
}

// selectAgent prompts user to select an agent if multiple are available
func selectAgent(agents []AgentInfo) string {
	if len(agents) == 0 {
		return ""
	}

	if len(agents) == 1 {
		fmt.Printf("%sDetected: %s%s\n", dim, agents[0].Command, reset)
		return agents[0].Command
	}

	// Multiple agents available - show menu
	fmt.Printf("\n%s=== Available AI Agents ===%s\n", bold, reset)
	for i, agent := range agents {
		versionStr := ""
		if agent.Version != "" {
			versionStr = fmt.Sprintf(" %s(%s)%s", dim, agent.Version, reset)
		}
		fmt.Printf("  %s[%d]%s %s%s\n", cyan, i+1, reset, agent.Command, versionStr)
	}
	fmt.Println()
	fmt.Printf("Select agent [1-%d]: ", len(agents))

	// Read selection
	var input string
	fmt.Scanln(&input)

	// Parse selection
	var selection int
	if _, err := fmt.Sscanf(input, "%d", &selection); err != nil || selection < 1 || selection > len(agents) {
		// Default to first agent
		fmt.Printf("%sUsing default: %s%s\n", dim, agents[0].Command, reset)
		return agents[0].Command
	}

	return agents[selection-1].Command
}

func main() {
	// Parse flags
	relay := flag.String("relay", RelayURL, "WebSocket relay base URL")
	command := flag.String("command", "", "Command to run (e.g., claude, aider). Auto-detects if not specified.")
	workDir := flag.String("workdir", "", "Working directory")
	showVersion := flag.Bool("version", false, "Show version and exit")
	qrImage := flag.Bool("qr", false, "Open QR code as image (better for scanning)")
	newSession := flag.Bool("new", false, "Force create a new session")
	listAgents := flag.Bool("list", false, "List available AI agents and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("aipilot-cli version %s\n", Version)
		os.Exit(0)
	}

	// List agents mode
	if *listAgents {
		agents := detectAvailableAgents()
		if len(agents) == 0 {
			fmt.Println("No AI agents found in PATH.")
			fmt.Println("Supported agents: claude, aider, codex, copilot, cursor")
			os.Exit(1)
		}
		fmt.Printf("\n%s=== Available AI Agents ===%s\n", bold, reset)
		for _, agent := range agents {
			versionStr := ""
			if agent.Version != "" {
				versionStr = fmt.Sprintf(" (%s)", agent.Version)
			}
			fmt.Printf("  %s✓%s %s%s\n", green, reset, agent.Command, versionStr)
		}
		fmt.Println()
		os.Exit(0)
	}

	// Use current directory if not specified
	if *workDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			log.Fatal("Failed to get working directory:", err)
		}
		*workDir = wd
	}

	// Try to load existing session first (before agent selection)
	var session, token, selectedCommand string
	var sessionResumed bool

	if !*newSession {
		if savedSession, err := loadSession(*workDir); err == nil {
			if savedSession.Relay == *relay {
				// Session exists for this workdir - use it
				session = savedSession.Session
				token = savedSession.Token
				selectedCommand = savedSession.Command
				sessionResumed = true
				fmt.Printf("%sSession found for this directory, using saved session...%s\n", dim, reset)
			} else {
				fmt.Printf("%sSession found but relay mismatch (saved: %s, current: %s)%s\n", dim, savedSession.Relay, *relay, reset)
			}
		} else {
			fmt.Printf("%sNo saved session found for %s%s\n", dim, *workDir, reset)
		}
	}

	// Agent selection only if no existing session
	if selectedCommand == "" {
		selectedCommand = *command
		if selectedCommand == "" {
			// Auto-detect available agents
			agents := detectAvailableAgents()
			if len(agents) == 0 {
				fmt.Printf("%sNo AI agents found in PATH.%s\n", red, reset)
				fmt.Println("Supported agents: claude, aider, codex, copilot, cursor")
				fmt.Println("Install one of these agents or specify with -command flag.")
				os.Exit(1)
			}
			selectedCommand = selectAgent(agents)
		} else {
			// Verify specified command exists
			if _, err := checkCommand(selectedCommand); err != nil {
				log.Fatalf("Error: %v\nPlease ensure '%s' is installed and in your PATH.", err, selectedCommand)
			}
		}
	}

	// Detect agent type and version
	agentType := detectAgentType(selectedCommand)
	agentVersion := getAgentVersion(selectedCommand, agentType)

	// Create new session if needed
	if session == "" {
		session = uuid.New().String()
		token = generateRandomToken()

		sessionData := &SessionData{
			Session:   session,
			Token:     token,
			Relay:     *relay,
			Command:   selectedCommand,
			WorkDir:   *workDir,
			CreatedAt: time.Now().Format(time.RFC3339),
		}
		saveSession(*workDir, sessionData)
	}

	// Create daemon
	daemon := &Daemon{
		session:   session,
		token:     token,
		relay:     *relay,
		command:   selectedCommand,
		workDir:   *workDir,
		agentType: agentType,
		stdinFd:   int(os.Stdin.Fd()),
	}

	// Initialize E2E encryption
	if err := daemon.initEncryption(); err != nil {
		log.Fatal("Failed to initialize encryption:", err)
	}

	// Display header
	fmt.Println()
	fmt.Printf("%s%sAIPilot CLI%s %s[%s]%s\n", bold, cyan, reset, dim, Build, reset)
	fmt.Println()

	// Connect to relay early to check if mobile is already waiting
	go daemon.connectToRelay()

	// Wait a bit to see if mobile is already connected
	if sessionResumed {
		fmt.Printf("%sChecking if mobile is already connected...%s\n", dim, reset)
		time.Sleep(800 * time.Millisecond)
	}

	// Show QR code only if mobile is not already connected
	if !daemon.isMobileConnected() {
		if *qrImage {
			daemon.printQRCode(true)
		} else {
			daemon.printQRCode(false)
			fmt.Printf("%sTip: Use -qr flag for easier scanning%s\n", dim, reset)
		}
		fmt.Println()
	} else {
		fmt.Printf("%s✓ Mobile already connected!%s\n\n", green, reset)
	}

	// Display session info
	fmt.Printf("  Session:  %s", session[:8]+"...")
	if sessionResumed {
		fmt.Printf(" %s(resumed)%s", dim, reset)
	}
	fmt.Println()
	fmt.Printf("  Command:  %s", selectedCommand)
	if agentVersion != "" {
		fmt.Printf(" %s(%s)%s", dim, agentVersion, reset)
	}
	fmt.Println()
	fmt.Printf("  WorkDir:  %s\n", *workDir)
	fmt.Printf("  Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Println()
	fmt.Printf("%sAIPilot: /qr /cli-status /disconnect /quit%s\n", dim, reset)
	fmt.Println()

	// Start PTY IMMEDIATELY (daemon mode - don't wait for mobile)
	fmt.Printf("Starting %s...\n", selectedCommand)
	cmd := exec.Command(selectedCommand)
	cmd.Dir = *workDir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Fatal("Failed to start PTY:", err)
	}
	defer ptmx.Close()

	daemon.mu.Lock()
	daemon.ptmx = ptmx
	daemon.cmd = cmd
	daemon.running = true
	daemon.mu.Unlock()

	// Set initial terminal size and store PC dimensions
	if term.IsTerminal(daemon.stdinFd) {
		width, height, err := term.GetSize(daemon.stdinFd)
		if err == nil && width > 0 && height > 0 {
			pty.Setsize(ptmx, &pty.Winsize{
				Cols: uint16(width),
				Rows: uint16(height),
			})
			// Store PC dimensions for dynamic resize
			daemon.pcCols = width
			daemon.pcRows = height
			daemon.currentClient = "pc" // Start with PC active
		}
	}

	// Handle termination
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	// Handle window resize
	resizeChan := setupResizeSignal()

	// Note: relay connection already started earlier (before QR display)

	// PTY -> stdout AND mobile (dual output)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				if err != io.EOF {
					// Silent
				}
				daemon.mu.Lock()
				daemon.running = false
				daemon.mu.Unlock()
				return
			}

			// Write to local terminal
			os.Stdout.Write(buf[:n])

			// Send to mobile if connected
			daemon.sendToMobile(buf[:n])
		}
	}()

	// Setup raw terminal for local input
	var oldState *term.State
	if term.IsTerminal(daemon.stdinFd) {
		var err error
		oldState, err = term.MakeRaw(daemon.stdinFd)
		if err != nil {
			fmt.Printf("%sWarning: Could not set raw mode: %v%s\n", yellow, err, reset)
		} else {
			daemon.oldState = oldState
			defer term.Restore(daemon.stdinFd, oldState)
		}
	}

	// stdin -> PTY (pass-through with AIPilot command detection)
	// Everything goes to PTY, but we buffer to detect /commands
	// If line is an AIPilot command, we clear Claude's input and execute it
	go func() {
		lineBuf := ""
		inEscapeSeq := false

		for {
			b := make([]byte, 1)
			n, err := os.Stdin.Read(b)
			if err != nil || n == 0 {
				return
			}

			char := b[0]

			// Track escape sequences (terminal responses, not user input)
			if char == 0x1b { // ESC
				inEscapeSeq = true
				daemon.sendToPTY(b)
				continue
			} else if inEscapeSeq {
				// End of escape sequence detection (simplified)
				if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || char == '~' {
					inEscapeSeq = false
				}
				// Don't switch to PC for escape sequences - these are terminal responses
				daemon.sendToPTY(b)
				continue
			}

			// For printable characters: send to PTY, then schedule debounced switch to PC
			// This prevents the first character from being lost during resize
			if !inEscapeSeq && char >= 32 && char < 127 {
				lineBuf += string(char)
				daemon.sendToPTY(b)
				daemon.schedulePCSwitch()
				continue
			}

			// Enter key
			if char == '\r' || char == '\n' {
				// Check if line is an AIPilot command
				cmd := strings.TrimSpace(strings.ToLower(lineBuf))
				if aipilotCmd := daemon.getAIPilotCommand(cmd); aipilotCmd != "" {
					// Clear Claude's input line (Ctrl+U)
					daemon.sendToPTY([]byte{0x15})

					// Temporarily restore terminal for command output
					if oldState != nil {
						term.Restore(daemon.stdinFd, oldState)
					}

					daemon.executeAIPilotCommand(aipilotCmd)

					// Re-enable raw mode
					if oldState != nil {
						oldState, _ = term.MakeRaw(daemon.stdinFd)
						daemon.oldState = oldState
					}
				} else {
					// Normal input - send Enter to PTY
					daemon.sendToPTY(b)
				}
				lineBuf = ""
				continue
			}

			// Backspace
			if char == 127 || char == 8 {
				if len(lineBuf) > 0 {
					lineBuf = lineBuf[:len(lineBuf)-1]
				}
				daemon.sendToPTY(b)
				continue
			}

			// Ctrl+C - reset buffer
			if char == 3 {
				lineBuf = ""
				daemon.sendToPTY(b)
				continue
			}

			// Ctrl+U - reset buffer (kill line)
			if char == 0x15 {
				lineBuf = ""
				daemon.sendToPTY(b)
				continue
			}

			// Other characters (control chars, etc.) - just send to PTY
			daemon.sendToPTY(b)
		}
	}()

	// Handle resize signals (PC terminal resize)
	go func() {
		for range resizeChan {
			if term.IsTerminal(daemon.stdinFd) {
				width, height, err := term.GetSize(daemon.stdinFd)
				if err == nil && width > 0 && height > 0 {
					// Store PC dimensions
					daemon.mu.Lock()
					daemon.pcCols = width
					daemon.pcRows = height
					// If currently on PC, apply resize immediately
					if daemon.currentClient == "pc" || daemon.currentClient == "" {
						if daemon.ptmx != nil {
							pty.Setsize(daemon.ptmx, &pty.Winsize{
								Cols: uint16(width),
								Rows: uint16(height),
							})
						}
						daemon.currentClient = "pc"
					}
					daemon.mu.Unlock()
				}
			}
		}
	}()

	// Wait for termination
	select {
	case <-sigChan:
		fmt.Println("\n\nShutting down AIPilot...")
	case err := <-waitForProcess(cmd):
		if err != nil {
			fmt.Println("\n\nProcess exited with error:", err)
		} else {
			fmt.Println("\n\nProcess exited.")
		}
	}
}

// connectToRelay connects to the WebSocket relay
func (d *Daemon) connectToRelay() {
	for {
		wsURL := fmt.Sprintf("%s/ws/%s?role=bridge", d.relay, d.session)
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			d.setRelayConnected(false)
			time.Sleep(5 * time.Second)
			continue
		}

		// Wait for registration confirmation
		var response Message
		if err := conn.ReadJSON(&response); err != nil {
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		if response.Type != "registered" {
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		d.mu.Lock()
		d.wsConn = conn
		d.relayConnected = true
		d.mu.Unlock()

		// Start ping keepalive
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				d.mu.RLock()
				c := d.wsConn
				connected := d.relayConnected
				d.mu.RUnlock()
				if !connected || c == nil {
					return
				}
				d.wsMu.Lock()
				err := c.WriteJSON(Message{Type: "ping"})
				d.wsMu.Unlock()
				if err != nil {
					return
				}
				<-ticker.C
			}
		}()

		// Handle incoming messages
		d.handleWebSocketMessages(conn)

		// Connection lost, retry
		d.mu.Lock()
		d.wsConn = nil
		d.relayConnected = false
		d.mobileConnected = false
		d.mu.Unlock()

		time.Sleep(2 * time.Second)
	}
}

// handleWebSocketMessages processes incoming WebSocket messages
func (d *Daemon) handleWebSocketMessages(conn *websocket.Conn) {
	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}

		switch msg.Type {
		case "data":
			// Data from mobile -> PTY (decrypt first)
			data, err := d.decrypt(msg.Payload)
			if err != nil {
				// Try unencrypted fallback for backwards compatibility
				data, err = base64.StdEncoding.DecodeString(msg.Payload)
				if err != nil {
					continue
				}
			}

			// If we receive data, mobile is definitely connected
			// (handles case where relay doesn't send "connected" message after CLI reconnect)
			if !d.isMobileConnected() {
				d.setMobileConnected(true)
			}

			// Check for control messages (format: \x00CTRL:command:args)
			if len(data) > 6 && data[0] == 0x00 && string(data[1:6]) == "CTRL:" {
				ctrlMsg := string(data[6:])
				d.handleControlMessage(ctrlMsg)
				continue
			}

			// Switch to mobile dimensions when mobile starts typing
			d.switchToClient("mobile")

			// Buffer mobile input for command detection
			for _, char := range data {
				if char == '\r' || char == '\n' {
					// Check if it's an AIPilot command
					cmd := strings.TrimSpace(strings.ToLower(d.mobileLineBuf))
					if aipilotCmd := d.getAIPilotCommand(cmd); aipilotCmd != "" {
						// Clear the line in PTY (Ctrl+U) and don't send Enter
						d.sendToPTY([]byte{0x15})
						d.executeAIPilotCommand(aipilotCmd)
						d.mobileLineBuf = ""
						continue
					}
					// Normal Enter - send to PTY
					d.sendToPTY([]byte{char})
					d.mobileLineBuf = ""
				} else if char == 127 || char == 8 { // Backspace
					if len(d.mobileLineBuf) > 0 {
						d.mobileLineBuf = d.mobileLineBuf[:len(d.mobileLineBuf)-1]
					}
					d.sendToPTY([]byte{char})
				} else if char == 3 { // Ctrl+C
					d.mobileLineBuf = ""
					d.sendToPTY([]byte{char})
				} else if char >= 32 && char < 127 { // Printable
					d.mobileLineBuf += string(char)
					d.sendToPTY([]byte{char})
				} else {
					// Other chars - pass through
					d.sendToPTY([]byte{char})
				}
			}

		case "connected":
			if msg.Role == "mobile" {
				d.setMobileConnected(true)
				// Trigger screen refresh
				d.mu.RLock()
				ptmx := d.ptmx
				d.mu.RUnlock()
				if ptmx != nil {
					size, err := pty.GetsizeFull(ptmx)
					if err == nil && size.Cols > 0 && size.Rows > 0 {
						pty.Setsize(ptmx, &pty.Winsize{
							Cols: size.Cols,
							Rows: size.Rows - 1,
						})
						time.Sleep(50 * time.Millisecond)
						pty.Setsize(ptmx, &pty.Winsize{
							Cols: size.Cols,
							Rows: size.Rows,
						})
					}
				}
			}

		case "disconnected":
			d.setMobileConnected(false)

		case "pong":
			// Keepalive response
		}
	}
}

func waitForProcess(cmd *exec.Cmd) <-chan error {
	ch := make(chan error, 1)
	go func() {
		ch <- cmd.Wait()
	}()
	return ch
}
