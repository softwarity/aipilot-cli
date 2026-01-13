package main

import (
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
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/skip2/go-qrcode"
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
)

// Status manages the CLI status display
type Status struct {
	mu                 sync.Mutex
	relayStatus        string
	mobileStatus       string
	agentStatus        string
	mobileConnectCount int
	statusLines        int  // number of status lines printed
	frozen             bool // stop updating display once terminal is active
}

func (s *Status) update(agent, relay, mobile string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if agent != "" {
		s.agentStatus = agent
	}
	if relay != "" {
		s.relayStatus = relay
	}
	if mobile != "" {
		s.mobileStatus = mobile
	}

	s.render()
}

func (s *Status) incrementConnect() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mobileConnectCount++
}

func (s *Status) freeze() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frozen = true
}

func (s *Status) render() {
	// Skip rendering if frozen (terminal is active)
	if s.frozen {
		return
	}

	// Move up to overwrite previous status lines
	if s.statusLines > 0 {
		for i := 0; i < s.statusLines; i++ {
			fmt.Print(moveUp)
		}
		fmt.Print(moveToCol0)
		for i := 0; i < s.statusLines; i++ {
			fmt.Print(clearLine + "\n")
		}
		for i := 0; i < s.statusLines; i++ {
			fmt.Print(moveUp)
		}
		fmt.Print(moveToCol0)
	}

	// Print status lines (order: Agent, Relay, Mobile)
	lines := []string{}

	// Agent status
	agentLine := fmt.Sprintf("  Agent:   %s", s.agentStatus)
	lines = append(lines, agentLine)

	// Relay status
	relayLine := fmt.Sprintf("  Relay:   %s", s.relayStatus)
	lines = append(lines, relayLine)

	// Mobile status with connection count
	mobileLine := fmt.Sprintf("  Mobile:  %s", s.mobileStatus)
	if s.mobileConnectCount > 1 {
		mobileLine += fmt.Sprintf(" %s(#%d)%s", dim, s.mobileConnectCount, reset)
	}
	lines = append(lines, mobileLine)

	for _, line := range lines {
		fmt.Println(line)
	}

	s.statusLines = len(lines)
}

func (s *Status) mobileConnected() {
	s.mu.Lock()
	s.mobileConnectCount++
	count := s.mobileConnectCount
	s.mu.Unlock()

	if count > 1 {
		// Reconnection - update status and refreeze (no extra line printed)
		s.update("", "", green+"✓ Connected"+reset)
		s.freeze()
	} else {
		// First connection, update status normally (will freeze after agent starts)
		s.update("", "", green+"✓ Connected"+reset)
	}
}

func (s *Status) mobileDisconnected() {
	s.mu.Lock()
	s.frozen = false // Unfreeze - mobile is gone, terminal output doesn't matter
	s.mu.Unlock()

	// Show full status since we're unfrozen
	s.update("", "", yellow+"⋯ Disconnected"+reset)
}

// openFile opens a file or URL with the system's default application
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

// Version and RelayURL are set at build time via -ldflags
var (
	Version  = "dev"
	RelayURL = "wss://aipilot-relay.francois-achache.workers.dev" // Base URL without /ws path
)

const (
	defaultCommand = "claude"
)

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
	// Create a hash of the workdir for unique filename
	h := sha256.Sum256([]byte(workDir))
	hash := hex.EncodeToString(h[:8]) // Use first 8 bytes
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

	// Create directory if needed
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

