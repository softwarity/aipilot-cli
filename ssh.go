package main

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// installSSHKey installs an SSH public key to authorized_keys
// It removes any existing key for this mobileId before adding the new one
func (d *Daemon) installSSHKey(username, mobileId, keyBase64 string) {
	keyBytes, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		d.sendControlMessage("ssh-setup-result:error:Invalid key encoding")
		return
	}
	publicKey := strings.TrimSpace(string(keyBytes))

	home, err := os.UserHomeDir()
	if err != nil {
		d.sendControlMessage("ssh-setup-result:error:Cannot find home directory")
		return
	}

	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, DirPermissions); err != nil {
		d.sendControlMessage("ssh-setup-result:error:Cannot create .ssh directory")
		return
	}

	authKeysPath := filepath.Join(sshDir, "authorized_keys")

	// Build the comment for this mobile (used to identify keys)
	keyComment := fmt.Sprintf("aipilot-%s", mobileId)

	existingKeys, readErr := os.ReadFile(authKeysPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		fmt.Printf("%s[AIPilot] Warning: Could not read authorized_keys: %v%s\n", yellow, readErr, reset)
	}

	// Check if exact key already installed
	if strings.Contains(string(existingKeys), publicKey) {
		d.sendControlMessage("ssh-setup-result:success:Key already installed")
		return
	}

	// Remove existing key for this mobileId (if any)
	var newLines []string
	removedOld := false
	if len(existingKeys) > 0 {
		lines := strings.Split(string(existingKeys), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Check if this line contains the mobileId comment
			if strings.Contains(line, keyComment) {
				removedOld = true
				continue // Skip this line (remove old key)
			}
			newLines = append(newLines, line)
		}
	}

	// Add the new key
	keyLine := fmt.Sprintf("%s %s", publicKey, keyComment)
	newLines = append(newLines, keyLine)

	// Write back the file
	content := strings.Join(newLines, "\n") + "\n"
	if err := os.WriteFile(authKeysPath, []byte(content), FilePermissions); err != nil {
		d.sendControlMessage("ssh-setup-result:error:Cannot write authorized_keys")
		return
	}

	if removedOld {
		d.sendControlMessage("ssh-setup-result:success:Key updated (replaced old key)")
		fmt.Printf("\n%s[AIPilot] SSH key updated for mobile %s%s\n", green, mobileId[:8], reset)
	} else {
		d.sendControlMessage("ssh-setup-result:success:Key installed successfully")
		fmt.Printf("\n%s[AIPilot] SSH key installed for mobile %s%s\n", green, mobileId[:8], reset)
	}
}

// DetectSSHInfo detects SSH availability without requiring a Daemon instance
// Returns SSHInfo that can be used when creating a session
// Uses proper system detection (ss/lsof/netstat) to find the actual SSH port
func DetectSSHInfo() *SSHInfo {
	currentUser := "unknown"
	if u, err := user.Current(); err == nil {
		currentUser = u.Username
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	// Try to detect SSH port using system tools
	sshPort := 0
	sshRunning := false

	// Method 1: Find sshd process and its port via system tools
	if port := detectSSHPortFromSystemStandalone(); port > 0 {
		sshPort = port
		sshRunning = true
	}

	// Method 2: Read from sshd_config and verify
	if !sshRunning {
		if port := detectSSHPortFromConfigStandalone(); port > 0 {
			if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), SSHConnectTimeout); err == nil {
				conn.Close()
				sshPort = port
				sshRunning = true
			}
		}
	}

	// Method 3: Fallback to common ports
	if !sshRunning {
		commonPorts := []int{DefaultSSHPort, AlternativeSSHPort, 22222, 222}
		for _, port := range commonPorts {
			if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), SSHQuickCheckTimeout); err == nil {
				conn.Close()
				sshPort = port
				sshRunning = true
				break
			}
		}
	}

	// Collect local IPs
	var ips []string
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					ips = append(ips, ipnet.IP.String())
				}
			}
		}
	}

	return &SSHInfo{
		Available: sshRunning,
		Port:      sshPort,
		Hostname:  hostname,
		Username:  currentUser,
		IPs:       ips,
	}
}

