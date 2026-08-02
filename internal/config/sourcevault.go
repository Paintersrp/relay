package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ResolveSourceVaultDir resolves durable storage for retained source evidence.
func ResolveSourceVaultDir() (string, bool, error) {
	if value, ok := os.LookupEnv("RELAY_SOURCE_VAULT_DIR"); ok {
		if strings.TrimSpace(value) == "" {
			return "", true, fmt.Errorf("RELAY_SOURCE_VAULT_DIR must not be empty")
		}
		absolute, err := filepath.Abs(value)
		if err != nil {
			return "", true, fmt.Errorf("resolve RELAY_SOURCE_VAULT_DIR: %w", err)
		}
		return filepath.Clean(absolute), true, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", false, fmt.Errorf("determine durable application-data directory: set RELAY_SOURCE_VAULT_DIR or configure a user home directory")
	}
	return ResolveSourceVaultDirFor(runtime.GOOS, os.Getenv("LOCALAPPDATA"), os.Getenv("XDG_DATA_HOME"), home)
}

// ResolveSourceVaultDirFor is separated from process state so platform mapping
// remains deterministic and testable on every host platform.
func ResolveSourceVaultDirFor(goos, localAppData, xdgDataHome, home string) (string, bool, error) {
	var root string
	switch goos {
	case "windows":
		if strings.TrimSpace(localAppData) == "" {
			return "", false, fmt.Errorf("determine durable application-data directory: LOCALAPPDATA is not set; set RELAY_SOURCE_VAULT_DIR")
		}
		root = filepath.Join(localAppData, "Relay", "source-vaults")
	case "darwin":
		root = filepath.Join(home, "Library", "Application Support", "Relay", "source-vaults")
	default:
		if strings.TrimSpace(xdgDataHome) != "" {
			root = filepath.Join(xdgDataHome, "relay", "source-vaults")
		} else {
			root = filepath.Join(home, ".local", "share", "relay", "source-vaults")
		}
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", false, fmt.Errorf("resolve durable application-data directory: %w", err)
	}
	return filepath.Clean(abs), false, nil
}
