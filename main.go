package main

import (
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
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/google/uuid"
	"golang.org/x/term"
)

// cliFlags holds all parsed command-line flags
type cliFlags struct {
	agent         string
	workDir       string
	listAgents    bool
	listSessions  bool
	clearSession  bool
	clearSessions bool
	unpairMobile  string
	showStatus    bool
}

// parseFlags parses command-line arguments and returns the flags
func parseFlags() *cliFlags {
	agent := flag.String("agent", "", "Agent to run (claude, aider, gemini). Use '?' to force re-selection.")
	workDir := flag.String("workdir", "", "Working directory")
	showVersion := flag.Bool("version", false, "Show version and exit")
	listAgents := flag.Bool("agents", false, "List available AI agents and exit")
	listSessions := flag.Bool("sessions", false, "List saved sessions and exit")
	clearSession := flag.Bool("clear-session", false, "Clear saved session for current directory and exit")
	clearSessions := flag.Bool("clear-sessions", false, "Clear all saved sessions and exit")
	unpairMobile := flag.String("unpair", "", "Unpair a mobile device by ID")
	showStatus := flag.Bool("status", false, "Show PC status, paired mobiles, and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("aipilot-cli version %s\n", Version)
		os.Exit(0)
	}

	return &cliFlags{
		agent:         *agent,
		workDir:       *workDir,
		listAgents:    *listAgents,
		listSessions:  *listSessions,
		clearSession:  *clearSession,
		clearSessions: *clearSessions,
		unpairMobile:  *unpairMobile,
		showStatus:    *showStatus,
	}
}

// handleSpecialModes handles status, unpair, and pairing modes. Returns true if program should exit.
func handleSpecialModes(flags *cliFlags, pcConfig *PCConfig, relayClient *RelayClient) bool {
	// Status mode
	if flags.showStatus {
		showPCStatus(pcConfig)
		return true
	}

	// List sessions mode
	if flags.listSessions {
		listSavedSessions()
		return true
	}

	// Clear current session mode
	if flags.clearSession {
		workDir := flags.workDir
		if workDir == "" {
			workDir, _ = os.Getwd()
		}
		clearCurrentSession(workDir, relayClient)
		return true
	}

	// Clear all sessions mode
	if flags.clearSessions {
		clearSavedSessions(relayClient)
		return true
	}

	// Unpair mode
	if flags.unpairMobile != "" {
		if err := handleUnpair(pcConfig, relayClient, flags.unpairMobile); err != nil {
			log.Fatal("Failed to unpair:", err)
		}
		return true
	}

	return false
}

// ensurePairedMobile checks if we have paired mobiles, initiates pairing if not.
func ensurePairedMobile(pcConfig *PCConfig, relayClient *RelayClient) {
	if pcConfig.hasPairedMobiles() {
		return
	}

	fmt.Printf("%sNo mobile devices paired.%s\n\n", yellow, reset)
	if err := handlePairing(pcConfig, relayClient, RelayURL); err != nil {
		log.Fatal("Pairing failed:", err)
	}
	fmt.Printf("\n%s✓ Pairing complete!%s\n\n", green, reset)
}

// handleListAgents displays available agents and exits if --list was specified
func handleListAgents(listAgents bool) {
	if !listAgents {
		return
	}

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

// resolveWorkDir returns the working directory, using current dir if not specified
func resolveWorkDir(workDir string) string {
	if workDir != "" {
		return workDir
	}
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal("Failed to get working directory:", err)
	}
	return wd
}

