package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"

	"github.com/jeff-vincent/pgmemory/internal/config"
)

func main() {
	systray.Run(onReady, onExit)
}

var (
	daemonCmd  *exec.Cmd
	daemonDone chan struct{} // closed by the background goroutine when the process exits
	daemonMu   sync.Mutex

	// uiGrace tracks the deadline until which the health poll should
	// not override click-handler state. This prevents the 3-second poll
	// from racing with daemon startup (takes several seconds) and from
	// seeing a still-alive daemon after the user clicked Stop.
	uiGrace   time.Time
	uiGraceMu sync.Mutex
)

func onReady() {
	systray.SetTitle("M")
	systray.SetTooltip("pgmemory – memory layer for coding agents")

	mStatus := systray.AddMenuItem("Status: checking...", "Daemon status")
	mStatus.Disable()

	mDB := systray.AddMenuItem("Database: checking...", "Postgres connection status")
	mDB.Disable()

	mConnectDB := systray.AddMenuItem("Configure Postgres...", "Set Postgres connection URL")
	mSetKey := systray.AddMenuItem("Set Anthropic Key...", "Store your Anthropic API key in the OS keychain")

	// Show checkmarks if credentials are already configured.
	if config.GetAnthropicAPIKey() != "" {
		mSetKey.SetTitle("Set Anthropic Key  ✓")
		mSetKey.Check()
	}

	systray.AddSeparator()

	mToggle := systray.AddMenuItem("Start", "Start or stop the daemon")
	mDash := systray.AddMenuItem("Open Dashboard", "API keys, LLM synthesis, and pipeline settings")

	systray.AddSeparator()

	// --- Mode submenu ---
	mMode := systray.AddMenuItem("Mode", "How pgmemory integrates with your agent")
	mModeProxy := mMode.AddSubMenuItem("Proxy – auto read & write", "Proxy intercepts API calls, captures everything automatically")
	mModeMCP := mMode.AddSubMenuItem("MCP – read & write", "Agent uses MCP tools to search and store explicitly")
	mModeMCPRO := mMode.AddSubMenuItem("MCP – read only", "Agent can search memories but cannot store new ones")

	systray.AddSeparator()

	mUninstall := systray.AddMenuItem("Uninstall pgmemory...", "Remove pgmemory and all its data")

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Quit", "Quit pgmemory tray")

	// Gate items behind MongoDB connection — disabled until connected.
	mSetKey.Disable()
	mToggle.Disable()
	mDash.Disable()
	mMode.Disable()

	cfg, _ := config.Load()
	port := cfg.Port

	// Apply initial mode checkmarks.
	setModeChecks(cfg.Mode, mModeProxy, mModeMCP, mModeMCPRO)

	binaryPath := findBinary()

	// Auto-start daemon on launch if not already running.
	if !checkHealth(port) {
		setStartGrace()
		startDaemon(binaryPath)
	}

	// Poll daemon health every 3 seconds.
	running := false
	synthActive := false
	dbConnected := false
	go func() {
		for {
			health := getHealth(port)
			ok := health != nil
			// During a grace period (start or stop), don't override click-handler state.
			if inUIGrace() {
				time.Sleep(3 * time.Second)
				continue
			}

			// Track synthesis status changes.
			if ok {
				newSynth, _ := health["synthesis"].(bool)
				if newSynth != synthActive {
					synthActive = newSynth
				}
			}

			// Track database connection status and gate menu items.
			if ok {
				dbStr, _ := health["database"].(string)
				newDBConnected := dbStr == "connected"
				if newDBConnected != dbConnected {
					dbConnected = newDBConnected
					if dbConnected {
						mDB.SetTitle("Database: ✅ connected")
						mSetKey.Enable()
						mToggle.Enable()
						mDash.Enable()
						mMode.Enable()
					} else if dbStr == "connecting" {
						mDB.SetTitle("Database: 🔄 connecting...")
					} else {
						mDB.SetTitle("Database: ❌ disconnected")
						mSetKey.Disable()
						mDash.Disable()
						mMode.Disable()
					}
				}
			}

			if ok != running {
				running = ok
				if running {
					subtitle := fmt.Sprintf("Status: ● running on port %d", port)
					if synthActive {
						subtitle += "  ✦ synthesis"
					}
					mStatus.SetTitle(subtitle)
					mToggle.SetTitle("Stop")
					if dbConnected {
						systray.SetTitle("M●")
					} else {
						systray.SetTitle("M⚠")
					}
				} else {
					synthActive = false
					dbConnected = false
					mStatus.SetTitle("Status: ○ stopped")
					mDB.SetTitle("Database: —")
					mToggle.SetTitle("Start")
					mToggle.Enable() // allow starting even when disconnected
					mSetKey.Disable()
					mDash.Disable()
					mMode.Disable()
					systray.SetTitle("M○")
				}
			}
			time.Sleep(3 * time.Second)
		}
	}()

	for {
		select {
		case <-mModeProxy.ClickedCh:
			switchMode(config.ModeProxy, mModeProxy, mModeMCP, mModeMCPRO, binaryPath, &running)

		case <-mModeMCP.ClickedCh:
			switchMode(config.ModeMCP, mModeProxy, mModeMCP, mModeMCPRO, binaryPath, &running)

		case <-mModeMCPRO.ClickedCh:
			switchMode(config.ModeMCPReadOnly, mModeProxy, mModeMCP, mModeMCPRO, binaryPath, &running)

		case <-mToggle.ClickedCh:
			if running {
				setStopGrace()
				stopDaemon()
				running = false
				dbConnected = false
				mToggle.SetTitle("Start")
				mStatus.SetTitle("Status: ○ stopped")
				mDB.SetTitle("Database: —")
				mSetKey.Disable()
				mDash.Disable()
				mMode.Disable()
				systray.SetTitle("M○")
			} else {
				setStartGrace()
				startDaemon(binaryPath)
				running = true
				mToggle.SetTitle("Stop")
				mStatus.SetTitle("Status: ● running on port " + fmt.Sprintf("%d", port))
				mDB.SetTitle("Database: 🔄 connecting...")
				systray.SetTitle("M●")
			}

		case <-mConnectDB.ClickedCh:
			go configurePostgresDialog(binaryPath, &running)

		case <-mSetKey.ClickedCh:
			go func() {
				setAnthropicKeyDialog(binaryPath, &running)
				if config.GetAnthropicAPIKey() != "" {
					mSetKey.SetTitle("Set Anthropic Key  ✓")
					mSetKey.Check()
				}
			}()

		case <-mDash.ClickedCh:
			openDashboard(port)

		case <-mUninstall.ClickedCh:
			go uninstallDialog(binaryPath)

		case <-mQuit.ClickedCh:
			stopDaemon()
			systray.Quit()
		}
	}
}

