package main

import (
	"encoding/json"
	"fmt"
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

// handleMobileInfo processes mobile-info control message and checks app version compatibility
func (d *Daemon) handleMobileInfo(args string) {
	var info struct {
		AppVersion string `json:"app_version"`
	}
	if err := json.Unmarshal([]byte(args), &info); err != nil {
		fmt.Printf("%s[mobile-info] Invalid JSON: %v%s\n", dim, err, reset)
		return
	}

	fmt.Printf("%s[mobile-info] App version: %s%s\n", dim, info.AppVersion, reset)

	appVer, err := parseSemver(info.AppVersion)
	if err != nil {
		return
	}
	minVer, err := parseSemver(MinAppVersion)
	if err != nil {
		return
	}

	// App is too old if its version is lower than MinAppVersion
	if appVer.Major < minVer.Major ||
		(appVer.Major == minVer.Major && appVer.Minor < minVer.Minor) ||
		(appVer.Major == minVer.Major && appVer.Minor == minVer.Minor && appVer.Patch < minVer.Patch) {
		msg := map[string]string{
			"min_app_version": MinAppVersion,
			"cli_version":     Version,
			"message":         fmt.Sprintf("Please update the app to at least v%s", MinAppVersion),
		}
		msgJSON, err := json.Marshal(msg)
		if err != nil {
			return
		}
		d.sendControlMessage("update-required:" + string(msgJSON))
	}
}