// selectAgentCommand selects the agent command based on flags and saved preferences
func selectAgentCommand(flags *cliFlags, workDir string) string {
	// Agent selection logic:
	// 1. If --agent ?: force re-selection
	// 2. If --agent <name> specified: use that agent
	// 3. Otherwise: use saved agent for this directory, or detect/ask

	if flags.agent == "?" {
		// Force re-selection
		agents := detectAvailableAgents()
		if len(agents) == 0 {
			printNoAgentsError()
			os.Exit(1)
		}
		return selectAgent(agents)
	}

	if flags.agent != "" {
		// Explicit command specified
		if _, err := checkCommand(flags.agent); err != nil {
			log.Fatalf("Error: %v\nPlease ensure '%s' is installed and in your PATH.", err, flags.agent)
		}
		return flags.agent
	}

	// Try to use saved agent for this directory
	savedAgent := getDirectoryAgent(workDir)
	if savedAgent != "" {
		// Verify agent still exists
		if _, err := checkCommand(savedAgent); err == nil {
			fmt.Printf("%sUsing saved agent for this directory: %s%s\n", dim, savedAgent, reset)
			return savedAgent
		}
		fmt.Printf("%sSaved agent '%s' not found, detecting...%s\n", yellow, savedAgent, reset)
	}

	// Detect if no saved agent or saved agent not found
	agents := detectAvailableAgents()
	if len(agents) == 0 {
		printNoAgentsError()
		os.Exit(1)
	}
	return selectAgent(agents)
}

// printNoAgentsError prints the error message when no agents are found
func printNoAgentsError() {
	fmt.Printf("%sNo AI agents found in PATH.%s\n", red, reset)
	fmt.Println("Supported agents: claude, gemini")
}

// createSession creates a session on the relay server
func createSession(relayClient *RelayClient, agentType AgentType, workDir, displayName string, sshInfo *SSHInfo) (*CreateSessionResponse, error) {
	fmt.Printf("%sCreating session on relay...%s\n", dim, reset)
	sessionResp, err := relayClient.CreateSession(string(agentType), workDir, displayName, sshInfo)
	if err != nil {
		// Fallback to local session if relay is unavailable
		fmt.Printf("%sWarning: Could not create session on relay: %v%s\n", yellow, err, reset)
		fmt.Printf("%sFalling back to local session...%s\n", dim, reset)
		return &CreateSessionResponse{
			SessionID: uuid.New().String(),
			Token:     generateRandomToken(),
		}, nil
	}
	return sessionResp, nil
}

// createDaemon creates and initializes the daemon
func createDaemon(session, token, relay, command, workDir string, agentType AgentType, pcConfig *PCConfig, relayClient *RelayClient) *Daemon {
	daemon := &Daemon{
		session:     session,
		token:       token,
		relay:       relay,
		command:     command,
		workDir:     workDir,
		agentType:   agentType,
		stdinFd:     int(os.Stdin.Fd()),
		pcConfig:    pcConfig,
		relayClient: relayClient,
	}

	// Initialize E2E encryption
	if err := daemon.initEncryption(); err != nil {
		log.Fatal("Failed to initialize encryption:", err)
	}

	return daemon
}

// displayHeader displays the application header and session info
func displayHeader(daemon *Daemon, session, command, workDir, agentVersion string) {
	fmt.Println()
	fmt.Printf("%s%sAIPilot CLI%s %s[%s]%s\n", bold, cyan, reset, dim, Build, reset)
	fmt.Println()

	// Connect to relay early
	go daemon.connectToRelay()

	// Wait a bit to see if mobile is already connected
	fmt.Printf("%sWaiting for mobile connection...%s\n", dim, reset)
	time.Sleep(800 * time.Millisecond)

	// Check connection status
	if daemon.isMobileConnected() {
		fmt.Printf("%s✓ Mobile connected!%s\n\n", green, reset)
	} else {
		// Paired mobiles can see the session in the app
		fmt.Printf("%sSession available in the AIPilot app.%s\n\n", dim, reset)
	}

	// Display session info
	fmt.Printf("  Session:  %s\n", session[:8]+"...")
	fmt.Printf("  Command:  %s", command)
	if agentVersion != "" {
		fmt.Printf(" %s(%s)%s", dim, agentVersion, reset)
	}
	fmt.Println()
	fmt.Printf("  WorkDir:  %s\n", workDir)
	fmt.Printf("  Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Println()
	fmt.Printf("%sAIPilot: //qr%s\n", dim, reset)
	fmt.Println()
}

// startPTY starts the PTY and returns the pty master and command
func startPTY(command, workDir string) (*os.File, *exec.Cmd) {
	fmt.Printf("Starting %s...\n", command)
	cmd := exec.Command(command)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Fatal("Failed to start PTY:", err)
	}
	return ptmx, cmd
}