func onExit() {
	stopDaemon()
}

func checkHealth(port int) bool {
	return getHealth(port) != nil
}

// getHealth fetches the full health response from the daemon, or nil if unreachable.
func getHealth(port int) map[string]any {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if json.Unmarshal(body, &result) != nil {
		return nil
	}
	if result["status"] != "ok" {
		return nil
	}
	return result
}

// setUIGrace sets a grace period during which the health poll won't override
// the state set by the click handler. The start case needs 15 s (daemon boot),
// the stop case only needs a few seconds for the process to exit.
func setUIGrace(d time.Duration) {
	uiGraceMu.Lock()
	uiGrace = time.Now().Add(d)
	uiGraceMu.Unlock()
}

func setStartGrace() { setUIGrace(15 * time.Second) }
func setStopGrace()  { setUIGrace(5 * time.Second) }

// inUIGrace returns true if we're within a UI grace period.
func inUIGrace() bool {
	uiGraceMu.Lock()
	defer uiGraceMu.Unlock()
	return time.Now().Before(uiGrace)
}

// Keep old names around for the test file.
func inStartGrace() bool { return inUIGrace() }

func startDaemon(binary string) {
	daemonMu.Lock()
	defer daemonMu.Unlock()

	if daemonCmd != nil && daemonCmd.Process != nil {
		return // already running
	}

	cmd := exec.Command(binary, "start")
	// Send logs to a file
	logDir := config.Dir()
	os.MkdirAll(logDir, 0700)
	logPath := filepath.Join(logDir, "daemon.log")
	logFile, err := os.OpenFile(logPath,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	if err := cmd.Start(); err != nil {
		return
	}
	daemonCmd = cmd
	done := make(chan struct{})
	daemonDone = done

	// Wait in background so we can clean up
	go func() {
		err := cmd.Wait()
		daemonMu.Lock()
		if daemonCmd == cmd {
			daemonCmd = nil
			daemonDone = nil
		}
		daemonMu.Unlock()
		close(done) // unblock any stopDaemon waiting on this channel
		if logFile != nil {
			logFile.Close()
		}

		// If the daemon exited with an error, surface the reason to the user.
		if err != nil {
			reason := extractCrashReason(logPath)
			if reason == "" {
				reason = err.Error()
			}
			exec.Command("osascript", "-e",
				fmt.Sprintf(`display notification "%s" with title "pgmemory stopped" subtitle "The daemon exited unexpectedly"`, reason)).Run()
		}
	}()
}

func stopDaemon() {
	daemonMu.Lock()
	if daemonCmd == nil || daemonCmd.Process == nil {
		daemonMu.Unlock()
		return
	}
	daemonCmd.Process.Signal(os.Interrupt)
	done := daemonDone // reuse the channel already waited on by startDaemon's goroutine
	daemonMu.Unlock()

	// Wait for the existing background goroutine (which calls cmd.Wait) to confirm exit.
	// Do NOT call Process.Wait() again — concurrent waitpid on the same PID is undefined.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		daemonMu.Lock()
		if daemonCmd != nil && daemonCmd.Process != nil {
			daemonCmd.Process.Kill()
		}
		daemonMu.Unlock()
		// Give Kill a moment to land, then move on.
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}

	daemonMu.Lock()
	daemonCmd = nil
	daemonDone = nil
	daemonMu.Unlock()
}

