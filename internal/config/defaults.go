package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

//go:embed defaults.yaml
var defaultsYAML []byte

// DefaultConfig returns the embedded default configuration.
func DefaultConfig() (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(defaultsYAML, &cfg); err != nil {
		return nil, fmt.Errorf("parse defaults.yaml: %w", err)
	}
	return &cfg, nil
}

// LoadConfig loads the default config and optionally merges a project-level
// .wf-agents.yaml from projectDir if the file exists.
func LoadConfig(projectDir string) (*Config, error) {
	base, err := DefaultConfig()
	if err != nil {
		return nil, err
	}

	override := filepath.Join(projectDir, ".wf-agents.yaml")
	data, err := os.ReadFile(override)
	if os.IsNotExist(err) {
		return base, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", override, err)
	}

	var overrideCfg Config
	if err := yaml.Unmarshal(data, &overrideCfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", override, err)
	}

	return MergeConfigs(base, &overrideCfg), nil
}