// setupTerminalSize sets the initial terminal size
func setupTerminalSize(daemon *Daemon) {
	if term.IsTerminal(daemon.stdinFd) {
		width, height, err := term.GetSize(daemon.stdinFd)
		if err == nil && width > 0 && height > 0 {
			daemon.resizePTY(uint16(height), uint16(width))
			daemon.mu.Lock()
			daemon.pcCols = width
			daemon.pcRows = height
			daemon.currentClient = "pc"
			daemon.mu.Unlock()
		}
	}
}

// startPTYReader starts a goroutine that reads from PTY and writes to stdout and mobile
func startPTYReader(daemon *Daemon) {
	go func() {
		buf := make([]byte, BufferSize)
		for {
			n, err := daemon.readFromPTY(buf)
			if err != nil {
				if err != io.EOF {
					// Silent
				}
				daemon.mu.Lock()
				daemon.running = false
				daemon.mu.Unlock()
				return
			}
			if n == 0 {
				// PTY not available
				daemon.mu.Lock()
				daemon.running = false
				daemon.mu.Unlock()
				return
			}

			os.Stdout.Write(buf[:n])
			daemon.sendToMobile(buf[:n])
		}
	}()
}

// setupRawTerminal sets up the terminal in raw mode and returns the old state
func setupRawTerminal(daemon *Daemon) *term.State {
	if !term.IsTerminal(daemon.stdinFd) {
		return nil
	}

	oldState, err := term.MakeRaw(daemon.stdinFd)
	if err != nil {
		fmt.Printf("%sWarning: Could not set raw mode: %v%s\n", yellow, err, reset)
		return nil
	}
	daemon.oldState = oldState
	return oldState
}

// startStdinReader starts a goroutine that reads from stdin and writes to PTY
func startStdinReader(daemon *Daemon, oldState *term.State) {
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

			// Track escape sequences
			if char == 0x1b { // ESC
				inEscapeSeq = true
				daemon.sendToPTY(b)
				continue
			} else if inEscapeSeq {
				if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || char == '~' {
					inEscapeSeq = false
				}
				daemon.sendToPTY(b)
				continue
			}

			// Printable characters
			if !inEscapeSeq && char >= 32 && char < 127 {
				lineBuf += string(char)
				daemon.sendToPTY(b)
				daemon.schedulePCSwitch()
				continue
			}

			// Enter key
			if char == '\r' || char == '\n' {
				lineBuf = handleEnterKey(daemon, lineBuf, b, oldState)
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

			// Ctrl+C or Ctrl+U - reset buffer
			if char == 3 || char == 0x15 {
				lineBuf = ""
				daemon.sendToPTY(b)
				continue
			}

			// Other characters
			daemon.sendToPTY(b)
		}
	}()
}

// handleEnterKey processes the enter key and returns the new line buffer
func handleEnterKey(daemon *Daemon, lineBuf string, b []byte, oldState *term.State) string {
	cmd := strings.TrimSpace(strings.ToLower(lineBuf))
	if aipilotCmd := daemon.getAIPilotCommand(cmd); aipilotCmd != "" {
		daemon.sendToPTY([]byte{0x15}) // Ctrl+U

		if oldState != nil {
			term.Restore(daemon.stdinFd, oldState)
		}

		daemon.executeAIPilotCommand(aipilotCmd)

		if oldState != nil {
			newState, _ := term.MakeRaw(daemon.stdinFd)
			daemon.oldState = newState
		}
	} else {
		daemon.sendToPTY(b)
	}
	return ""
}

// startResizeHandler starts a goroutine that handles terminal resize signals
func startResizeHandler(daemon *Daemon, resizeChan <-chan os.Signal) {
	go func() {
		for range resizeChan {
			if term.IsTerminal(daemon.stdinFd) {
				width, height, err := term.GetSize(daemon.stdinFd)
				if err == nil && width > 0 && height > 0 {
					daemon.mu.Lock()
					daemon.pcCols = width
					daemon.pcRows = height
					shouldResize := daemon.currentClient == "pc" || daemon.currentClient == ""
					daemon.mu.Unlock()

					if shouldResize {
						daemon.resizePTY(uint16(height), uint16(width))
						daemon.mu.Lock()
						daemon.currentClient = "pc"
						daemon.mu.Unlock()
					}
				}
			}
		}
	}()
}