// detectSSHPortFromSystemStandalone finds sshd listening port from system tools (standalone version)
func detectSSHPortFromSystemStandalone() int {
	switch runtime.GOOS {
	case "linux":
		// Try ss first (modern Linux)
		if out, err := exec.Command("ss", "-tlnp").Output(); err == nil {
			lines := strings.Split(string(out), "\n")
			for _, line := range lines {
				if strings.Contains(line, "sshd") {
					if port := parsePortFromLine(line); port > 0 {
						return port
					}
				}
			}
		}
		// Fallback to netstat
		if out, err := exec.Command("netstat", "-tlnp").Output(); err == nil {
			lines := strings.Split(string(out), "\n")
			for _, line := range lines {
				if strings.Contains(line, "sshd") || strings.Contains(line, ":22 ") {
					if port := parsePortFromLine(line); port > 0 {
						return port
					}
				}
			}
		}

	case "darwin": // macOS
		if out, err := exec.Command("lsof", "-i", "-P", "-n").Output(); err == nil {
			lines := strings.Split(string(out), "\n")
			for _, line := range lines {
				if strings.Contains(line, "sshd") && strings.Contains(line, "LISTEN") {
					if port := parsePortFromLsofLine(line); port > 0 {
						return port
					}
				}
			}
		}
		if out, err := exec.Command("netstat", "-an").Output(); err == nil {
			lines := strings.Split(string(out), "\n")
			for _, line := range lines {
				if strings.Contains(line, "LISTEN") && strings.Contains(line, ".22 ") {
					return 22
				}
			}
		}

	case "windows":
		if out, err := exec.Command("netstat", "-an").Output(); err == nil {
			lines := strings.Split(string(out), "\n")
			for _, line := range lines {
				if strings.Contains(line, "LISTENING") && strings.Contains(line, ":22 ") {
					return 22
				}
			}
		}
		if out, err := exec.Command("powershell", "-Command",
			"Get-Service sshd -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Status").Output(); err == nil {
			if strings.Contains(string(out), "Running") {
				return 22
			}
		}
	}

	return 0
}

// detectSSHPortFromConfigStandalone reads SSH port from sshd_config (standalone version)
func detectSSHPortFromConfigStandalone() int {
	configPaths := []string{
		"/etc/ssh/sshd_config",
		"/etc/sshd_config",
		"/usr/local/etc/ssh/sshd_config",
	}

	for _, path := range configPaths {
		if data, err := os.ReadFile(path); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "Port ") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						if port, err := strconv.Atoi(parts[1]); err == nil {
							return port
						}
					}
				}
			}
			return 22 // Config exists but no Port specified = default 22
		}
	}

	return 0
}

// parsePortFromLine extracts port from ss/netstat output line
func parsePortFromLine(line string) int {
	fields := strings.Fields(line)
	for _, field := range fields {
		if strings.Contains(field, ":") && !strings.Contains(field, "*") {
			parts := strings.Split(field, ":")
			if len(parts) >= 2 {
				if port, err := strconv.Atoi(parts[len(parts)-1]); err == nil && port > 0 && port < 65536 {
					return port
				}
			}
		}
	}
	return 0
}

// parsePortFromLsofLine extracts port from lsof output (macOS)
func parsePortFromLsofLine(line string) int {
	fields := strings.Fields(line)
	for _, field := range fields {
		if strings.Contains(field, ":") && strings.Contains(field, "*:") {
			parts := strings.Split(field, ":")
			if len(parts) >= 2 {
				portStr := strings.Split(parts[1], " ")[0]
				if port, err := strconv.Atoi(portStr); err == nil && port > 0 && port < 65536 {
					return port
				}
			}
		}
	}
	return 0
}