// extractCrashReason reads the daemon log and returns the last error line.
func extractCrashReason(logPath string) string {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")

	// Walk backwards looking for a line that looks like an error.
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-20; i-- {
		line := lines[i]
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "failed") {
			// Strip the log timestamp prefix if present (e.g., "2024/01/01 12:00:00 ")
			if idx := strings.Index(line, " "); idx > 0 {
				if idx2 := strings.Index(line[idx+1:], " "); idx2 > 0 {
					candidate := line[idx+1+idx2+1:]
					if len(candidate) > 10 {
						line = candidate
					}
				}
			}
			// Truncate for notification display.
			if len(line) > 120 {
				line = line[:120] + "..."
			}
			return line
		}
	}
	return ""
}

func findBinary() string {
	// 1. Common install locations (prefer installed copy so updates take effect).
	if runtime.GOOS == "darwin" {
		for _, p := range []string{"/usr/local/bin/pgmemory", "/opt/homebrew/bin/pgmemory"} {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}

	// 2. PATH lookup
	if p, err := exec.LookPath("pgmemory"); err == nil {
		return p
	}

	// 3. Same directory as the tray binary (fallback for dev / app bundle)
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	candidate := filepath.Join(dir, "pgmemory")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}

	return "pgmemory" // hope PATH has it
}

func configurePostgresDialog(binaryPath string, running *bool) {
	// Read current value to show as default.
	cfg, _ := config.Load()
	defaultURL := cfg.PostgresURL
	if defaultURL == "" {
		defaultURL = "postgres://user:pass@host:5432/dbname?sslmode=require"
	}

	urlOut, err := exec.Command("osascript",
		"-e", fmt.Sprintf(`display dialog "Enter your Postgres connection URL:\n\nLeave blank to use the built-in embedded database." default answer "%s" buttons {"Cancel", "Save"} default button "Save" with title "pgmemory – Postgres"`, defaultURL),
		"-e", `text returned of result`,
	).Output()
	if err != nil {
		return // cancelled
	}
	url := strings.TrimSpace(string(urlOut))

	// Store the URL (empty string clears it, reverting to embedded mode).
	if err := config.StoreCredential("postgres_url", url); err != nil {
		exec.Command("osascript", "-e",
			fmt.Sprintf(`display dialog "Failed to save connection URL: %s" buttons {"OK"} with icon stop with title "pgmemory"`, err.Error())).Run()
		return
	}

	// Restart daemon to pick up the new connection.
	stopDaemon()
	time.Sleep(500 * time.Millisecond)
	setStartGrace()
	startDaemon(binaryPath)
	*running = true

	msg := "Connecting to Postgres — pgmemory is restarting"
	if url == "" {
		msg = "Using embedded Postgres — pgmemory is restarting"
	}
	exec.Command("osascript", "-e",
		fmt.Sprintf(`display notification "%s" with title "pgmemory"`, msg)).Run()
}