// waitForTermination waits for either a signal or process exit, then cleans up
func waitForTermination(sigChan <-chan os.Signal, cmd *exec.Cmd, daemon *Daemon) {
	var exitMsg string

	select {
	case <-sigChan:
		exitMsg = "Shutting down AIPilot..."
	case err := <-waitForProcess(cmd):
		if err != nil {
			exitMsg = fmt.Sprintf("Process exited with error: %v", err)
		} else {
			exitMsg = "" // Silent exit
		}
	}

	// Restore terminal before printing (fixes raw mode line breaks)
	if daemon.oldState != nil {
		term.Restore(daemon.stdinFd, daemon.oldState)
	}

	if exitMsg != "" {
		fmt.Printf("\n%s\n", exitMsg)
	}

	// Cleanup: delete session from relay and local file
	daemon.cleanup()
}

func main() {
	// Parse flags
	flags := parseFlags()

	// Load or create PC configuration
	pcConfig, err := getOrCreatePCConfig()
	if err != nil {
		log.Fatal("Failed to load PC configuration:", err)
	}

	// Create relay client
	relayClient := NewRelayClient(RelayURL, pcConfig)

	// Handle special modes (status, unpair, pairing)
	if handleSpecialModes(flags, pcConfig, relayClient) {
		os.Exit(0)
	}

	// Ensure we have paired mobiles
	ensurePairedMobile(pcConfig, relayClient)

	// Handle --list flag
	handleListAgents(flags.listAgents)

	// Resolve working directory
	workDir := resolveWorkDir(flags.workDir)

	// Select agent command
	selectedCommand := selectAgentCommand(flags, workDir)

	// Save agent choice for this directory
	if err := setDirectoryAgent(workDir, selectedCommand); err != nil {
		fmt.Printf("%sWarning: Could not save agent preference: %v%s\n", yellow, err, reset)
	}

	// Detect agent type and version
	agentType := detectAgentType(selectedCommand)
	agentVersion := getAgentVersion(selectedCommand, agentType)
	displayName := filepath.Base(workDir)

	// Try to load existing session, or create new one
	// Detect SSH availability for session info
	sshInfo := DetectSSHInfo()

	var session, token string

	if savedSession, err := loadSession(workDir); err == nil {
		// Reuse existing session
		session = savedSession.Session
		token = savedSession.Token
		fmt.Printf("%s✓ Resuming session%s\n", green, reset)
	} else {
		// Create new session with SSH info
		sessionResp, _ := createSession(relayClient, agentType, workDir, displayName, sshInfo)
		session = sessionResp.SessionID
		token = sessionResp.Token

		// Save for next time
		saveSession(workDir, &SessionData{
			Session:   session,
			Token:     token,
			Relay:     RelayURL,
			Command:   selectedCommand,
			WorkDir:   workDir,
			CreatedAt: time.Now().Format(time.RFC3339),
		})
	}

	// Create and initialize daemon
	daemon := createDaemon(session, token, RelayURL, selectedCommand, workDir, agentType, pcConfig, relayClient)

	// Display header and session info
	displayHeader(daemon, session, selectedCommand, workDir, agentVersion)

	// Start PTY
	ptmx, cmd := startPTY(selectedCommand, workDir)
	defer ptmx.Close()

	daemon.mu.Lock()
	daemon.ptmx = ptmx
	daemon.cmd = cmd
	daemon.running = true
	daemon.mu.Unlock()

	// Setup terminal
	setupTerminalSize(daemon)

	// Handle termination signals (SIGINT, SIGTERM, SIGHUP)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	// Handle window resize
	resizeChan := setupResizeSignal()

	// Start PTY reader goroutine
	startPTYReader(daemon)

	// Setup raw terminal
	oldState := setupRawTerminal(daemon)
	if oldState != nil {
		defer term.Restore(daemon.stdinFd, oldState)
	}

	// Start stdin reader goroutine
	startStdinReader(daemon, oldState)

	// Start resize handler goroutine
	startResizeHandler(daemon, resizeChan)

	// Wait for termination
	waitForTermination(sigChan, cmd, daemon)
}

func waitForProcess(cmd *exec.Cmd) <-chan error {
	ch := make(chan error, 1)
	go func() {
		ch <- cmd.Wait()
	}()
	return ch
}