func clearSession(workDir string) error {
	path := getSessionFilePath(workDir)
	if path == "" {
		return nil
	}
	return os.Remove(path)
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
	relay := flag.String("relay", RelayURL, "WebSocket relay base URL")
	command := flag.String("command", defaultCommand, "Command to run (e.g., claude, bash)")
	workDir := flag.String("workdir", "", "Working directory")
	showVersion := flag.Bool("version", false, "Show version and exit")
	qrImage := flag.Bool("qr", false, "Open QR code as image (better for scanning)")
	newSession := flag.Bool("new", false, "Force create a new session (ignore saved session)")
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
	_, err := checkCommand(*command)
	if err != nil {
		log.Fatalf("Error: %v\nPlease ensure '%s' is installed and in your PATH.", err, *command)
	}

	// Detect agent type and version
	agentType := detectAgentType(*command)
	agentVersion := getAgentVersion(*command, agentType)

	// Try to load existing session or create new one
	var session, token string
	var sessionResumed bool

	if !*newSession {
		if savedSession, err := loadSession(*workDir); err == nil {
			// Use saved session if relay and command match
			if savedSession.Relay == *relay && savedSession.Command == *command {
				session = savedSession.Session
				token = savedSession.Token
				sessionResumed = true
				fmt.Printf("%s[Resuming session %s...]%s\n", dim, session[:8], reset)
			}
		}
	}

	if session == "" {
		// Create new session
		session = uuid.New().String()
		token = generateRandomToken()

		// Save session for future use
		sessionData := &SessionData{
			Session:   session,
			Token:     token,
			Relay:     *relay,
			Command:   *command,
			WorkDir:   *workDir,
			CreatedAt: time.Now().Format(time.RFC3339),
		}
		if err := saveSession(*workDir, sessionData); err != nil {
			fmt.Printf("%s[Warning: Could not save session: %v]%s\n", dim, err, reset)
		}
	}

	// Create QR data (always show for new connection or re-scan)
	qrData := QRData{
		Relay:     *relay,
		Session:   session,
		Token:     token,
		Command:   *command,
		AgentType: agentType,
	}

	qrJSON, _ := json.Marshal(qrData)

	// Display header
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════╗")
	if sessionResumed {
		fmt.Println("║   AIPilot - Session Resumed           ║")
	} else {
		fmt.Println("║   AIPilot - New Session               ║")
	}
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
		fmt.Printf("%sTip: Use -qr flag for easier scanning%s\n", dim, reset)
		fmt.Println()
	}

	// Display session info
	fmt.Printf("  Session:  %s\n", session[:8]+"...")
	fmt.Printf("  Command:  %s", *command)
	if agentVersion != "" {
		fmt.Printf(" %s(%s)%s", dim, agentVersion, reset)
	}
	fmt.Println()
	fmt.Printf("  WorkDir:  %s\n", *workDir)
	fmt.Printf("  Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Println()

	// Initialize status display (order: Agent, Relay, Mobile)
	status := &Status{
		agentStatus:  dim + "⋯ Waiting" + reset,
		relayStatus:  dim + "⋯ Waiting" + reset,
		mobileStatus: dim + "⋯ Waiting" + reset,
	}
	status.render()

	// Connect to relay using new endpoint format: /ws/{sessionId}?role=bridge
	status.update("", yellow+"⋯ Connecting..."+reset, "")
	wsURL := fmt.Sprintf("%s/ws/%s?role=bridge", *relay, session)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		status.update("", red+"✗ Failed"+reset, "")
		log.Fatal("Failed to connect to relay:", err)
	}
	defer conn.Close()

	// Wait for registration confirmation (sent automatically by relay)
	var response Message
	if err := conn.ReadJSON(&response); err != nil {
		status.update("", red+"✗ Failed"+reset, "")
		log.Fatal("Failed to read registration response:", err)
	}

	if response.Type == "error" {
		status.update("", red+"✗ Failed"+reset, "")
		log.Fatal("Registration failed:", response.Error)
	}

	if response.Type != "registered" {
		status.update("", red+"✗ Failed"+reset, "")
		log.Fatal("Unexpected response:", response.Type)
	}

	status.update("", green+"✓ Registered"+reset, "")

	// Start ping keepalive goroutine (10s to avoid Cloudflare timeout)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			<-ticker.C
			pingMsg := Message{Type: "ping"}
			if err := conn.WriteJSON(pingMsg); err != nil {
				return
			}
		}
	}()

	// Wait for mobile to connect BEFORE starting agent
	status.update("", "", yellow+"⋯ Waiting..."+reset)
	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			log.Fatal("Connection lost while waiting for mobile:", err)
		}
		if msg.Type == "connected" && msg.Role == "mobile" {
			status.mobileConnected()
			break
		}
		if msg.Type == "pong" {
			continue // ignore keepalive
		}
	}

	// NOW start PTY with command (after mobile is connected)
	status.update(yellow+"⋯ Starting..."+reset, "", "")
	cmd := exec.Command(*command)
	cmd.Dir = *workDir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		status.update(red+"✗ Failed"+reset, "", "")
		log.Fatal("Failed to start PTY:", err)
	}
	defer ptmx.Close()

	status.update(green+"✓ Running"+reset, "", "")

	// NOW freeze - all status updates are done, terminal output goes via WebSocket
	status.freeze()

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
					// Silent error - don't pollute terminal
				}
				return
			}

			msg := Message{
				Type:    "data",
				Payload: base64.StdEncoding.EncodeToString(buf[:n]),
			}
			if err := conn.WriteJSON(msg); err != nil {
				return
			}
		}
	}()

	// WebSocket -> PTY (mobile input to terminal)
	go func() {
		for {
			var msg Message
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}

			switch msg.Type {
			case "data":
				data, err := base64.StdEncoding.DecodeString(msg.Payload)
				if err != nil {
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
				status.mobileDisconnected()

			case "connected":
				if msg.Role == "mobile" {
					status.mobileConnected()
					// Trigger screen refresh by sending current size
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

			case "pong":
				// Keepalive response, ignore
			}
		}
	}()

	// Wait for termination
	select {
	case <-sigChan:
		fmt.Println("\n\nShutting down...")
	case err := <-waitForProcess(cmd):
		if err != nil {
			fmt.Println("\n\nProcess exited with error:", err)
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
