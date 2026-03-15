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

// busyPattern is the lowercase pattern to detect in PTY output
var busyPattern = []byte("esc to ")

// initAgentStatus initializes the agent status detection fields
func (d *Daemon) initAgentStatus() {
	d.agentStatusBuf = make([]byte, 0, agentStatusBufSize)
}

// scanAgentStatus scans PTY output for the busy pattern.
// Called from startPTYReader on every read — must be fast.
func (d *Daemon) scanAgentStatus(data []byte) {
	// Append printable chars (lowercased) to rolling buffer, skip ANSI sequences
	inEscape := d.agentStatusInEscape
	buf := d.agentStatusBuf

	for _, b := range data {
		if inEscape {
			// Inside ANSI escape: wait for terminating letter
			if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') {
				inEscape = false
			}
			continue
		}
		if b == 0x1b {
			inEscape = true
			continue
		}
		// Only keep printable ASCII
		if b >= 0x20 && b <= 0x7e {
			// Lowercase on the fly
			if b >= 'A' && b <= 'Z' {
				b += 0x20
			}
			buf = append(buf, b)
		}
	}
	d.agentStatusInEscape = inEscape

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
				d.sendControlMessage("agent-status:idle")
			})
		}
	}
}
