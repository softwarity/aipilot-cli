package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SessionData represents a saved session for persistence
type SessionData struct {
	Session   string `json:"session"`
	Token     string `json:"token"`
	Relay     string `json:"relay"`
	Command   string `json:"command"`
	WorkDir   string `json:"workdir"`
	CreatedAt string `json:"created_at"`
}

func getSessionFilePath(workDir string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	h := sha256.Sum256([]byte(workDir))
	hash := hex.EncodeToString(h[:8])
	return filepath.Join(home, ".aipilot", "sessions", fmt.Sprintf("%s.json", hash))
}

func loadSession(workDir string) (*SessionData, error) {
	path := getSessionFilePath(workDir)
	if path == "" {
		return nil, fmt.Errorf("cannot determine home directory")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var session SessionData
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}

	return &session, nil
}

func saveSession(workDir string, session *SessionData) error {
	path := getSessionFilePath(workDir)
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, DirPermissions); err != nil {
		return err
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, FilePermissions)
}

func generateRandomToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Critical: cannot generate secure random token
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

// listSavedSessions lists all saved sessions
func listSavedSessions() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("%sError: cannot determine home directory%s\n", red, reset)
		return
	}

	sessionsDir := filepath.Join(home, ".aipilot", "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		fmt.Printf("%sNo saved sessions found.%s\n", dim, reset)
		return
	}

	if len(entries) == 0 {
		fmt.Printf("%sNo saved sessions found.%s\n", dim, reset)
		return
	}

	fmt.Printf("%sSaved sessions:%s\n\n", bold, reset)
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			path := filepath.Join(sessionsDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var session SessionData
			if err := json.Unmarshal(data, &session); err != nil {
				continue
			}
			fmt.Printf("  %s%s%s\n", cyan, session.WorkDir, reset)
			fmt.Printf("    Session: %s...  Created: %s\n", session.Session[:8], session.CreatedAt)
		}
	}
	fmt.Println()
}

// clearSavedSessions removes all saved sessions (local and relay)
func clearSavedSessions(relayClient *RelayClient) {
	// Clear local sessions
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("%sError: cannot determine home directory%s\n", red, reset)
		return
	}

	sessionsDir := filepath.Join(home, ".aipilot", "sessions")
	entries, err := os.ReadDir(sessionsDir)
	localCount := 0
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
				path := filepath.Join(sessionsDir, entry.Name())
				if err := os.Remove(path); err == nil {
					localCount++
				}
			}
		}
	}

	// Clear relay sessions
	relayCount := 0
	if relayClient != nil {
		count, err := relayClient.PurgeAllSessions()
		if err == nil {
			relayCount = count
		}
	}

	if localCount > 0 || relayCount > 0 {
		fmt.Printf("%s✓ Cleared %d local + %d relay session(s).%s\n", green, localCount, relayCount, reset)
	} else {
		fmt.Printf("%sNo sessions to clear.%s\n", dim, reset)
	}
}
