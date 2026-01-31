package main

import (
	"context"
	"encoding/base64"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// connectToRelay connects to the WebSocket relay
func (d *Daemon) connectToRelay() {
	for {
		wsURL := d.relay + "/ws/" + d.session + "?role=bridge"
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			d.setRelayConnected(false)
			time.Sleep(RelayConnectDelay)
			continue
		}

		// Wait for registration confirmation
		var response Message
		if err := conn.ReadJSON(&response); err != nil {
			conn.Close()
			time.Sleep(RelayConnectDelay)
			continue
		}

		if response.Type != "registered" {
			conn.Close()
			time.Sleep(RelayConnectDelay)
			continue
		}

		// Cancel any previous ping goroutine
		d.mu.Lock()
		if d.pingCancel != nil {
			d.pingCancel()
		}
		d.pingCtx, d.pingCancel = context.WithCancel(context.Background())
		pingCtx := d.pingCtx
		d.wsConn = conn
		d.relayConnected = true
		d.mu.Unlock()

		// Start ping keepalive with context cancellation
		go func(ctx context.Context) {
			ticker := time.NewTicker(PingInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
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
				}
			}
		}(pingCtx)

		// Handle incoming messages
		d.handleWebSocketMessages(conn)

		// Connection lost, cancel ping goroutine and retry
		d.mu.Lock()
		if d.pingCancel != nil {
			d.pingCancel()
			d.pingCancel = nil
		}
		d.wsConn = nil
		d.relayConnected = false
		d.mobileConnected = false
		d.mu.Unlock()

		time.Sleep(ReconnectDelay)
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
				// Trigger screen refresh by resizing PTY slightly
				cols, rows, err := d.getPTYSize()
				if err == nil && cols > 0 && rows > 0 {
					d.resizePTY(rows-1, cols)
					time.Sleep(50 * time.Millisecond)
					d.resizePTY(rows, cols)
				}
			}

		case "disconnected":
			d.setMobileConnected(false)

		case "pong":
			// Keepalive response
		}
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
			// Fallback to unencrypted if encryption fails
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
