package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// connectToRelay connects to the WebSocket relay
func (d *Daemon) connectToRelay() {
	for {
		wsURL := d.relay + "/ws/" + d.session + "?role=bridge&pc_id=" + d.pcConfig.PCID
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
			// If session expired on relay, recreate
			if response.Type == "error" {
				for {
					if err := d.recreateSession(); err == nil {
						break
					}
					time.Sleep(RelayConnectDelay)
				}
			} else {
				time.Sleep(RelayConnectDelay)
			}
			continue
		}

		// Set read deadline: if no message within 3 ping intervals, connection is dead
		conn.SetReadDeadline(time.Now().Add(PingInterval * 3))

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
						c.Close() // Force close to unblock ReadJSON
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

// recreateSession creates a new session on the relay after connection loss.
// Deletes the old session first to prevent ghost sessions, then creates a new one.
func (d *Daemon) recreateSession() error {
	// Clean up old session (best effort - may already be gone via alarm)
	d.mu.RLock()
	oldSession := d.session
	d.mu.RUnlock()
	if oldSession != "" {
		d.relayClient.DeleteSession(oldSession)
	}

	sshInfo := DetectSSHInfo()
	displayName := d.workDir
	if idx := strings.LastIndex(d.workDir, "/"); idx >= 0 {
		displayName = d.workDir[idx+1:]
	}

	sessionResp, err := d.relayClient.CreateSession(string(d.agentType), d.workDir, displayName, sshInfo)
	if err != nil {
		return err
	}

	d.mu.Lock()
	d.session = sessionResp.SessionID
	d.token = sessionResp.Token
	d.mu.Unlock()

	if err := d.initEncryption(); err != nil {
		return err
	}

	return nil
}

// handleWebSocketMessages processes incoming WebSocket messages
func (d *Daemon) handleWebSocketMessages(conn *websocket.Conn) {
	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		// Reset read deadline on any successful read
		conn.SetReadDeadline(time.Now().Add(PingInterval * 3))

		switch msg.Type {
		case "data":
			// Data from mobile -> PTY (decrypt first)
			data, err := d.decrypt(msg.Payload)
			if err != nil {
				fmt.Printf("%sDecryption failed, dropping message: %v%s\n", red, err, reset)
				continue
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
				// Don't trigger refresh here - wait for mobile's resize message
				// which arrives after mobile has set up its output listener
			}

		case "disconnected":
			d.setMobileConnected(false)

		case "mobile_paired":
			// A new mobile was paired on this PC - add our session token for it
			if msg.MobileID != "" && msg.PublicKey != "" {
				mobile := PairedMobile{
					ID:        msg.MobileID,
					Name:      msg.MobileName,
					PublicKey: msg.PublicKey,
				}
				// Update local pcConfig with the new mobile
				d.pcConfig.addPairedMobile(mobile)
				if err := savePCConfig(d.pcConfig); err != nil {
					fmt.Printf("%sFailed to save config: %v%s\n", red, err, reset)
				}
				// Add encrypted token for this session
				d.addTokenForMobile(mobile)
			}

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
		if d.aesGCM == nil {
			return // Encryption not initialized yet
		}
		encrypted, err := d.encrypt(data)
		if err != nil {
			fmt.Printf("%sEncryption failed, dropping message: %v%s\n", red, err, reset)
			return
		}

		msg := Message{
			Type:    "data",
			Payload: encrypted,
		}
		d.wsMu.Lock()
		err = conn.WriteJSON(msg)
		d.wsMu.Unlock()
		if err != nil {
			fmt.Printf("%sWebSocket write error: %v%s\n", red, err, reset)
		}
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
		if d.aesGCM == nil {
			return // Encryption not initialized yet
		}
		// Build control message: \x00CTRL:msg
		ctrlData := append([]byte{0x00}, []byte("CTRL:"+msg)...)

		encrypted, err := d.encrypt(ctrlData)
		if err != nil {
			fmt.Printf("%sControl message encryption failed: %v%s\n", red, err, reset)
			return
		}

		wsMsg := Message{
			Type:    "data",
			Payload: encrypted,
		}
		d.wsMu.Lock()
		err = conn.WriteJSON(wsMsg)
		d.wsMu.Unlock()
		if err != nil {
			fmt.Printf("%sWebSocket control write error: %v%s\n", red, err, reset)
		}
	}
}
