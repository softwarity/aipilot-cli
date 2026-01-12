package main

import (
	"crypto/hmac"
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
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/creack/pty"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/skip2/go-qrcode"
)

// openURL opens a file or URL with the system's default application
func openFile(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default: // linux, freebsd, etc.
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// Version is set at build time via -ldflags
var Version = "dev"

const (
	defaultRelay   = "wss://aipilot-relay.francois-achache.workers.dev/ws"
	defaultCommand = "claude"
	secretKey      = "aipilot-secret-key-change-in-production"
)

// AgentType represents known AI agent types
type AgentType string

const (
	AgentClaude  AgentType = "claude"
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

func generateToken(session string) string {
	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(session))
	h.Write([]byte(fmt.Sprintf("%d", time.Now().Unix()/300))) // 5 min window
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// checkCommand verifies the command exists and returns its path
func checkCommand(command string) (string, error) {
	path, err := exec.LookPath(command)
	if err != nil {
		return "", fmt.Errorf("command '%s' not found in PATH", command)
	}
	return path, nil
}

// detectAgentType determines the agent type from command name
func detectAgentType(command string) AgentType {
	cmd := strings.ToLower(command)
	switch {
	case strings.Contains(cmd, "claude"):
		return AgentClaude
	case strings.Contains(cmd, "aider"):
		return AgentAider
	default:
		return AgentGeneric
	}
}

// getAgentVersion tries to get the version of the agent
func getAgentVersion(command string, agentType AgentType) string {
	var versionFlag string
	switch agentType {
	case AgentClaude:
		versionFlag = "--version"
	case AgentAider:
		versionFlag = "--version"
	default:
		versionFlag = "--version"
	}

	cmd := exec.Command(command, versionFlag)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	// Extract first line and clean it
	version := strings.TrimSpace(strings.Split(string(output), "\n")[0])
	return version
}

func main() {
	// Parse flags
	relay := flag.String("relay", defaultRelay, "WebSocket relay URL")
	command := flag.String("command", defaultCommand, "Command to run (e.g., claude, bash)")
	workDir := flag.String("workdir", "", "Working directory")
	showVersion := flag.Bool("version", false, "Show version and exit")
	qrImage := flag.Bool("qr", false, "Open QR code as image (better for scanning)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("aipilot-cli version %s\n", Version)
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

	// Verify command exists
	cmdPath, err := checkCommand(*command)
	if err != nil {
		log.Fatalf("Error: %v\nPlease ensure '%s' is installed and in your PATH.", err, *command)
	}
	fmt.Printf("✓ Found %s at %s\n", *command, cmdPath)

	// Detect agent type and version
	agentType := detectAgentType(*command)
	agentVersion := getAgentVersion(*command, agentType)
	if agentVersion != "" {
		fmt.Printf("✓ Agent version: %s\n", agentVersion)
	}

	// Generate session
	session := uuid.New().String()
	token := generateToken(session)

	// Create QR data (minimal for smaller QR code)
	qrData := QRData{
		Relay:     *relay,
		Session:   session,
		Token:     token,
		Command:   *command,
		AgentType: agentType,
		// WorkingDir, AgentVersion, OS, CLIVersion shown in terminal but not in QR
	}

	qrJSON, _ := json.Marshal(qrData)

	// Display QR code
	fmt.Println("\n╔═══════════════════════════════════════╗")
	fmt.Println("║   AIPilot - Scan to Connect           ║")
	fmt.Println("╚═══════════════════════════════════════╝")
	fmt.Println()

	if *qrImage {
		// Generate QR code as image file
		tmpDir := os.TempDir()
		qrFile := filepath.Join(tmpDir, fmt.Sprintf("aipilot-qr-%s.png", session[:8]))
		if err := qrcode.WriteFile(string(qrJSON), qrcode.Medium, 300, qrFile); err != nil {
			log.Fatal("Failed to generate QR code image:", err)
		}
		fmt.Printf("QR code saved to: %s\n", qrFile)
		if err := openFile(qrFile); err != nil {
			fmt.Println("Could not open QR image automatically.")
			fmt.Println("Please open the file manually:", qrFile)
		} else {
			fmt.Println("QR code image opened in default viewer.")
		}
		fmt.Println()
	} else {
		qr, err := qrcode.New(string(qrJSON), qrcode.Medium)
		if err != nil {
			log.Fatal("Failed to generate QR code:", err)
		}
		fmt.Println(qr.ToSmallString(false))
		fmt.Println("Tip: Use -qr flag to open QR code as image for easier scanning")
		fmt.Println()
	}

	fmt.Printf("Session:  %s\n", session[:8]+"...")
	fmt.Printf("Agent:    %s (%s)\n", *command, agentType)
	fmt.Printf("WorkDir:  %s\n", *workDir)
	fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Println("\nWaiting for mobile connection...")

	// Connect to relay
	conn, _, err := websocket.DefaultDialer.Dial(*relay, nil)
	if err != nil {
		log.Fatal("Failed to connect to relay:", err)
	}
	defer conn.Close()

	// Register session
	registerMsg := Message{
		Type:    "register",
		Session: session,
	}
	if err := conn.WriteJSON(registerMsg); err != nil {
		log.Fatal("Failed to register session:", err)
	}

	// Wait for registration confirmation
	var response Message
	if err := conn.ReadJSON(&response); err != nil {
		log.Fatal("Failed to read registration response:", err)
	}

	if response.Type == "error" {
		log.Fatal("Registration failed:", response.Error)
	}

	fmt.Println("✓ Registered with relay")

	// Start ping keepalive goroutine
	stopPing := make(chan struct{})
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				pingMsg := Message{Type: "ping"}
				if err := conn.WriteJSON(pingMsg); err != nil {
					return
				}
			case <-stopPing:
				return
			}
		}
	}()

	// Wait for mobile connection
	for {
		if err := conn.ReadJSON(&response); err != nil {
			close(stopPing)
			log.Fatal("Connection error:", err)
		}

		if response.Type == "connected" && response.Role == "mobile" {
			fmt.Println("✓ Mobile connected!")
			break
		}

		if response.Type == "pong" {
			// Keepalive response, ignore
			continue
		}

		if response.Type == "error" {
			close(stopPing)
			log.Fatal("Error:", response.Error)
		}
	}
	close(stopPing)

	// Start PTY with command
	fmt.Printf("\nStarting %s...\n\n", *command)

	cmd := exec.Command(*command)
	cmd.Dir = *workDir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Fatal("Failed to start PTY:", err)
	}
	defer ptmx.Close()

	// Handle window resize (Unix only, no-op on Windows)
	_ = setupResizeSignal()

	// Handle termination
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	// PTY -> WebSocket (terminal output to mobile)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Println("PTY read error:", err)
				}
				return
			}

			msg := Message{
				Type:    "data",
				Payload: base64.StdEncoding.EncodeToString(buf[:n]),
			}
			if err := conn.WriteJSON(msg); err != nil {
				log.Println("WebSocket write error:", err)
				return
			}
		}
	}()

	// WebSocket -> PTY (mobile input to terminal)
	go func() {
		for {
			var msg Message
			if err := conn.ReadJSON(&msg); err != nil {
				log.Println("WebSocket read error:", err)
				return
			}

			switch msg.Type {
			case "data":
				data, err := base64.StdEncoding.DecodeString(msg.Payload)
				if err != nil {
					log.Println("Base64 decode error:", err)
					continue
				}
				ptmx.Write(data)

			case "resize":
				if msg.Cols > 0 && msg.Rows > 0 {
					pty.Setsize(ptmx, &pty.Winsize{
						Cols: uint16(msg.Cols),
						Rows: uint16(msg.Rows),
					})
				}

			case "disconnected":
				fmt.Println("\n\nMobile disconnected.")
				// Don't return - wait for reconnection

			case "connected":
				if msg.Role == "mobile" {
					fmt.Println("\n✓ Mobile reconnected!")
					// Trigger screen refresh by sending current size
					// This makes Claude redraw its UI
					size, err := pty.GetsizeFull(ptmx)
					if err == nil && size.Cols > 0 && size.Rows > 0 {
						// Send a slightly different size then back to trigger redraw
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
		}
	}()

	// Wait for termination
	select {
	case <-sigChan:
		fmt.Println("\n\nShutting down...")
	case err := <-waitForProcess(cmd):
		if err != nil {
			log.Println("Process exited with error:", err)
		} else {
			fmt.Println("\n\nProcess exited.")
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
