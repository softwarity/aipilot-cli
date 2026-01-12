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
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/skip2/go-qrcode"
)

const (
	defaultRelay   = "wss://aipilot-relay.francois-achache.workers.dev/ws"
	defaultCommand = "claude"
	secretKey      = "aipilot-secret-key-change-in-production"
)

// QRData is encoded in the QR code for mobile to scan
type QRData struct {
	Relay      string `json:"r"`
	Session    string `json:"s"`
	Token      string `json:"t"`
	Command    string `json:"c,omitempty"`
	WorkingDir string `json:"w,omitempty"`
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

func main() {
	// Parse flags
	relay := flag.String("relay", defaultRelay, "WebSocket relay URL")
	command := flag.String("command", defaultCommand, "Command to run (e.g., claude, bash)")
	workDir := flag.String("workdir", "", "Working directory")
	flag.Parse()

	// Use current directory if not specified
	if *workDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			log.Fatal("Failed to get working directory:", err)
		}
		*workDir = wd
	}

	// Generate session
	session := uuid.New().String()
	token := generateToken(session)

	// Create QR data
	qrData := QRData{
		Relay:      *relay,
		Session:    session,
		Token:      token,
		Command:    *command,
		WorkingDir: *workDir,
	}

	qrJSON, _ := json.Marshal(qrData)

	// Display QR code
	fmt.Println("\n╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║              AIPilot Bridge - Scan to Connect              ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	qr, err := qrcode.New(string(qrJSON), qrcode.Medium)
	if err != nil {
		log.Fatal("Failed to generate QR code:", err)
	}
	fmt.Println(qr.ToSmallString(false))

	fmt.Printf("Session: %s\n", session[:8]+"...")
	fmt.Printf("Command: %s\n", *command)
	fmt.Printf("WorkDir: %s\n", *workDir)
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

	// Wait for mobile connection
	for {
		if err := conn.ReadJSON(&response); err != nil {
			log.Fatal("Connection error:", err)
		}

		if response.Type == "connected" && response.Role == "mobile" {
			fmt.Println("✓ Mobile connected!")
			break
		}

		if response.Type == "error" {
			log.Fatal("Error:", response.Error)
		}
	}

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

	// Handle window resize
	resizeChan := make(chan os.Signal, 1)
	signal.Notify(resizeChan, syscall.SIGWINCH)

	// Handle termination
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

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
				return
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
