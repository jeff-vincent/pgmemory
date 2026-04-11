// Package credential provides secure credential storage using the OS keychain.
//
// On macOS it uses the system Keychain via /usr/bin/security.
// On Linux it uses libsecret (secret-tool) if available.
package credential

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

const serviceName = "memoryd"

// Set stores a credential securely in the OS keychain.
func Set(key, value string) error {
	switch runtime.GOOS {
	case "darwin":
		return darwinSet(key, value)
	case "linux":
		return linuxSet(key, value)
	default:
		return fmt.Errorf("credential storage not supported on %s", runtime.GOOS)
	}
}

// Get retrieves a credential from the OS keychain.
// Returns ("", nil) if the key doesn't exist.
func Get(key string) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return darwinGet(key)
	case "linux":
		return linuxGet(key)
	default:
		return "", fmt.Errorf("credential storage not supported on %s", runtime.GOOS)
	}
}

// Delete removes a credential from the OS keychain.
func Delete(key string) error {
	switch runtime.GOOS {
	case "darwin":
		return darwinDelete(key)
	case "linux":
		return linuxDelete(key)
	default:
		return fmt.Errorf("credential storage not supported on %s", runtime.GOOS)
	}
}

// --- macOS: Keychain via /usr/bin/security ---

func darwinSet(key, value string) error {
	// Delete first to avoid "already exists" errors on update.
	_ = darwinDelete(key)

	cmd := exec.Command("/usr/bin/security", "add-generic-password",
		"-s", serviceName,
		"-a", key,
		"-w", value,
		"-U",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keychain set %q: %s (%w)", key, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func darwinGet(key string) (string, error) {
	cmd := exec.Command("/usr/bin/security", "find-generic-password",
		"-s", serviceName,
		"-a", key,
		"-w",
	)
	out, err := cmd.Output()
	if err != nil {
		// Exit code 44 = "could not be found" — not an error.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 44 {
			return "", nil
		}
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

func darwinDelete(key string) error {
	exec.Command("/usr/bin/security", "delete-generic-password",
		"-s", serviceName,
		"-a", key,
	).Run()
	return nil
}

// --- Linux: secret-tool (libsecret D-Bus) ---

func linuxSet(key, value string) error {
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return fmt.Errorf("secret-tool not found — install libsecret-tools for secure credential storage")
	}
	cmd := exec.Command("secret-tool", "store",
		"--label", fmt.Sprintf("memoryd %s", key),
		"service", serviceName,
		"key", key,
	)
	cmd.Stdin = strings.NewReader(value)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("secret-tool store %q: %s (%w)", key, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func linuxGet(key string) (string, error) {
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return "", nil
	}
	cmd := exec.Command("secret-tool", "lookup",
		"service", serviceName,
		"key", key,
	)
	out, err := cmd.Output()
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

func linuxDelete(key string) error {
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return nil
	}
	exec.Command("secret-tool", "clear",
		"service", serviceName,
		"key", key,
	).Run()
	return nil
}
