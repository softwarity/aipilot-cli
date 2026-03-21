package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Claude Code specific hook installation.
// This file contains only Claude-specific code. When another agent
// supports hooks, create a similar hook_<agent>.go file.

// claudeSettingsPath returns the path to Claude Code's settings.json
func claudeSettingsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// hookCommand is the command that Claude Code will execute for each hook event
const hookCommand = "aipilot-cli --agent-event"

// claudeHookEvents lists the Claude Code events we want to hook into
var claudeHookEvents = []string{"UserPromptSubmit", "Stop", "StopFailure", "Notification"}

// ensureClaudeHooksInstalled reads ~/.claude/settings.json and adds
// aipilot hook entries for agent status detection if not already present.
func ensureClaudeHooksInstalled() {
	settingsPath := claudeSettingsPath()
	if settingsPath == "" {
		return
	}

	// Read existing settings
	settings, err := readClaudeSettings(settingsPath)
	if err != nil {
		fmt.Printf("%s[hook] Cannot read Claude settings: %v%s\n", dim, err, reset)
		return
	}

	// Ensure hooks section exists
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		hooks = make(map[string]interface{})
		settings["hooks"] = hooks
	}

	modified := false
	for _, eventName := range claudeHookEvents {
		if addClaudeHookIfMissing(hooks, eventName) {
			modified = true
		}
	}

	if !modified {
		return
	}

	// Write back
	if err := writeClaudeSettings(settingsPath, settings); err != nil {
		fmt.Printf("%s[hook] Cannot write Claude settings: %v%s\n", dim, err, reset)
		return
	}

	fmt.Printf("%s[hook] Installed aipilot hooks in %s%s\n", dim, settingsPath, reset)
}

// addClaudeHookIfMissing adds an aipilot hook entry to the given event array
// if one doesn't already exist. Returns true if the settings were modified.
func addClaudeHookIfMissing(hooks map[string]interface{}, eventName string) bool {
	// Get or create the event array
	var eventEntries []interface{}
	if existing, ok := hooks[eventName]; ok {
		if arr, ok := existing.([]interface{}); ok {
			eventEntries = arr
		}
	}

	// Check if aipilot hook is already installed
	for _, entry := range eventEntries {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		hooksList, ok := entryMap["hooks"].([]interface{})
		if !ok {
			continue
		}
		for _, h := range hooksList {
			hookMap, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			if cmd, ok := hookMap["command"].(string); ok && cmd == hookCommand {
				return false // Already installed
			}
		}
	}

	// Add new entry
	newEntry := map[string]interface{}{
		"matcher": "",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": hookCommand,
				"timeout": 5,
				"async":   true,
			},
		},
	}

	hooks[eventName] = append(eventEntries, newEntry)
	return true
}

// readClaudeSettings reads and parses Claude Code's settings.json
func readClaudeSettings(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No settings file yet — start fresh
			return make(map[string]interface{}), nil
		}
		return nil, err
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	return settings, nil
}

// writeClaudeSettings writes settings back to Claude Code's settings.json
func writeClaudeSettings(path string, settings map[string]interface{}) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, append(data, '\n'), 0644)
}
