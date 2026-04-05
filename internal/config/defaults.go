package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	wf "github.com/eklemin/wf-agents/workflow"
)

// DefaultConfig returns the embedded default configuration.
func DefaultConfig() (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(wf.DefaultsYAML, &cfg); err != nil {
		return nil, fmt.Errorf("parse defaults.yaml: %w", err)
	}
	return &cfg, nil
}

// LoadConfig loads the default config and applies a 3-level merge:
//
//	embedded defaults → preset (from extends field) → project (.wf-agents/workflow.yaml)
func LoadConfig(projectDir string) (*Config, error) {
	base, err := DefaultConfig()
	if err != nil {
		return nil, err
	}

	overridePath := filepath.Join(projectDir, ".wf-agents", "workflow.yaml")
	data, err := os.ReadFile(overridePath)
	if os.IsNotExist(err) {
		return base, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", overridePath, err)
	}

	var projectCfg Config
	if err := yaml.Unmarshal(data, &projectCfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", overridePath, err)
	}

	// Middle layer: preset
	if projectCfg.Extends != "" {
		presetDir, err := ResolvePresetDir(projectCfg.Extends)
		if err != nil {
			return nil, fmt.Errorf("resolve preset %q: %w", projectCfg.Extends, err)
		}
		presetCfg, err := LoadPresetConfig(presetDir)
		if err != nil {
			return nil, fmt.Errorf("load preset config: %w", err)
		}
		base = MergeConfigs(base, presetCfg)
	}

	return MergeConfigs(base, &projectCfg), nil
}
