package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/user"
	"runtime"
)

// printStatus shows the current status
func (d *Daemon) printStatus() {
	d.mu.RLock()
	defer d.mu.RUnlock()

	fmt.Printf("\n%s=== AIPilot Status ===%s\n", bold, reset)
	fmt.Printf("  Session:  %s\n", d.session[:8]+"...")
	fmt.Printf("  Command:  %s\n", d.command)
	fmt.Printf("  WorkDir:  %s\n", d.workDir)
	fmt.Printf("  Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)

	// Relay status
	if d.relayConnected {
		fmt.Printf("  Relay:    %s✓ Connected%s\n", green, reset)
	} else {
		fmt.Printf("  Relay:    %s✗ Disconnected%s\n", red, reset)
	}

	// Mobile status
	if d.mobileConnected {
		fmt.Printf("  Mobile:   %s✓ Connected%s\n", green, reset)
	} else {
		fmt.Printf("  Mobile:   %s⋯ Waiting%s\n", yellow, reset)
	}

	// Agent status
	if d.running {
		fmt.Printf("  Agent:    %s✓ Running%s\n", green, reset)
	} else {
		fmt.Printf("  Agent:    %s✗ Stopped%s\n", red, reset)
	}
	fmt.Println()
}

// sendCLIInfo sends CLI information to mobile
func (d *Daemon) sendCLIInfo() {
	hostname, err := os.Hostname()
	if err != nil {
		fmt.Printf("%sWarning: Could not get hostname: %v%s\n", yellow, err, reset)
		hostname = "unknown"
	}

	currentUser := "unknown"
	if u, err := user.Current(); err == nil {
		currentUser = u.Username
	}

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

	// Quick SSH check
	sshPort := DefaultSSHPort
	sshRunning := false
	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", DefaultSSHPort), SSHQuickCheckTimeout); err == nil {
		conn.Close()
		sshRunning = true
	} else if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", AlternativeSSHPort), SSHQuickCheckTimeout); err == nil {
		conn.Close()
		sshPort = AlternativeSSHPort
		sshRunning = true
	}

	info := map[string]interface{}{
		"os":          runtime.GOOS,
		"arch":        runtime.GOARCH,
		"hostname":    hostname,
		"user":        currentUser,
		"cli_version": Version,
		"working_dir": d.workDir,
		"agent":       d.command,
		"agent_type":  string(d.agentType),
		"ssh_running": sshRunning,
		"ssh_port":    sshPort,
		"ips":         ips,
	}

	infoJSON, err := json.Marshal(info)
	if err != nil {
		return
	}

	d.sendControlMessage("cli-info:" + string(infoJSON))
}

