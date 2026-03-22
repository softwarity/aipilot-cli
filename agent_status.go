package main

import (
	"bytes"
	"time"
)

// Agent busy/idle detection via PTY output scanning.
// Detects "esc to " pattern (case-insensitive) in terminal output
// to determine if the agent is busy (thinking/processing).

const (
	// AgentIdleDebounce is the delay before declaring idle after pattern disappears
	AgentIdleDebounce = 500 * time.Millisecond
	// agentStatusBufSize is the rolling buffer size for pattern scanning
	agentStatusBufSize = 256
)

// ANSI escape sequence parser states
const (
	escNone   = 0 // Normal text
	escStart  = 1 // Just saw ESC (0x1b)
	escCSI    = 2 // In CSI sequence: ESC [ ... letter
	escOSC    = 3 // In OSC sequence: ESC ] ... (BEL or ST)
	escOSCESC = 4 // In OSC, saw ESC (awaiting \ for ST terminator)
)

// busyPattern is the lowercase pattern to detect in PTY output
var busyPattern = []byte("esc to ")

// initAgentStatus initializes the agent status detection fields
func (d *Daemon) initAgentStatus() {
	d.agentStatusBuf = make([]byte, 0, agentStatusBufSize)
}

// scanAgentStatus scans PTY output for the busy pattern.
// Called from startPTYReader on every read — must be fast.
func (d *Daemon) scanAgentStatus(data []byte) {
	// Skip PTY scanning when receiving status via hook socket
	if d.agentStatusViaSocket {
		return
	}

	state := d.agentEscState
	buf := d.agentStatusBuf

	for _, b := range data {
		switch state {
		case escNone:
			if b == 0x1b {
				state = escStart
				continue
			}
			// Only keep printable ASCII, lowercase on the fly
			if b >= 0x20 && b <= 0x7e {
				if b >= 'A' && b <= 'Z' {
					b += 0x20
				}
				buf = append(buf, b)
			}

		case escStart:
			// Byte right after ESC determines the sequence type
			if b == '[' {
				state = escCSI
			} else if b == ']' {
				state = escOSC
			} else {
				// Two-byte escape (e.g. ESC c, ESC M) — done
				state = escNone
			}

		case escCSI:
			// CSI: ESC [ (params) letter — letter terminates
			if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') {
				state = escNone
			}
			// else: parameters (digits, ;, ?, etc.) — keep consuming

		case escOSC:
			// OSC: ESC ] ... terminated by BEL (0x07) or ST (ESC \)
			if b == 0x07 {
				state = escNone
			} else if b == 0x1b {
				state = escOSCESC
			}
			// else: OSC content — keep consuming

		case escOSCESC:
			// Inside OSC, saw ESC — if next is \, it's ST (end of OSC)
			if b == '\\' {
				state = escNone
			} else {
				// Not ST — treat as new escape sequence
				if b == '[' {
					state = escCSI
				} else if b == ']' {
					state = escOSC
				} else {
					state = escNone
				}
			}
		}
	}
	d.agentEscState = state

	// Trim buffer to max size (keep tail)
	if len(buf) > agentStatusBufSize {
		buf = buf[len(buf)-agentStatusBufSize:]
	}
	d.agentStatusBuf = buf

	// Check for pattern
	found := bytes.Contains(buf, busyPattern)

	if found {
		// Agent is busy — notify immediately if state changed
		if d.agentIdleTimer != nil {
			d.agentIdleTimer.Stop()
			d.agentIdleTimer = nil
		}
		if !d.agentBusy {
			d.agentBusy = true
			d.sendControlMessage("agent-status:busy")
		}
	} else if d.agentBusy {
		// Pattern gone — debounce before declaring idle
		if d.agentIdleTimer == nil {
			d.agentIdleTimer = time.AfterFunc(AgentIdleDebounce, func() {
				d.agentBusy = false
				d.agentIdleTimer = nil
				d.agentStatusBuf = d.agentStatusBuf[:0]
				d.sendControlMessage("agent-status:idle")
			})
		}
	}
}
