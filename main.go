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
	"time"

	"github.com/creack/pty"
	"github.com/google/uuid"
	"golang.org/x/term"
)

func main() {
	// Parse flags
	relay := flag.String("relay", RelayURL, "WebSocket relay base URL")
	command := flag.String("command", "", "Command to run (e.g., claude, aider). Auto-detects if not specified.")
	agentFlag := flag.String("agent", "", "Alias for -command")
	workDir := flag.String("workdir", "", "Working directory")
	showVersion := flag.Bool("version", false, "Show version and exit")
	qrImage := flag.Bool("qr", false, "Open QR code as image (better for scanning)")
	newSession := flag.Bool("new", false, "Force create a new session (ignore saved agent for this directory)")
	listAgents := flag.Bool("list", false, "List available AI agents and exit")
	doPairing := flag.Bool("pair", false, "Initiate pairing with a new mobile device")
	unpairMobile := flag.String("unpair", "", "Unpair a mobile device by ID")
	showStatus := flag.Bool("status", false, "Show PC status, paired mobiles, and exit")
	flag.Parse()

	// Handle -agent as alias for -command
	if *agentFlag != "" && *command == "" {
		*command = *agentFlag
	}

	if *showVersion {
		fmt.Printf("aipilot-cli version %s\n", Version)
		os.Exit(0)
	}

	// Load or create PC configuration
	pcConfig, err := getOrCreatePCConfig()
	if err != nil {
		log.Fatal("Failed to load PC configuration:", err)
	}

	// Create relay client
	relayClient := NewRelayClient(*relay, pcConfig)

	// Status mode
	if *showStatus {
		showPCStatus(pcConfig)
		os.Exit(0)
	}

	// Unpair mode
	if *unpairMobile != "" {
		if err := handleUnpair(pcConfig, relayClient, *unpairMobile); err != nil {
			log.Fatal("Failed to unpair:", err)
		}
		os.Exit(0)
	}

	// Pairing mode (explicit --pair flag)
	if *doPairing {
		if err := handlePairing(pcConfig, relayClient, *relay, *qrImage); err != nil {
			log.Fatal("Pairing failed:", err)
		}
		os.Exit(0)
	}

	// Check if we have paired mobiles, if not, show pairing QR and ask what to do
	if !pcConfig.hasPairedMobiles() {
		fmt.Printf("%sNo mobile devices paired.%s\n\n", yellow, reset)
		if err := handlePairing(pcConfig, relayClient, *relay, *qrImage); err != nil {
			log.Fatal("Pairing failed:", err)
		}
		fmt.Println()

		// Ask user what to do next
		fmt.Printf("%s✓ Pairing complete!%s\n\n", green, reset)
		fmt.Printf("  %s[1]%s Launch agent now\n", cyan, reset)
		fmt.Printf("  %s[2]%s Exit (run aipilot-cli later)\n", cyan, reset)
		fmt.Print("\nChoice [1]: ")

		var input string
		fmt.Scanln(&input)
		input = strings.TrimSpace(input)

		if input == "2" {
			fmt.Printf("\n%sRun aipilot-cli again to start a session.%s\n", dim, reset)
			fmt.Printf("%sUse --pair to add another mobile device.%s\n\n", dim, reset)
			os.Exit(0)
		}
		// Default (1 or empty): continue to launch agent
		fmt.Println()
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

	// Agent selection logic:
	// 1. If --agent or --command specified: use that
	// 2. If --new: ignore saved config, detect/ask
	// 3. Otherwise: use saved agent for this directory, or detect/ask
	var selectedCommand string

	if *command != "" {
		// Explicit command specified
		selectedCommand = *command
		if _, err := checkCommand(selectedCommand); err != nil {
			log.Fatalf("Error: %v\nPlease ensure '%s' is installed and in your PATH.", err, selectedCommand)
		}
	} else if *newSession {
		// Force new selection
		agents := detectAvailableAgents()
		if len(agents) == 0 {
			fmt.Printf("%sNo AI agents found in PATH.%s\n", red, reset)
			fmt.Println("Supported agents: claude, aider, codex, copilot, cursor")
			fmt.Println("Install one of these agents or specify with --agent flag.")
			os.Exit(1)
		}
		selectedCommand = selectAgent(agents)
	} else {
		// Try to use saved agent for this directory
		savedAgent := getDirectoryAgent(*workDir)
		if savedAgent != "" {
			// Verify agent still exists
			if _, err := checkCommand(savedAgent); err == nil {
				selectedCommand = savedAgent
				fmt.Printf("%sUsing saved agent for this directory: %s%s\n", dim, savedAgent, reset)
			} else {
				fmt.Printf("%sSaved agent '%s' not found, detecting...%s\n", yellow, savedAgent, reset)
			}
		}

		// Detect if no saved agent or saved agent not found
		if selectedCommand == "" {
			agents := detectAvailableAgents()
			if len(agents) == 0 {
				fmt.Printf("%sNo AI agents found in PATH.%s\n", red, reset)
				fmt.Println("Supported agents: claude, aider, codex, copilot, cursor")
				fmt.Println("Install one of these agents or specify with --agent flag.")
				os.Exit(1)
			}
			selectedCommand = selectAgent(agents)
		}
	}

	// Save agent choice for this directory
	if err := setDirectoryAgent(*workDir, selectedCommand); err != nil {
		fmt.Printf("%sWarning: Could not save agent preference: %v%s\n", yellow, err, reset)
	}

	// Detect agent type and version
	agentType := detectAgentType(selectedCommand)
	agentVersion := getAgentVersion(selectedCommand, agentType)

	// Create display name for the session (directory basename)
	displayName := filepath.Base(*workDir)

	// Don't purge sessions automatically - user can use //purge manually if needed
	// Multiple sessions from the same PC should coexist

	// Create session on relay
	fmt.Printf("%sCreating session on relay...%s\n", dim, reset)
	sessionResp, err := relayClient.CreateSession(string(agentType), *workDir, displayName)
	if err != nil {
		// Fallback to local session if relay is unavailable
		fmt.Printf("%sWarning: Could not create session on relay: %v%s\n", yellow, err, reset)
		fmt.Printf("%sFalling back to local session...%s\n", dim, reset)
		sessionResp = &CreateSessionResponse{
			SessionID: uuid.New().String(),
			Token:     generateRandomToken(),
		}
	}

	session := sessionResp.SessionID
	token := sessionResp.Token

	// Cleanup session on exit
	defer func() {
		fmt.Printf("%sCleaning up session...%s\n", dim, reset)
		if err := relayClient.DeleteSession(session); err != nil {
			// Silently ignore errors on cleanup
			fmt.Printf("%sWarning: Could not cleanup session: %v%s\n", yellow, err, reset)
		}
	}()

	// Create daemon
	daemon := &Daemon{
		session:     session,
		token:       token,
		relay:       *relay,
		command:     selectedCommand,
		workDir:     *workDir,
		agentType:   agentType,
		stdinFd:     int(os.Stdin.Fd()),
		pcConfig:    pcConfig,
		relayClient: relayClient,
	}

	// Initialize E2E encryption
	if err := daemon.initEncryption(); err != nil {
		log.Fatal("Failed to initialize encryption:", err)
	}

	// Display header
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
	fmt.Printf("  Command:  %s", selectedCommand)
	if agentVersion != "" {
		fmt.Printf(" %s(%s)%s", dim, agentVersion, reset)
	}
	fmt.Println()
	fmt.Printf("  WorkDir:  %s\n", *workDir)
	fmt.Printf("  Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Println()
	fmt.Printf("%sAIPilot: //qr //status //purge //disconnect //quit (Ctrl+A for menu)%s\n", dim, reset)
	fmt.Println()

	// Start PTY
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

	// Set initial terminal size
	if term.IsTerminal(daemon.stdinFd) {
		width, height, err := term.GetSize(daemon.stdinFd)
		if err == nil && width > 0 && height > 0 {
			pty.Setsize(ptmx, &pty.Winsize{
				Cols: uint16(width),
				Rows: uint16(height),
			})
			daemon.pcCols = width
			daemon.pcRows = height
			daemon.currentClient = "pc"
		}
	}

	// Handle termination
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	// Handle window resize
	resizeChan := setupResizeSignal()

	// PTY -> stdout AND mobile
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

			os.Stdout.Write(buf[:n])
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

	// stdin -> PTY with command detection
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
				cmd := strings.TrimSpace(strings.ToLower(lineBuf))
				if aipilotCmd := daemon.getAIPilotCommand(cmd); aipilotCmd != "" {
					daemon.sendToPTY([]byte{0x15}) // Ctrl+U

					if oldState != nil {
						term.Restore(daemon.stdinFd, oldState)
					}

					daemon.executeAIPilotCommand(aipilotCmd)

					if oldState != nil {
						oldState, _ = term.MakeRaw(daemon.stdinFd)
						daemon.oldState = oldState
					}
				} else {
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

			// Ctrl+U - reset buffer
			if char == 0x15 {
				lineBuf = ""
				daemon.sendToPTY(b)
				continue
			}

			// Other characters
			daemon.sendToPTY(b)
		}
	}()

	// Handle resize signals
	go func() {
		for range resizeChan {
			if term.IsTerminal(daemon.stdinFd) {
				width, height, err := term.GetSize(daemon.stdinFd)
				if err == nil && width > 0 && height > 0 {
					daemon.mu.Lock()
					daemon.pcCols = width
					daemon.pcRows = height
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

func waitForProcess(cmd *exec.Cmd) <-chan error {
	ch := make(chan error, 1)
	go func() {
		ch <- cmd.Wait()
	}()
	return ch
}
