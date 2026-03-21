package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
)

// HookMessage is the generic JSON format received on the hook socket.
// Extensible: new event types can be added without changing the socket protocol.
type HookMessage struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// AgentStatusData is the data payload for "agent_status" events
type AgentStatusData struct {
	Status string `json:"status"` // "busy" or "idle"
}

// startHookSocket creates a Unix domain socket and listens for hook events.
// Each connection sends one JSON message per line, then closes.
func (d *Daemon) startHookSocket(socketPath string) {
	// Remove stale socket file if it exists
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		fmt.Printf("%s[hook] Failed to create socket %s: %v%s\n", dim, socketPath, err, reset)
		return
	}

	d.mu.Lock()
	d.hookSocketListener = listener
	d.hookSocketPath = socketPath
	d.mu.Unlock()

	fmt.Printf("%s[hook] Listening on %s%s\n", dim, socketPath, reset)

	go d.hookSocketAcceptLoop(listener)
}

// hookSocketAcceptLoop accepts connections and processes messages
func (d *Daemon) hookSocketAcceptLoop(listener net.Listener) {
	var wg sync.WaitGroup
	for {
		conn, err := listener.Accept()
		if err != nil {
			// Listener closed (shutdown)
			break
		}
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			defer c.Close()
			d.hookSocketHandleConn(c)
		}(conn)
	}
	wg.Wait()
}

// hookSocketHandleConn reads JSON messages from a single connection
func (d *Daemon) hookSocketHandleConn(conn net.Conn) {
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg HookMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			fmt.Printf("%s[hook] Invalid JSON: %v%s\n", dim, err, reset)
			continue
		}

		d.handleHookMessage(msg)
	}
}

// handleHookMessage dispatches a hook message by event type
func (d *Daemon) handleHookMessage(msg HookMessage) {
	switch msg.Event {
	case "agent_status":
		d.handleHookAgentStatus(msg.Data)
	default:
		fmt.Printf("%s[hook] Unknown event: %s%s\n", dim, msg.Event, reset)
	}
}

// handleHookAgentStatus processes agent_status events from hooks
func (d *Daemon) handleHookAgentStatus(data json.RawMessage) {
	var status AgentStatusData
	if err := json.Unmarshal(data, &status); err != nil {
		fmt.Printf("%s[hook] Invalid agent_status data: %v%s\n", dim, err, reset)
		return
	}

	// Mark that we're receiving status via socket — disable PTY scan
	d.agentStatusViaSocket = true

	switch status.Status {
	case "busy":
		if d.agentIdleTimer != nil {
			d.agentIdleTimer.Stop()
			d.agentIdleTimer = nil
		}
		if !d.agentBusy {
			d.agentBusy = true
			d.sendControlMessage("agent-status:busy")
			fmt.Printf("%s[hook] Agent status: busy%s\n", dim, reset)
		}
	case "idle":
		if d.agentBusy {
			d.agentBusy = false
			d.sendControlMessage("agent-status:idle")
			fmt.Printf("%s[hook] Agent status: idle%s\n", dim, reset)
		}
	}
}

// stopHookSocket closes the socket listener and removes the socket file
func (d *Daemon) stopHookSocket() {
	d.mu.Lock()
	listener := d.hookSocketListener
	socketPath := d.hookSocketPath
	d.hookSocketListener = nil
	d.mu.Unlock()

	if listener != nil {
		listener.Close()
	}
	if socketPath != "" {
		os.Remove(socketPath)
	}
}
