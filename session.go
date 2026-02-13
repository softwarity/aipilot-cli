package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func generateRandomToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Critical: cannot generate secure random token
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

// listSessions lists all sessions from the relay, grouped by working directory
func listSessions(relayClient *RelayClient) {
	sessions, err := relayClient.ListAllSessions()
	if err != nil {
		fmt.Printf("%sError: could not list sessions: %v%s\n", red, err, reset)
		return
	}

	if len(sessions) == 0 {
		fmt.Printf("%sNo sessions found.%s\n", dim, reset)
		return
	}

	// Group by working directory
	groups := make(map[string][]SessionInfo)
	var order []string
	for _, s := range sessions {
		if _, exists := groups[s.WorkingDir]; !exists {
			order = append(order, s.WorkingDir)
		}
		groups[s.WorkingDir] = append(groups[s.WorkingDir], s)
	}

	fmt.Printf("%sSessions:%s\n\n", bold, reset)
	for _, wd := range order {
		fmt.Printf("  %s%s%s\n", cyan, wd, reset)
		for _, s := range groups[wd] {
			fmt.Printf("    %s  %s  %s\n", s.ID, s.AgentType, s.CreatedAt)
		}
	}
	fmt.Println()
}

// killAllSessions purges all sessions for this PC from the relay
func killAllSessions(relayClient *RelayClient) {
	count, err := relayClient.PurgeAllSessions()
	if err != nil {
		fmt.Printf("%sError: could not purge sessions: %v%s\n", red, err, reset)
		return
	}
	if count > 0 {
		fmt.Printf("%s✓ Killed %d session(s).%s\n", green, count, reset)
	} else {
		fmt.Printf("%sNo sessions to kill.%s\n", dim, reset)
	}
}

// killSessionByID deletes a specific session by ID
func killSessionByID(id string, relayClient *RelayClient) {
	sessions, err := relayClient.ListAllSessions()
	if err != nil {
		fmt.Printf("%sError: could not query sessions: %v%s\n", red, err, reset)
		return
	}

	for _, s := range sessions {
		if s.ID == id || (len(id) >= 8 && len(s.ID) >= 8 && s.ID[:8] == id[:8]) {
			if err := relayClient.DeleteSession(s.ID); err != nil {
				fmt.Printf("%sError: could not delete session: %v%s\n", red, err, reset)
				return
			}
			fmt.Printf("%s✓ Killed session %s (%s)%s\n", green, s.ID[:8]+"...", s.WorkingDir, reset)
			return
		}
	}

	fmt.Printf("%sError: session not found: %s%s\n", red, id, reset)
}
