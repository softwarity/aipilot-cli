package main

import (
	"encoding/json"
	"net"
	"runtime"
)

// sendCLIInfo sends CLI information to mobile
func (d *Daemon) sendCLIInfo() {
	// Gather all non-loopback IPv4 addresses for mobile connectivity
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

	// Use thorough SSH detection (ss/lsof/netstat/config parsing)
	sshInfo := DetectSSHInfo()

	info := map[string]interface{}{
		"os":          runtime.GOOS,
		"arch":        runtime.GOARCH,
		"hostname":    sshInfo.Hostname,
		"user":        sshInfo.Username,
		"cli_version": Version,
		"working_dir": d.workDir,
		"agent":       d.command,
		"agent_type":  string(d.agentType),
		"ssh_running": sshInfo.Available,
		"ssh_port":    sshInfo.Port,
		"ips":         ips,
	}

	infoJSON, err := json.Marshal(info)
	if err != nil {
		return
	}

	d.sendControlMessage("cli-info:" + string(infoJSON))
}