// setAnthropicKeyDialog prompts the user for their Anthropic API key and
// stores it in the OS keychain, then restarts the daemon to pick it up.
func setAnthropicKeyDialog(binaryPath string, running *bool) {
	keyOut, err := exec.Command("osascript",
		"-e", `display dialog "Enter your Anthropic API key:" default answer "" with hidden answer buttons {"Cancel", "Save"} default button "Save" with title "pgmemory – Anthropic Key"`,
		"-e", `text returned of result`,
	).Output()
	if err != nil {
		return // cancelled
	}
	key := strings.TrimSpace(string(keyOut))
	if key == "" {
		return
	}
	if err := config.SaveAnthropicAPIKey(key); err != nil {
		exec.Command("osascript", "-e",
			fmt.Sprintf(`display dialog "Failed to save API key: %s" buttons {"OK"} with icon stop with title "pgmemory"`, err.Error())).Run()
		return
	}

	// Restart daemon to pick up the new key.
	if *running {
		stopDaemon()
		time.Sleep(500 * time.Millisecond)
		setStartGrace()
		startDaemon(binaryPath)
	}

	exec.Command("osascript", "-e",
		`display notification "Anthropic API key saved" with title "pgmemory"`).Run()
}

// setModeChecks updates submenu check marks to reflect the active mode.
func setModeChecks(mode string, proxy, mcp, mcpRO *systray.MenuItem) {
	proxy.Uncheck()
	mcp.Uncheck()
	mcpRO.Uncheck()

	switch mode {
	case config.ModeMCP:
		mcp.Check()
	case config.ModeMCPReadOnly:
		mcpRO.Check()
	default: // proxy is the default
		proxy.Check()
	}
}

// switchMode persists the new mode, updates submenu checks, and restarts the daemon.
func switchMode(mode string, proxy, mcp, mcpRO *systray.MenuItem, binaryPath string, running *bool) {
	if err := config.SetMode(mode); err != nil {
		exec.Command("osascript", "-e",
			fmt.Sprintf(`display dialog "Failed to set mode: %s" buttons {"OK"} with icon stop with title "pgmemory"`, err.Error())).Run()
		return
	}

	setModeChecks(mode, proxy, mcp, mcpRO)

	// Restart daemon so it picks up the new mode.
	if *running {
		stopDaemon()
		time.Sleep(500 * time.Millisecond)
		setStartGrace()
		startDaemon(binaryPath)
	}

	label := map[string]string{
		config.ModeProxy:       "Proxy – auto read & write",
		config.ModeMCP:         "MCP – read & write",
		config.ModeMCPReadOnly: "MCP – read only",
	}[mode]
	exec.Command("osascript", "-e",
		fmt.Sprintf(`display notification "Mode set to: %s" with title "pgmemory"`, label)).Run()
}

