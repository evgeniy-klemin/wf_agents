package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLeadProtocol_ProjectOverrideWins verifies that a team-lead.md placed in
// the project's .wf-agents/ directory wins over preset and plugin workflow/ default.
func TestLeadProtocol_ProjectOverrideWins(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	// Plugin default: root/workflow/team-lead.md
	require.NoError(t, os.MkdirAll(filepath.Join(root, "workflow"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "workflow", "team-lead.md"), []byte("plugin"), 0644))

	// Preset: presets/myorg/go/team-lead.md
	presetDir := filepath.Join(root, "presets", "myorg", "go")
	require.NoError(t, os.MkdirAll(presetDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(presetDir, "team-lead.md"), []byte("preset"), 0644))

	// Project: .wf-agents/team-lead.md — must win
	projectDir := t.TempDir()
	wfDir := filepath.Join(projectDir, ".wf-agents")
	require.NoError(t, os.MkdirAll(wfDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(wfDir, "workflow.yaml"), []byte("extends: myorg/go"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(wfDir, "team-lead.md"), []byte("project"), 0644))

	got, err := ResolveFile("team-lead.md", projectDir, filepath.Join(root, "workflow"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(wfDir, "team-lead.md"), got)
	assert.Contains(t, got, ".wf-agents")
}

// TestLeadProtocol_PresetFallback verifies that when no project override exists,
// team-lead.md resolves to the preset's copy.
func TestLeadProtocol_PresetFallback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	// Plugin default: root/workflow/team-lead.md
	require.NoError(t, os.MkdirAll(filepath.Join(root, "workflow"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "workflow", "team-lead.md"), []byte("plugin"), 0644))

	// Preset: presets/myorg/go/team-lead.md — must win (no project override)
	presetDir := filepath.Join(root, "presets", "myorg", "go")
	require.NoError(t, os.MkdirAll(presetDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(presetDir, "team-lead.md"), []byte("preset"), 0644))

	// Project: has workflow.yaml with extends but no team-lead.md override
	projectDir := t.TempDir()
	wfDir := filepath.Join(projectDir, ".wf-agents")
	require.NoError(t, os.MkdirAll(wfDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(wfDir, "workflow.yaml"), []byte("extends: myorg/go"), 0644))

	got, err := ResolveFile("team-lead.md", projectDir, filepath.Join(root, "workflow"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(presetDir, "team-lead.md"), got)
	assert.Contains(t, got, "presets")
}

// TestLeadProtocol_WorkflowDefaultFallback verifies that when neither project nor
// preset has team-lead.md, resolution falls back to plugin's workflow/ directory.
func TestLeadProtocol_WorkflowDefaultFallback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	// Plugin default: only location with team-lead.md
	require.NoError(t, os.MkdirAll(filepath.Join(root, "workflow"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "workflow", "team-lead.md"), []byte("plugin"), 0644))

	// Project: no .wf-agents dir, no preset reference
	projectDir := t.TempDir()

	got, err := ResolveFile("team-lead.md", projectDir, filepath.Join(root, "workflow"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "workflow", "team-lead.md"), got)
	assert.Contains(t, got, "workflow")
}

// TestLeadProtocol_NotFound verifies that an error is returned when team-lead.md
// is missing from all three levels.
func TestLeadProtocol_NotFound(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	// Plugin default workflow/ dir exists but has no team-lead.md
	require.NoError(t, os.MkdirAll(filepath.Join(root, "workflow"), 0755))

	projectDir := t.TempDir()

	_, err := ResolveFile("team-lead.md", projectDir, filepath.Join(root, "workflow"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "team-lead.md")
}
