package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// PluginRoot returns the plugin root directory.
// Checks CLAUDE_PLUGIN_ROOT env var first, then falls back to
// filepath.Dir(filepath.Dir(os.Executable())) — binary lives in bin/,
// plugin root is one level up.
func PluginRoot() (string, error) {
	if root := os.Getenv("CLAUDE_PLUGIN_ROOT"); root != "" {
		return root, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("CLAUDE_PLUGIN_ROOT not set and cannot determine executable path: %w", err)
	}
	return filepath.Dir(filepath.Dir(exe)), nil
}
