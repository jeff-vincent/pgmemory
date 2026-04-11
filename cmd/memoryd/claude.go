package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
)

// registerMCPServers attempts to register memoryd as an MCP server in every
// supported coding agent found on this machine. Each registration is
// idempotent and non-fatal — failures are logged and skipped.
func registerMCPServers() {
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("[mcp-register] could not determine executable path: %v", err)
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("[mcp-register] could not determine home directory: %v", err)
		return
	}

	entry := mcpEntry(execPath)

	registrars := []struct {
		name string
		fn   func() error
	}{
		{"Claude Code", func() error { return registerClaudeCode(home, entry) }},
		{"Cursor", func() error { return registerStandard(home, cursorMCPPath(home), entry) }},
		{"Windsurf", func() error { return registerStandard(home, windsurfMCPPath(home), entry) }},
		{"Cline", func() error { return registerStandard(home, clineMCPPath(home), entry) }},
	}

	for _, r := range registrars {
		if err := r.fn(); err != nil {
			log.Printf("[mcp-register] %s: %v", r.name, err)
		}
	}
}

// mcpEntry returns the standard MCP server entry for memoryd.
func mcpEntry(execPath string) map[string]any {
	return map[string]any{
		"command": execPath,
		"args":    []string{"mcp"},
		"env":     map[string]any{},
	}
}

// --- Claude Code ---

// registerClaudeCode writes to ~/.claude/settings.json under
// projects[home].mcpServers. Created if it doesn't exist.
func registerClaudeCode(home string, entry map[string]any) error {
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	var settings map[string]any
	data, err := os.ReadFile(settingsPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &settings); err != nil {
			return err
		}
	}
	if settings == nil {
		settings = map[string]any{}
	}

	projects, _ := settings["projects"].(map[string]any)
	if projects == nil {
		projects = map[string]any{}
		settings["projects"] = projects
	}

	homeProject, _ := projects[home].(map[string]any)
	if homeProject == nil {
		homeProject = map[string]any{}
		projects[home] = homeProject
	}

	mcpServers, _ := homeProject["mcpServers"].(map[string]any)
	if mcpServers == nil {
		mcpServers = map[string]any{}
		homeProject["mcpServers"] = mcpServers
	}

	if _, exists := mcpServers["memoryd"]; exists {
		return nil
	}

	mcpServers["memoryd"] = entry

	return writeJSON(settingsPath, settings)
}

// --- Standard format (Cursor, Windsurf, Cline) ---

// registerStandard writes an {"mcpServers":{"memoryd":{...}}} config file.
// It only proceeds if the agent's config directory already exists (proving
// the agent is installed). The config file itself is created if absent.
func registerStandard(home, configPath string, entry map[string]any) error {
	dir := filepath.Dir(configPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil // agent not installed, skip silently
	}

	var cfg map[string]any
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return err
		}
	}
	if cfg == nil {
		cfg = map[string]any{}
	}

	mcpServers, _ := cfg["mcpServers"].(map[string]any)
	if mcpServers == nil {
		mcpServers = map[string]any{}
		cfg["mcpServers"] = mcpServers
	}

	if _, exists := mcpServers["memoryd"]; exists {
		return nil
	}

	mcpServers["memoryd"] = entry

	if err := writeJSON(configPath, cfg); err != nil {
		return err
	}

	log.Printf("[mcp-register] registered in %s", configPath)
	return nil
}

// --- Path helpers ---

func cursorMCPPath(home string) string {
	return filepath.Join(home, ".cursor", "mcp.json")
}

func windsurfMCPPath(home string) string {
	return filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")
}

func clineMCPPath(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Code", "User",
			"globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")
	default:
		return filepath.Join(home, ".config", "Code", "User",
			"globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")
	}
}

// --- Helpers ---

func writeJSON(path string, v any) error {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}
