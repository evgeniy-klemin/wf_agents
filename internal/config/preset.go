package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ResolvePresetDir resolves an extends value to a local directory path.
//
// Resolution rules:
//   - ""                     → "", nil  (no preset)
//   - "gitlab:..." or "github:..." → remote fetch via FetchRemotePreset
//   - starts with "/"        → absolute path, validated via os.Stat
//   - otherwise              → named preset: $CLAUDE_PLUGIN_ROOT/presets/<name>/
func ResolvePresetDir(extends string) (string, error) {
	if extends == "" {
		return "", nil
	}

	if strings.HasPrefix(extends, "gitlab:") || strings.HasPrefix(extends, "github:") {
		ref, err := ParseRemoteRef(extends)
		if err != nil {
			return "", fmt.Errorf("parse remote ref %q: %w", extends, err)
		}
		return FetchRemotePreset(ref)
	}

	if strings.HasPrefix(extends, "/") {
		if _, err := os.Stat(extends); err != nil {
			return "", fmt.Errorf("preset dir not found: %s: %w", extends, err)
		}
		return extends, nil
	}

	// Expand tilde paths (e.g. ~/projects/.../presets/...) before falling through
	// to the named preset logic, which would otherwise incorrectly join them under
	// $CLAUDE_PLUGIN_ROOT/presets/~/...
	if strings.HasPrefix(extends, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot expand ~ in preset path: %w", err)
		}
		expanded := filepath.Join(home, extends[1:])
		if _, err := os.Stat(expanded); err != nil {
			return "", fmt.Errorf("preset dir not found: %s: %w", expanded, err)
		}
		return expanded, nil
	}

	// Named preset
	root, err := PluginRoot()
	if err != nil {
		return "", fmt.Errorf("cannot resolve named preset %q: %w", extends, err)
	}

	dir := filepath.Join(root, "presets", filepath.FromSlash(extends))
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("preset dir not found: %s: %w", dir, err)
	}
	return dir, nil
}

// ResolvePresetDirFromYAML extracts the extends field from raw YAML bytes and
// resolves it to a local directory. Intended for lightweight callers (e.g. phasedocs)
// that only need the preset dir without loading a full Config.
// Returns ("", nil) when extends is empty or not set.
func ResolvePresetDirFromYAML(data []byte) (string, error) {
	var partial struct {
		Extends string `yaml:"extends"`
	}
	if err := yaml.Unmarshal(data, &partial); err != nil {
		return "", err
	}
	if partial.Extends == "" {
		return "", nil
	}
	return ResolvePresetDir(partial.Extends)
}

// LoadPresetConfig reads <presetDir>/workflow.yaml and returns the parsed Config.
// If the file does not exist, an empty Config is returned (no error).
func LoadPresetConfig(presetDir string) (*Config, error) {
	if presetDir == "" {
		return &Config{}, nil
	}

	path := filepath.Join(presetDir, "workflow.yaml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read preset config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse preset config %s: %w", path, err)
	}
	return &cfg, nil
}
