package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/user"
	"runtime"
	"strings"
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
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"hostname":   hostname,
		"user":       currentUser,
		"cliVersion": Version,
		"workingDir": d.workDir,
		"agent":      d.command,
		"agentType":  string(d.agentType),
		"sshRunning": sshRunning,
		"sshPort":    sshPort,
		"ips":        ips,
	}

	infoJSON, err := json.Marshal(info)
	if err != nil {
		return
	}

	d.sendControlMessage("cli-info:" + string(infoJSON))
}

// showMenu displays the AIPilot interactive menu
func (d *Daemon) showMenu() {
	fmt.Printf("\n%s=== AIPilot Menu (Ctrl+A) ===%s\n", bold, reset)
	fmt.Printf("  %s[1]%s Show pairing QR\n", cyan, reset)
	fmt.Printf("  %s[2]%s Open QR as image\n", cyan, reset)
	fmt.Printf("  %s[3]%s Connection status\n", cyan, reset)
	fmt.Printf("  %s[4]%s Disconnect mobile\n", cyan, reset)
	fmt.Printf("  %s[5]%s Purge all sessions\n", cyan, reset)
	fmt.Printf("  %s[q]%s Quit AIPilot\n", cyan, reset)
	fmt.Printf("  %s[Enter]%s Return to %s\n", cyan, reset, d.command)
	fmt.Print("\nChoice: ")

	var input string
	fmt.Scanln(&input)

	switch strings.ToLower(strings.TrimSpace(input)) {
	case "1":
		d.executeAIPilotCommand("qr")
	case "2":
		d.executeAIPilotCommand("qr-image")
	case "3":
		d.printStatus()
	case "4":
		d.disconnectMobile()
	case "5":
		d.purgeAllSessions()
	case "q":
		fmt.Printf("%sShutting down AIPilot...%s\n", yellow, reset)
		os.Exit(0)
	default:
		fmt.Println("Returning to", d.command)
	}
}
