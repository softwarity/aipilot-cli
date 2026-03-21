package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
)

// agentEventMain is the entry point for --hook mode.
// It reads a Claude Code hook payload from stdin, maps it to a generic
// HookMessage, and sends it to the AIPILOT_HOOK_SOCKET Unix socket.
// Exits silently with code 0 in all cases (must not block the agent).
func agentEventMain() {
	socketPath := os.Getenv("AIPILOT_HOOK_SOCKET")
	if socketPath == "" {
		// Not running under aipilot — exit silently
		os.Exit(0)
	}

	// Read hook payload from stdin
	input, err := io.ReadAll(os.Stdin)
	if err != nil || len(input) == 0 {
		os.Exit(0)
	}

	// Extract hook_event_name from the payload
	var payload struct {
		HookEventName string `json:"hook_event_name"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		os.Exit(0)
	}

	// Map agent-specific event to generic hook message
	msg := mapHookEvent(payload.HookEventName)
	if msg == nil {
		// Unknown or unhandled event — ignore
		os.Exit(0)
	}

	// Send to socket
	data, err := json.Marshal(msg)
	if err != nil {
		os.Exit(0)
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		// Socket not available (CLI may have exited) — exit silently
		os.Exit(0)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "%s\n", data)
}

// mapHookEvent maps a hook event name to a generic HookMessage.
// Currently supports Claude Code events. Add other agents here.
func mapHookEvent(eventName string) *HookMessage {
	switch eventName {
	case "UserPromptSubmit":
		return newAgentStatusMessage("busy")
	case "Stop", "StopFailure", "Notification":
		return newAgentStatusMessage("idle")
	default:
		return nil
	}
}

// newAgentStatusMessage creates a HookMessage for agent_status events
func newAgentStatusMessage(status string) *HookMessage {
	data, _ := json.Marshal(AgentStatusData{Status: status})
	return &HookMessage{
		Event: "agent_status",
		Data:  data,
	}
}
