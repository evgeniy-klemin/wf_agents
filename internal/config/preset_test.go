package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePresetDir_Empty(t *testing.T) {
	dir, err := ResolvePresetDir("")
	require.NoError(t, err)
	assert.Equal(t, "", dir)
}

func TestResolvePresetDir_AbsolutePath(t *testing.T) {
	tmp := t.TempDir()
	dir, err := ResolvePresetDir(tmp)
	require.NoError(t, err)
	assert.Equal(t, tmp, dir)
}

func TestResolvePresetDir_AbsolutePath_NotFound(t *testing.T) {
	_, err := ResolvePresetDir("/nonexistent/path/that/does/not/exist")
	assert.Error(t, err)
}

func TestResolvePresetDir_Named(t *testing.T) {
	// Set up a fake CLAUDE_PLUGIN_ROOT with a preset dir
	root := t.TempDir()
	presetDir := filepath.Join(root, "presets", "iriski", "default-go")
	require.NoError(t, os.MkdirAll(presetDir, 0755))
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	dir, err := ResolvePresetDir("iriski/default-go")
	require.NoError(t, err)
	assert.Equal(t, presetDir, dir)
}

func TestResolvePresetDir_Named_NotFound(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	_, err := ResolvePresetDir("nonexistent/preset")
	assert.Error(t, err)
}

func TestResolvePresetDir_TildePath(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	// Create a temp dir, then compute its tilde-relative path from home.
	// t.TempDir() may or may not be under home; create it under home explicitly.
	tmpDir, err := os.MkdirTemp(homeDir, "wf-agents-tilde-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	// Build the tilde path: "~" + everything after homeDir
	tildeRelative := strings.TrimPrefix(tmpDir, homeDir)
	tildePath := "~" + tildeRelative

	dir, err := ResolvePresetDir(tildePath)
	require.NoError(t, err)
	assert.Equal(t, tmpDir, dir)
}

func TestResolvePresetDir_TildePath_NotFound(t *testing.T) {
	_, err := ResolvePresetDir("~/nonexistent-wf-agents-path-that-does-not-exist-12345")
	assert.Error(t, err)
}

func TestResolvePresetDir_Remote_GitLab(t *testing.T) {
	// Remote refs are dispatched to FetchRemotePreset — just verify routing (not actual fetch)
	// We don't test actual network calls here; see remote_test.go for those.
	// This test only verifies that a gitlab: prefix does NOT fall through to named preset logic.
	// We use a known-bad URI to confirm it takes the remote path (returns an error from fetch, not "not found in presets").
	_, err := ResolvePresetDir("gitlab:some/project//some/path")
	// Should error (no real glab available in test env), but error should not be "preset dir not found"
	assert.Error(t, err)
	assert.NotContains(t, err.Error(), "preset dir not found")
}

func TestLoadPresetConfig_MissingFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadPresetConfig(dir)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	// Empty preset — all fields zero
	assert.Empty(t, cfg.Tracking)
	assert.Empty(t, cfg.Guards)
}

func TestLoadPresetConfig_ValidFile(t *testing.T) {
	dir := t.TempDir()
	yaml := []byte(`
tracking:
  lint:
    patterns:
      - "task lint"
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow.yaml"), yaml, 0644))

	cfg, err := LoadPresetConfig(dir)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, []string{"task lint"}, cfg.Tracking["lint"].Patterns)
}

func TestLoadConfig_WithPreset(t *testing.T) {
	// Set up CLAUDE_PLUGIN_ROOT with a preset
	root := t.TempDir()
	presetDir := filepath.Join(root, "presets", "myorg", "go")
	require.NoError(t, os.MkdirAll(presetDir, 0755))
	presetYAML := []byte(`
tracking:
  lint:
    patterns:
      - "task lint"
  test:
    patterns:
      - "task test"
`)
	require.NoError(t, os.WriteFile(filepath.Join(presetDir, "workflow.yaml"), presetYAML, 0644))
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	// Set up project dir that extends the preset
	projectDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, ".wf-agents"), 0755))
	projectYAML := []byte(`
extends: myorg/go
`)
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".wf-agents", "workflow.yaml"), projectYAML, 0644))

	cfg, err := LoadConfig(projectDir)
	require.NoError(t, err)
	assert.Equal(t, []string{"task lint"}, cfg.Tracking["lint"].Patterns)
	assert.Equal(t, []string{"task test"}, cfg.Tracking["test"].Patterns)
}

func TestLoadConfig_ProjectOverridesPreset(t *testing.T) {
	root := t.TempDir()
	presetDir := filepath.Join(root, "presets", "myorg", "go")
	require.NoError(t, os.MkdirAll(presetDir, 0755))
	presetYAML := []byte(`
tracking:
  lint:
    patterns:
      - "preset-lint"
`)
	require.NoError(t, os.WriteFile(filepath.Join(presetDir, "workflow.yaml"), presetYAML, 0644))
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	projectDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, ".wf-agents"), 0755))
	projectYAML := []byte(`
extends: myorg/go
tracking:
  lint:
    patterns:
      - "project-lint"
`)
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".wf-agents", "workflow.yaml"), projectYAML, 0644))

	cfg, err := LoadConfig(projectDir)
	require.NoError(t, err)
	// Project overrides preset for same key
	assert.Equal(t, []string{"project-lint"}, cfg.Tracking["lint"].Patterns)
}
