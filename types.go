package main

import (
	"context"
	"crypto/cipher"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/gorilla/websocket"
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

// Version and RelayURL are set at build time via -ldflags
var (
	Version  = "dev"
	Build    = "20260120-1"
	RelayURL = "wss://aipilot-relay.francois-achache.workers.dev"
)

const defaultCommand = "claude"

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
	mu    sync.RWMutex
	wsMu  sync.Mutex // Mutex for WebSocket writes
	ptyMu sync.Mutex // Mutex for PTY I/O operations

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

	// PC configuration (for pairing status)
	pcConfig    *PCConfig
	relayClient *RelayClient

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

	// Debounce timer for PC switch
	pcSwitchTimer *time.Timer

	// Chunked file uploads in progress
	chunkedUploads map[string]*ChunkedUpload
	uploadMu       sync.Mutex

	// Context for cancelling ping goroutine
	pingCtx    context.Context
	pingCancel context.CancelFunc
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

// Daemon helper methods for state access
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
