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
