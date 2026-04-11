package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TokenPath returns the path to the local API token file.
func TokenPath() string {
	return filepath.Join(Dir(), "token")
}

// EnsureToken returns the existing token or generates and persists a new one.
// The token file is created with mode 0600 (owner read/write only).
func EnsureToken() (string, error) {
	if err := EnsureDir(); err != nil {
		return "", err
	}
	path := TokenPath()
	data, err := os.ReadFile(path)
	if err == nil {
		token := strings.TrimSpace(string(data))
		if len(token) == 64 {
			return token, nil
		}
	}
	// Generate a new 32-byte (64 hex char) token.
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	token := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(token+"\n"), 0600); err != nil {
		return "", fmt.Errorf("writing token: %w", err)
	}
	return token, nil
}

// LoadToken reads the token from disk. Returns "" if not found or unreadable.
func LoadToken() string {
	data, err := os.ReadFile(TokenPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
