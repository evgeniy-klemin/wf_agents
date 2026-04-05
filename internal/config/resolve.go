package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// ResolveFile resolves a filename using the three-step search order:
//  1. <cwd>/.wf-agents/<filename>  (project-level override)
//  2. <presetDir>/<filename>        (preset, resolved from extends in <cwd>/.wf-agents/workflow.yaml)
//  3. <workflowDefaultDir>/<filename>      (workflow default directory, e.g. filepath.Join(root, "workflow"))
//
// Returns the absolute path of the first existing file found, or an error if none found.
// The caller is responsible for passing workflowDefaultDir (do not rely on CLAUDE_PLUGIN_ROOT internally).
func ResolveFile(filename, cwd, workflowDefaultDir string) (string, error) {
	// 1. Project-level override
	if cwd != "" {
		projectFile := filepath.Join(cwd, ".wf-agents", filename)
		if _, err := os.Stat(projectFile); err == nil {
			return projectFile, nil
		}
	}

	// 2. Preset-level
	if cwd != "" {
		cfgData, err := os.ReadFile(filepath.Join(cwd, ".wf-agents", "workflow.yaml"))
		if err == nil {
			presetDir, err := ResolvePresetDirFromYAML(cfgData)
			if err != nil {
				// extends is set but resolution failed — surface the error
				return "", fmt.Errorf("resolve preset: %w", err)
			}
			if presetDir != "" {
				presetFile := filepath.Join(presetDir, filename)
				if _, err := os.Stat(presetFile); err == nil {
					return presetFile, nil
				}
			}
		}
	}

	// 3. Workflow default directory
	if workflowDefaultDir != "" {
		defaultFile := filepath.Join(workflowDefaultDir, filename)
		if _, err := os.Stat(defaultFile); err == nil {
			return defaultFile, nil
		}
	}

	return "", fmt.Errorf("file %q not found (searched: project .wf-agents/, preset, plugin default %s)", filename, workflowDefaultDir)
}
