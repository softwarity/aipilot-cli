package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// RelayClient handles API calls to the relay server
type RelayClient struct {
	baseURL    string
	httpClient *http.Client
	pcConfig   *PCConfig
}

// NewRelayClient creates a new relay API client
func NewRelayClient(relayURL string, pcConfig *PCConfig) *RelayClient {
	// Convert WebSocket URL to HTTP URL
	baseURL := relayURL
	baseURL = strings.Replace(baseURL, "wss://", "https://", 1)
	baseURL = strings.Replace(baseURL, "ws://", "http://", 1)

	return &RelayClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: HTTPClientTimeout,
		},
		pcConfig: pcConfig,
	}
}

// setPCHeaders sets X-PC-ID and Authorization headers on a request
func (c *RelayClient) setPCHeaders(req *http.Request) {
	req.Header.Set("X-PC-ID", c.pcConfig.PCID)
	if c.pcConfig.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.pcConfig.Secret)
	}
}

// --- Pairing API ---

// PairingInitRequest is the request body for POST /api/pairing/init
type PairingInitRequest struct {
	PCID      string `json:"pc_id"`
	PCName    string `json:"pc_name"`
	PublicKey string `json:"public_key"`
}

// PairingInitResponse is the response from POST /api/pairing/init
type PairingInitResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	Secret    string `json:"secret,omitempty"`
}

// InitPairing initiates a pairing request and returns a token
func (c *RelayClient) InitPairing() (*PairingInitResponse, error) {
	req := PairingInitRequest{
		PCID:      c.pcConfig.PCID,
		PCName:    c.pcConfig.PCName,
		PublicKey: c.pcConfig.PublicKey,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Post(
		c.baseURL+"/api/pairing/init",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to relay: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("pairing init failed: %s (failed to read response: %v)", resp.Status, err)
		}
		return nil, fmt.Errorf("pairing init failed: %s - %s", resp.Status, string(respBody))
	}

	var result PairingInitResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Save secret if provided by relay (first registration or re-registration)
	if result.Secret != "" && c.pcConfig.Secret == "" {
		c.pcConfig.Secret = result.Secret
		savePCConfig(c.pcConfig)
	}

	return &result, nil
}

// PairingStatusResponse is the response when checking pairing status
type PairingStatusResponse struct {
	Status     string `json:"status"` // "pending", "completed", "expired"
	MobileID   string `json:"mobile_id,omitempty"`
	MobileName string `json:"mobile_name,omitempty"`
	PublicKey  string `json:"public_key,omitempty"`
}

// CheckPairingStatus checks if a pairing has been completed (HTTP polling - kept for fallback)
func (c *RelayClient) CheckPairingStatus(token string) (*PairingStatusResponse, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/api/pairing/status?token=" + token)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("pairing status check failed: %s (failed to read response: %v)", resp.Status, err)
		}
		return nil, fmt.Errorf("pairing status check failed: %s - %s", resp.Status, string(respBody))
	}

	var result PairingStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// WaitPairingViaWebSocket connects to the relay WebSocket and waits for pairing completion.
// This replaces HTTP polling (~150 requests per pairing → 1 WebSocket connection).
// Returns the pairing result or an error. Falls back to HTTP polling on WS failure.
func (c *RelayClient) WaitPairingViaWebSocket(token string, timeout time.Duration) (*PairingStatusResponse, error) {
	// Build WebSocket URL from HTTP base URL
	wsURL := c.baseURL
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL += "/ws/pairing/" + token

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		// Fallback to HTTP polling
		return c.pollPairingStatus(token, timeout)
	}
	defer conn.Close()

	// Set read deadline for timeout
	conn.SetReadDeadline(time.Now().Add(timeout))

	// Start ping goroutine to keep connection alive
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(PingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				conn.WriteJSON(map[string]string{"type": "ping"})
			}
		}
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("pairing WebSocket error: %w", err)
		}

		var result PairingStatusResponse
		if err := json.Unmarshal(message, &result); err != nil {
			continue // Ignore non-JSON messages (e.g. pong)
		}

		switch result.Status {
		case "completed":
			return &result, nil
		case "expired":
			return &result, nil
		case "":
			// Ignore messages without status (e.g. pong)
			continue
		}
	}
}

// pollPairingStatus is the HTTP polling fallback for pairing status
func (c *RelayClient) pollPairingStatus(token string, timeout time.Duration) (*PairingStatusResponse, error) {
	timeoutCh := time.After(timeout)
	ticker := time.NewTicker(PairingPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCh:
			return &PairingStatusResponse{Status: "expired"}, nil
		case <-ticker.C:
			status, err := c.CheckPairingStatus(token)
			if err != nil {
				continue
			}
			if status.Status != "pending" {
				return status, nil
			}
		}
	}
}

// --- Session API ---

// CreateSessionRequest is the request body for POST /api/sessions
type CreateSessionRequest struct {
	PCID            string            `json:"pc_id"`
	AgentType       string            `json:"agent_type"`
	WorkingDir      string            `json:"working_dir"`
	DisplayName     string            `json:"display_name"`      // Short name for display
	Token           string            `json:"token,omitempty"`   // Session token for E2E encryption
	EncryptedTokens map[string]string `json:"encrypted_tokens"`  // mobile_id -> encrypted token
	// SSH info for auto-setup
	SSHAvailable bool     `json:"ssh_available,omitempty"`
	SSHPort      int      `json:"ssh_port,omitempty"`
	Hostname     string   `json:"hostname,omitempty"`
	Username     string   `json:"username,omitempty"`
	IPs          []string `json:"ips,omitempty"` // Local network IPs
}

