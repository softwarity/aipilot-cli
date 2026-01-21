package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// AgentType represents known AI agent types
type AgentType string

const (
	AgentClaude  AgentType = "claude"
	AgentGemini  AgentType = "gemini"
	AgentOpenAI  AgentType = "openai"
	AgentAider   AgentType = "aider"
	AgentGeneric AgentType = "generic"
)

// AgentInfo contains information about a detected agent
type AgentInfo struct {
	Command   string
	Type      AgentType
	Version   string
	Available bool
}

// knownAgents lists all known AI terminal agents to detect
var knownAgents = []struct {
	command     string
	agentType   AgentType
	displayName string
}{
	{"claude", AgentClaude, "Claude Code"},
	{"aider", AgentAider, "Aider"},
	{"gemini", AgentGemini, "Google Gemini CLI"},
}

func checkCommand(command string) (string, error) {
	path, err := exec.LookPath(command)
	if err != nil {
		return "", fmt.Errorf("command '%s' not found in PATH", command)
	}
	return path, nil
}

func detectAgentType(command string) AgentType {
	cmd := strings.ToLower(command)
	switch {
	case strings.Contains(cmd, "claude"):
		return AgentClaude
	case strings.Contains(cmd, "gemini"):
		return AgentGemini
	case strings.Contains(cmd, "gpt") || strings.Contains(cmd, "codex") || strings.Contains(cmd, "openai"):
		return AgentOpenAI
	case strings.Contains(cmd, "aider"):
		return AgentAider
	default:
		return AgentGeneric
	}
}

func getAgentVersion(command string, agentType AgentType) string {
	cmd := exec.Command(command, "--version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	version := strings.TrimSpace(strings.Split(string(output), "\n")[0])

	if len(version) > 50 {
		version = version[:47] + "..."
	}

	if strings.Contains(strings.ToLower(version), "install") ||
		strings.Contains(strings.ToLower(version), "error") ||
		strings.Contains(version, "?") {
		return ""
	}

	return version
}

// detectAvailableAgents scans for installed AI agents
func detectAvailableAgents() []AgentInfo {
	var available []AgentInfo

	for _, agent := range knownAgents {
		if _, err := exec.LookPath(agent.command); err == nil {
			version := getAgentVersion(agent.command, agent.agentType)
			available = append(available, AgentInfo{
				Command:   agent.command,
				Type:      agent.agentType,
				Version:   version,
				Available: true,
			})
		}
	}

	return available
}

// selectAgent prompts user to select an agent if multiple are available
func selectAgent(agents []AgentInfo) string {
	if len(agents) == 0 {
		return ""
	}

	if len(agents) == 1 {
		fmt.Printf("%sDetected: %s%s\n", dim, agents[0].Command, reset)
		return agents[0].Command
	}

	fmt.Printf("\n%s=== Available AI Agents ===%s\n", bold, reset)
	for i, agent := range agents {
		versionStr := ""
		if agent.Version != "" {
			versionStr = fmt.Sprintf(" %s(%s)%s", dim, agent.Version, reset)
		}
		fmt.Printf("  %s[%d]%s %s%s\n", cyan, i+1, reset, agent.Command, versionStr)
	}
	fmt.Println()
	fmt.Printf("Select agent [1-%d]: ", len(agents))

	var input string
	fmt.Scanln(&input)

	var selection int
	if _, err := fmt.Sscanf(input, "%d", &selection); err != nil || selection < 1 || selection > len(agents) {
		fmt.Printf("%sUsing default: %s%s\n", dim, agents[0].Command, reset)
		return agents[0].Command
	}

	return agents[selection-1].Command
}
