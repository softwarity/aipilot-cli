package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/user"
	"runtime"
)

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