// uninstallDialog confirms with the user and removes pgmemory components.
func uninstallDialog(binaryPath string) {
	// Confirm.
	_, err := exec.Command("osascript", "-e",
		`display dialog "This will stop the daemon and remove:\n\n• pgmemory binary & llama-server\n• Pgmemory.app\n• ~/.pgmemory (config, models, data)\n• MCP config entries (Claude Code, Claude Desktop, Cursor, Windsurf)\n• Stored credentials (OS keychain)\n\nThis cannot be undone." buttons {"Cancel", "Uninstall"} default button "Cancel" with icon caution with title "Uninstall pgmemory"`, "-e",
		`button returned of result`).Output()
	if err != nil {
		return // user cancelled
	}

	// 1. Stop daemon.
	stopDaemon()

	// 2. Kill llama-server (embedding subprocess).
	exec.Command("pkill", "-f", "llama-server").Run()

	var removed []string
	var failed []string

	// 2b. Remove keychain credentials.
	config.DeleteCredentials()
	removed = append(removed, "Keychain credentials")

	// 3. Remove MCP config entries from all known agents.
	home, _ := os.UserHomeDir()
	agentConfigs := []struct {
		name string
		path string
	}{
		{"Claude Code", filepath.Join(home, ".mcp.json")},
		{"Claude Desktop", filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")},
		{"Cursor", filepath.Join(home, ".cursor", "mcp.json")},
		{"Windsurf", filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")},
	}
	for _, ac := range agentConfigs {
		if cleanMCPConfig(ac.path) {
			removed = append(removed, ac.name+" MCP config")
		}
	}

	// 5. Remove ~/.pgmemory.
	pgmemoryDir := config.Dir()
	if _, err := os.Stat(pgmemoryDir); err == nil {
		if os.RemoveAll(pgmemoryDir) == nil {
			removed = append(removed, "~/.pgmemory")
		} else {
			failed = append(failed, "~/.pgmemory (permission denied)")
		}
	}

	// 7. Remove Pgmemory.app.
	appPath := "/Applications/Pgmemory.app"
	if _, err := os.Stat(appPath); err == nil {
		if os.RemoveAll(appPath) == nil {
			removed = append(removed, "Pgmemory.app")
		} else {
			failed = append(failed, "Pgmemory.app (remove manually)")
		}
	}

	// 8. Remove binaries.
	if binaryPath != "" && binaryPath != "pgmemory" {
		if os.Remove(binaryPath) == nil {
			removed = append(removed, "pgmemory binary")
		} else {
			if exec.Command("sudo", "rm", "-f", binaryPath).Run() == nil {
				removed = append(removed, "pgmemory binary")
			} else {
				failed = append(failed, fmt.Sprintf("binary at %s (remove manually)", binaryPath))
			}
		}
	}

	// Remove llama-server if we installed it.
	llamaPath := filepath.Join(filepath.Dir(binaryPath), "llama-server")
	if binaryPath != "" && binaryPath != "pgmemory" {
		if _, err := os.Stat(llamaPath); err == nil {
			if os.Remove(llamaPath) == nil {
				removed = append(removed, "llama-server binary")
			} else {
				exec.Command("sudo", "rm", "-f", llamaPath).Run()
				removed = append(removed, "llama-server binary")
			}
		}
	}

	// 9. Show result.
	msg := "pgmemory has been uninstalled."
	if len(removed) > 0 {
		msg += "\n\nRemoved:\n• " + strings.Join(removed, "\n• ")
	}
	if len(failed) > 0 {
		msg += "\n\nCould not remove:\n• " + strings.Join(failed, "\n• ")
	}
	msg += "\n\nThe tray app will now quit."

	exec.Command("osascript", "-e",
		fmt.Sprintf(`display dialog "%s" buttons {"OK"} with title "pgmemory"`, msg)).Run()

	systray.Quit()
}

// cleanMCPConfig removes the pgmemory (and legacy memoryd) entries from an MCP config file.
// Works with any agent that uses the standard {"mcpServers": {...}} format.
func cleanMCPConfig(configPath string) bool {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}

	var cfg map[string]any
	if json.Unmarshal(data, &cfg) != nil {
		return false
	}

	servers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		return false
	}

	_, hasPgmemory := servers["pgmemory"]
	_, hasLegacy := servers["memoryd"]
	if !hasPgmemory && !hasLegacy {
		return false
	}

	delete(servers, "pgmemory")
	delete(servers, "memoryd")
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false
	}

	return os.WriteFile(configPath, out, 0600) == nil
}

func openDashboard(port int) {
	token := config.LoadToken()
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	if token != "" {
		url += "?token=" + token
	}
	exec.Command("open", url).Start()
}
