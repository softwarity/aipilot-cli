package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// PairedMobile represents a mobile device paired with this PC
type PairedMobile struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
	PairedAt  string `json:"paired_at"`
}

// PCConfig represents the PC's identity and paired devices
type PCConfig struct {
	PCID         string         `json:"pc_id"`
	PCName       string         `json:"pc_name"`
	PrivateKey   string         `json:"private_key"`
	PublicKey    string         `json:"public_key"`
	PairedMobiles []PairedMobile `json:"paired_mobiles"`
	CreatedAt    string         `json:"created_at"`
}

// DirectoryConfig represents remembered agent choice per directory
type DirectoryConfig struct {
	DefaultAgent string `json:"default_agent"`
	LastUsed     string `json:"last_used"`
}

// DirectoriesConfig maps directory paths to their config
type DirectoriesConfig map[string]DirectoryConfig

// getConfigDir returns the aipilot config directory path
func getConfigDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		// Fallback to home directory
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine config directory: %w", err)
		}
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "aipilot"), nil
}

// ensureConfigDir creates the config directory if it doesn't exist
func ensureConfigDir() (string, error) {
	dir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, DirPermissions); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}
	return dir, nil
}

// loadPCConfig loads the PC configuration
func loadPCConfig() (*PCConfig, error) {
	dir, err := getConfigDir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No config yet
		}
		return nil, err
	}

	var config PCConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// savePCConfig saves the PC configuration
func savePCConfig(config *PCConfig) error {
	dir, err := ensureConfigDir()
	if err != nil {
		return err
	}

	path := filepath.Join(dir, "config.json")
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, FilePermissions)
}

// createPCConfig creates a new PC configuration with generated keys
func createPCConfig() (*PCConfig, error) {
	// Generate X25519 key pair for NaCl box encryption
	priv, pub, err := GenerateX25519KeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %w", err)
	}

	// Get hostname for PC name
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "Unknown PC"
	}

	config := &PCConfig{
		PCID:          uuid.New().String(),
		PCName:        hostname,
		PrivateKey:    hex.EncodeToString(priv[:]),
		PublicKey:     hex.EncodeToString(pub[:]),
		PairedMobiles: []PairedMobile{},
		CreatedAt:     time.Now().Format(time.RFC3339),
	}

	if err := savePCConfig(config); err != nil {
		return nil, err
	}

	return config, nil
}

// getOrCreatePCConfig loads existing config or creates a new one
func getOrCreatePCConfig() (*PCConfig, error) {
	config, err := loadPCConfig()
	if err != nil {
		return nil, err
	}

	if config == nil {
		return createPCConfig()
	}

	return config, nil
}

// hasPairedMobiles returns true if at least one mobile is paired
func (c *PCConfig) hasPairedMobiles() bool {
	return len(c.PairedMobiles) > 0
}

// getPairedMobile returns a paired mobile by ID, or nil if not found
func (c *PCConfig) getPairedMobile(mobileID string) *PairedMobile {
	for i := range c.PairedMobiles {
		if c.PairedMobiles[i].ID == mobileID {
			return &c.PairedMobiles[i]
		}
	}
	return nil
}

// addPairedMobile adds a new paired mobile
func (c *PCConfig) addPairedMobile(mobile PairedMobile) {
	// Check if already exists
	for i, m := range c.PairedMobiles {
		if m.ID == mobile.ID {
			// Update existing
			c.PairedMobiles[i] = mobile
			return
		}
	}
	c.PairedMobiles = append(c.PairedMobiles, mobile)
}

// removePairedMobile removes a paired mobile by ID
func (c *PCConfig) removePairedMobile(mobileID string) bool {
	for i, m := range c.PairedMobiles {
		if m.ID == mobileID {
			c.PairedMobiles = append(c.PairedMobiles[:i], c.PairedMobiles[i+1:]...)
			return true
		}
	}
	return false
}

// loadDirectoriesConfig loads the directories configuration
func loadDirectoriesConfig() (DirectoriesConfig, error) {
	dir, err := getConfigDir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, "directories.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(DirectoriesConfig), nil
		}
		return nil, err
	}

	var config DirectoriesConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return config, nil
}

// saveDirectoriesConfig saves the directories configuration
func saveDirectoriesConfig(config DirectoriesConfig) error {
	dir, err := ensureConfigDir()
	if err != nil {
		return err
	}

	path := filepath.Join(dir, "directories.json")
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, FilePermissions)
}

// getDirectoryAgent returns the default agent for a directory
func getDirectoryAgent(workDir string) string {
	config, err := loadDirectoriesConfig()
	if err != nil {
		return ""
	}

	if dc, ok := config[workDir]; ok {
		return dc.DefaultAgent
	}
	return ""
}

// setDirectoryAgent saves the default agent for a directory
func setDirectoryAgent(workDir, agent string) error {
	config, err := loadDirectoriesConfig()
	if err != nil {
		return err
	}

	config[workDir] = DirectoryConfig{
		DefaultAgent: agent,
		LastUsed:     time.Now().Format(time.RFC3339),
	}

	return saveDirectoriesConfig(config)
}

// PairingQRData is the data encoded in the pairing QR code
type PairingQRData struct {
	Type      string `json:"type"` // "pairing"
	Relay     string `json:"r"`
	Token     string `json:"t"`
	PCID      string `json:"pc"`
	PCName    string `json:"n"`
	PublicKey string `json:"k"`
	// Optional: session info for immediate display (backup if notification fails)
	SessionID    string `json:"s,omitempty"`
	WorkingDir   string `json:"wd,omitempty"`
	AgentType    string `json:"at,omitempty"`
	SSHAvailable bool   `json:"sa,omitempty"`
	SSHPort      int    `json:"sp,omitempty"`
	Hostname     string `json:"h,omitempty"`
	Username     string `json:"u,omitempty"`
}
