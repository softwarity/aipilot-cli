package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
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

	// Try to load existing session first
	var session, token, selectedCommand string
	var sessionResumed bool

	if !*newSession {
		if savedSession, err := loadSession(*workDir); err == nil {
			if savedSession.Relay == *relay {
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
			agents := detectAvailableAgents()
			if len(agents) == 0 {
				fmt.Printf("%sNo AI agents found in PATH.%s\n", red, reset)
				fmt.Println("Supported agents: claude, aider, codex, copilot, cursor")
				fmt.Println("Install one of these agents or specify with -command flag.")
				os.Exit(1)
			}
			selectedCommand = selectAgent(agents)
		} else {
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

	// Connect to relay early
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
