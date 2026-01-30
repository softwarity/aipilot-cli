package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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
			Timeout: 30 * time.Second,
		},
		pcConfig: pcConfig,
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
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("pairing init failed: %s - %s", resp.Status, string(respBody))
	}

	var result PairingInitResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// PairingCompleteRequest is sent by the mobile to complete pairing
type PairingCompleteRequest struct {
	Token      string `json:"token"`
	MobileID   string `json:"mobile_id"`
	MobileName string `json:"mobile_name"`
	PublicKey  string `json:"public_key"`
}

// PairingStatusResponse is the response when checking pairing status
type PairingStatusResponse struct {
	Status     string `json:"status"` // "pending", "completed", "expired"
	MobileID   string `json:"mobile_id,omitempty"`
	MobileName string `json:"mobile_name,omitempty"`
	PublicKey  string `json:"public_key,omitempty"`
}

// CheckPairingStatus checks if a pairing has been completed
func (c *RelayClient) CheckPairingStatus(token string) (*PairingStatusResponse, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/api/pairing/status?token=" + token)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("pairing status check failed: %s - %s", resp.Status, string(respBody))
	}

	var result PairingStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// --- Session API ---

// CreateSessionRequest is the request body for POST /api/sessions
type CreateSessionRequest struct {
	PCID            string            `json:"pc_id"`
	AgentType       string            `json:"agent_type"`
	WorkingDir      string            `json:"working_dir"`
	DisplayName     string            `json:"display_name"`      // Short name for display
	EncryptedTokens map[string]string `json:"encrypted_tokens"`  // mobile_id -> encrypted token
}

// CreateSessionResponse is the response from POST /api/sessions
type CreateSessionResponse struct {
	SessionID string `json:"session_id"`
	Token     string `json:"token"` // Session token for WebSocket auth
}

// CreateSession registers a new session on the relay
// It encrypts the session token for each paired mobile device
func (c *RelayClient) CreateSession(agentType, workDir, displayName string) (*CreateSessionResponse, error) {
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
		EncryptedTokens: encryptedTokens,
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
	httpReq.Header.Set("X-PC-ID", c.pcConfig.PCID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
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
	body, err := json.Marshal(map[string]string{
		"mobile_id":       mobileID,
		"encrypted_token": encryptedToken,
	})
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequest("POST", c.baseURL+"/api/sessions/"+sessionID+"/tokens", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-PC-ID", c.pcConfig.PCID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to add session token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
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
	httpReq.Header.Set("X-PC-ID", c.pcConfig.PCID)
	// TODO: Add signature header for auth

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
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
	httpReq.Header.Set("X-PC-ID", c.pcConfig.PCID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("failed to purge sessions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("session purge failed: %s - %s", resp.Status, string(respBody))
	}

	var result struct {
		Success      bool `json:"success"`
		DeletedCount int  `json:"deletedCount"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	return result.DeletedCount, nil
}

// --- Mobile Management API ---

// ListPairedMobiles returns the list of mobiles paired with this PC
func (c *RelayClient) ListPairedMobiles() ([]PairedMobile, error) {
	httpReq, err := http.NewRequest("GET", c.baseURL+"/api/pairing/mobiles", nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("X-PC-ID", c.pcConfig.PCID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list mobiles: %s - %s", resp.Status, string(respBody))
	}

	var result []PairedMobile
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// UnpairMobile removes a paired mobile
func (c *RelayClient) UnpairMobile(mobileID string) error {
	httpReq, err := http.NewRequest("DELETE", c.baseURL+"/api/pairing/mobiles/"+mobileID, nil)
	if err != nil {
		return err
	}
	httpReq.Header.Set("X-PC-ID", c.pcConfig.PCID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to unpair mobile: %s - %s", resp.Status, string(respBody))
	}

	return nil
}
