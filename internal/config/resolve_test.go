package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ResolveFile applies a three-level priority order:
//   1. project:  <cwd>/.wf-agents/<filename>
//   2. preset:   <CLAUDE_PLUGIN_ROOT>/presets/<org>/<name>/<filename>  (from workflow.yaml "extends")
//   3. default:  workflowDefaultDir/<filename>  (e.g. <CLAUDE_PLUGIN_ROOT>/workflow/)
//
// Tests below verify each level wins when it should, and that edge cases
// (no cwd, missing file, broken extends) behave correctly.

// TestResolveFile_ProjectLevelWins verifies that a file placed in the project's
// .wf-agents/ directory takes priority over both the preset and plugin default,
// even when the same filename exists in all three locations.
// Expected winner: project level → path contains ".wf-agents".
func TestResolveFile_ProjectLevelWins(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	// Plugin default: PLANNING.md exists at level 3
	workflowDefaultDir := filepath.Join(root, "workflow")
	require.NoError(t, os.MkdirAll(workflowDefaultDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowDefaultDir, "PLANNING.md"), []byte("plugin"), 0644))

	// Preset: PLANNING.md also exists at level 2
	presetDir := filepath.Join(root, "presets", "myorg", "go")
	require.NoError(t, os.MkdirAll(presetDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(presetDir, "PLANNING.md"), []byte("preset"), 0644))

	// Project: PLANNING.md exists at level 1 — this must win
	projectDir := t.TempDir()
	wfDir := filepath.Join(projectDir, ".wf-agents")
	require.NoError(t, os.MkdirAll(wfDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(wfDir, "workflow.yaml"), []byte("extends: myorg/go"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(wfDir, "PLANNING.md"), []byte("project"), 0644))

	got, err := ResolveFile("PLANNING.md", projectDir, workflowDefaultDir)
	require.NoError(t, err)
	// Resolved path must point into the project's .wf-agents directory, not the preset or plugin default.
	assert.Equal(t, filepath.Join(wfDir, "PLANNING.md"), got)
	assert.Contains(t, got, ".wf-agents", "expected project-level path, got %s", got)
}

// TestResolveFile_PresetFallback verifies that when the project has no PLANNING.md
// but its workflow.yaml specifies "extends: myorg/go", the preset's file is used.
// Expected winner: preset level → path contains "presets".
func TestResolveFile_PresetFallback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	// Plugin default: PLANNING.md at level 3
	workflowDefaultDir := filepath.Join(root, "workflow")
	require.NoError(t, os.MkdirAll(workflowDefaultDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowDefaultDir, "PLANNING.md"), []byte("plugin"), 0644))

	// Preset: PLANNING.md at level 2 — this must win because project has no file
	presetDir := filepath.Join(root, "presets", "myorg", "go")
	require.NoError(t, os.MkdirAll(presetDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(presetDir, "PLANNING.md"), []byte("preset"), 0644))

	// Project: workflow.yaml extends the preset but has no PLANNING.md override
	projectDir := t.TempDir()
	wfDir := filepath.Join(projectDir, ".wf-agents")
	require.NoError(t, os.MkdirAll(wfDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(wfDir, "workflow.yaml"), []byte("extends: myorg/go"), 0644))

	got, err := ResolveFile("PLANNING.md", projectDir, workflowDefaultDir)
	require.NoError(t, err)
	// Resolved path must point into the preset directory, not the plugin default.
	assert.Equal(t, filepath.Join(presetDir, "PLANNING.md"), got)
	assert.Contains(t, got, "presets", "expected preset-level path, got %s", got)
}

// TestResolveFile_PluginDefaultFallback verifies that when neither the project nor
// any preset has the file, resolution falls back to workflowDefaultDir.
// Expected winner: plugin default level → path contains the workflowDefaultDir.
func TestResolveFile_PluginDefaultFallback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	// Plugin default: only location that has PLANNING.md
	workflowDefaultDir := filepath.Join(root, "workflow")
	require.NoError(t, os.MkdirAll(workflowDefaultDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowDefaultDir, "PLANNING.md"), []byte("plugin"), 0644))

	// Project: exists but has no .wf-agents dir, so no project file and no preset reference
	projectDir := t.TempDir()

	got, err := ResolveFile("PLANNING.md", projectDir, workflowDefaultDir)
	require.NoError(t, err)
	// Resolved path must be the plugin default — no project or preset override exists.
	assert.Equal(t, filepath.Join(workflowDefaultDir, "PLANNING.md"), got)
	assert.Contains(t, got, "workflow", "expected plugin-default-level path, got %s", got)
}

// TestResolveFile_NoCwd verifies that passing an empty cwd skips the project
// and preset levels entirely and falls straight through to the plugin default.
// Expected winner: plugin default level (project lookup is skipped when cwd == "").
func TestResolveFile_NoCwd(t *testing.T) {
	root := t.TempDir()
	workflowDefaultDir := filepath.Join(root, "workflow")
	require.NoError(t, os.MkdirAll(workflowDefaultDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowDefaultDir, "PLANNING.md"), []byte("plugin"), 0644))

	// cwd="" means no project-level or preset lookup is attempted
	got, err := ResolveFile("PLANNING.md", "", workflowDefaultDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(workflowDefaultDir, "PLANNING.md"), got)
}

// TestResolveFile_NotFoundAnywhere verifies that an error is returned (and the
// filename appears in the message) when no level has the requested file.
func TestResolveFile_NotFoundAnywhere(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	// workflowDefaultDir exists but contains no PLANNING.md
	workflowDefaultDir := filepath.Join(root, "workflow")
	require.NoError(t, os.MkdirAll(workflowDefaultDir, 0755))

	projectDir := t.TempDir()

	_, err := ResolveFile("PLANNING.md", projectDir, workflowDefaultDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PLANNING.md")
}

// TestResolveFile_EmptyPluginDefaultDir verifies that when workflowDefaultDir is
// empty and no project or preset file exists, an error is returned.
func TestResolveFile_EmptyPluginDefaultDir(t *testing.T) {
	projectDir := t.TempDir()
	// No .wf-agents dir, no preset, and workflowDefaultDir="" — nothing can be found

	_, err := ResolveFile("PLANNING.md", projectDir, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PLANNING.md")
}

// TestResolveFile_PresetExtendsSetButDirMissing verifies that when workflow.yaml
// contains an "extends" key pointing to a preset that does not exist under
// CLAUDE_PLUGIN_ROOT/presets/, an error is returned immediately instead of
// silently falling back to the plugin default.
// This prevents misconfigured presets from going unnoticed.
func TestResolveFile_PresetExtendsSetButDirMissing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	// Plugin default exists — but should NOT be used because extends is set and broken
	workflowDefaultDir := filepath.Join(root, "workflow")
	require.NoError(t, os.MkdirAll(workflowDefaultDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowDefaultDir, "PLANNING.md"), []byte("plugin"), 0644))

	// Project: workflow.yaml references a preset that doesn't exist on disk
	projectDir := t.TempDir()
	wfDir := filepath.Join(projectDir, ".wf-agents")
	require.NoError(t, os.MkdirAll(wfDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(wfDir, "workflow.yaml"), []byte("extends: nonexistent/preset"), 0644))

	_, err := ResolveFile("PLANNING.md", projectDir, workflowDefaultDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "preset")
}