// CreateSessionResponse is the response from POST /api/sessions
type CreateSessionResponse struct {
	SessionID string `json:"session_id"`
	Token     string `json:"token"` // Session token for WebSocket auth
}

// SSHInfo contains SSH availability information for a session
type SSHInfo struct {
	Available bool
	Port      int
	Hostname  string
	Username  string
	IPs       []string
}

// CreateSession registers a new session on the relay
// It encrypts the session token for each paired mobile device
func (c *RelayClient) CreateSession(agentType, workDir, displayName string, sshInfo *SSHInfo) (*CreateSessionResponse, error) {
	// Get the PC's private key for encryption
	pcPrivateKey, err := GetPrivateKeyFromHex(c.pcConfig.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get private key: %w", err)
	}

	// Generate a session token (will be returned by relay, but we need to encrypt it for each mobile)
	// For now, we'll create the session first and then update with encrypted tokens
	// Actually, let's generate a token locally and encrypt it before sending
	sessionToken := generateRandomToken()

	// Encrypt token for each paired mobile
	encryptedTokens := make(map[string]string)
	for _, mobile := range c.pcConfig.PairedMobiles {
		if mobile.PublicKey == "" {
			// Skip mobiles without public key (legacy pairing)
			continue
		}
		encrypted, err := EncryptForMobile(sessionToken, mobile.PublicKey, pcPrivateKey)
		if err != nil {
			// Log but don't fail - mobile might not be able to connect directly
			fmt.Printf("Warning: Could not encrypt token for %s: %v\n", mobile.Name, err)
			continue
		}
		encryptedTokens[mobile.ID] = encrypted
	}

	req := CreateSessionRequest{
		PCID:            c.pcConfig.PCID,
		AgentType:       agentType,
		WorkingDir:      workDir,
		DisplayName:     displayName,
		Token:           sessionToken,
		EncryptedTokens: encryptedTokens,
	}

	// Add SSH info if available
	if sshInfo != nil {
		req.SSHAvailable = sshInfo.Available
		req.SSHPort = sshInfo.Port
		req.Hostname = sshInfo.Hostname
		req.Username = sshInfo.Username
		req.IPs = sshInfo.IPs
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest("POST", c.baseURL+"/api/sessions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.setPCHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("session creation failed: %s (failed to read response: %v)", resp.Status, err)
		}
		return nil, fmt.Errorf("session creation failed: %s - %s", resp.Status, string(respBody))
	}

	var result CreateSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Override the token with our locally generated one that matches the encrypted versions
	result.Token = sessionToken

	return &result, nil
}

// AddSessionTokenForMobile adds an encrypted token for a newly paired mobile
func (c *RelayClient) AddSessionTokenForMobile(sessionID, mobileID, encryptedToken string) error {
	payload := map[string]string{
		"mobile_id":       mobileID,
		"encrypted_token": encryptedToken,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequest("POST", c.baseURL+"/api/sessions/"+sessionID+"/tokens", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.setPCHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to add session token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("add session token failed: %s (failed to read response: %v)", resp.Status, err)
		}
		return fmt.Errorf("add session token failed: %s - %s", resp.Status, string(respBody))
	}

	return nil
}

// DeleteSession removes a session from the relay
func (c *RelayClient) DeleteSession(sessionID string) error {
	httpReq, err := http.NewRequest("DELETE", c.baseURL+"/api/sessions/"+sessionID, nil)
	if err != nil {
		return err
	}
	c.setPCHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("session deletion failed: %s (failed to read response: %v)", resp.Status, err)
		}
		return fmt.Errorf("session deletion failed: %s - %s", resp.Status, string(respBody))
	}

	return nil
}

// PurgeAllSessions removes all sessions for this PC from the relay
func (c *RelayClient) PurgeAllSessions() (int, error) {
	httpReq, err := http.NewRequest("DELETE", c.baseURL+"/api/sessions", nil)
	if err != nil {
		return 0, err
	}
	c.setPCHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("failed to purge sessions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return 0, fmt.Errorf("session purge failed: %s (failed to read response: %v)", resp.Status, err)
		}
		return 0, fmt.Errorf("session purge failed: %s - %s", resp.Status, string(respBody))
	}

	var result struct {
		Success      bool `json:"success"`
		DeletedCount int  `json:"deleted_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	return result.DeletedCount, nil
}

// --- Mobile Management API ---

// SessionInfo represents a session returned by the relay for CLI queries
type SessionInfo struct {
	ID              string `json:"id"`
	AgentType       string `json:"agent_type"`
	WorkingDir      string `json:"working_dir"`
	DisplayName     string `json:"display_name"`
	Status          string `json:"status"`
	Token           string `json:"token,omitempty"`
	CreatedAt       string `json:"created_at"`
}

// ListAllSessions returns all sessions for this PC
func (c *RelayClient) ListAllSessions() ([]SessionInfo, error) {
	reqURL := c.baseURL + "/api/sessions?for_cli=true"
	httpReq, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	c.setPCHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list sessions failed: %s - %s", resp.Status, string(respBody))
	}

	var sessions []SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

// UnpairMobile removes a paired mobile
func (c *RelayClient) UnpairMobile(mobileID string) error {
	httpReq, err := http.NewRequest("DELETE", c.baseURL+"/api/pairing/mobiles/"+mobileID, nil)
	if err != nil {
		return err
	}
	c.setPCHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to unpair mobile: %s (failed to read response: %v)", resp.Status, err)
		}
		return fmt.Errorf("failed to unpair mobile: %s - %s", resp.Status, string(respBody))
	}

	return nil
}
