package phasedocs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eklemin/wf-agents/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FullInstructions loads phase docs using the same three-level priority as ResolveFile:
//   1. project:  <cwd>/.wf-agents/phases/<PHASE>.md
//   2. preset:   <CLAUDE_PLUGIN_ROOT>/presets/<org>/<name>/phases/<PHASE>.md  (from workflow.yaml "extends")
//   3. default:  <CLAUDE_PLUGIN_ROOT>/workflow/phases/<PHASE>.md
//
// Tests below verify that the correct level wins in each scenario.

// TestFullInstructions_FallsBackToPluginDefault is a smoke test that runs against
// the real CLAUDE_PLUGIN_ROOT on disk (no temp dir). It verifies that calling
// FullInstructions without a project dir either returns non-empty content
// (plugin default found) or an error — both are acceptable depending on env.
func TestFullInstructions_FallsBackToPluginDefault(t *testing.T) {
	// No project override, no preset — expects plugin default or graceful error
	result, err := FullInstructions(model.Phase("PLANNING"), "", false)
	if err == nil {
		assert.NotEmpty(t, result)
	}
}

// TestFullInstructions_ProjectFileWins verifies that a PLANNING.md placed in the
// project's .wf-agents/ directory is returned as the phase instructions, even
// when a plugin default would otherwise be available.
// Expected winner: project level — content matches "# project PLANNING".
func TestFullInstructions_ProjectFileWins(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	// Plugin default: workflow/team-lead.md must exist for team-lead preamble resolution
	require.NoError(t, os.MkdirAll(filepath.Join(root, "workflow"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "workflow", "team-lead.md"), []byte("# Team Lead"), 0644))

	// Project: PLANNING.md at level 1 — must win over any default
	projectDir := t.TempDir()
	wfDir := filepath.Join(projectDir, ".wf-agents")
	require.NoError(t, os.MkdirAll(filepath.Join(wfDir, "phases"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(wfDir, "phases", "PLANNING.md"), []byte("# project PLANNING"), 0644))

	result, err := FullInstructions(model.Phase("PLANNING"), projectDir, false)
	require.NoError(t, err)
	// Content must come from the project file, not any preset or plugin default.
	assert.Contains(t, result, "# project PLANNING")
}

// TestFullInstructions_PresetPhaseDocs verifies that when the project has no
// PLANNING.md override but its workflow.yaml extends a preset, the preset's
// PLANNING.md is used as the phase instructions.
// Expected winner: preset level — content matches "# preset PLANNING".
func TestFullInstructions_PresetPhaseDocs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	// Plugin default: workflow/team-lead.md must exist for team-lead preamble resolution
	require.NoError(t, os.MkdirAll(filepath.Join(root, "workflow"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "workflow", "team-lead.md"), []byte("# Team Lead"), 0644))

	// Preset: PLANNING.md at level 2 — should win when project has no override
	presetDir := filepath.Join(root, "presets", "myorg", "go")
	require.NoError(t, os.MkdirAll(filepath.Join(presetDir, "phases"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(presetDir, "phases", "PLANNING.md"), []byte("# preset PLANNING"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(presetDir, "workflow.yaml"), []byte("tracking: {}"), 0644))

	// Project: references the preset via "extends" but provides no PLANNING.md override
	projectDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, ".wf-agents"), 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(projectDir, ".wf-agents", "workflow.yaml"),
		[]byte("extends: myorg/go"),
		0644,
	))

	result, err := FullInstructions(model.Phase("PLANNING"), projectDir, false)
	require.NoError(t, err)
	// Content must come from the preset, not the project or plugin default.
	assert.Contains(t, result, "# preset PLANNING")
}

// TestFullInstructions_ProjectOverridesPreset verifies that when both the project
// and the preset have a PLANNING.md, the project's file takes priority.
// Expected winner: project level — preset content must not appear in the result.
func TestFullInstructions_ProjectOverridesPreset(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	// Plugin default: workflow/team-lead.md must exist for team-lead preamble resolution
	require.NoError(t, os.MkdirAll(filepath.Join(root, "workflow"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "workflow", "team-lead.md"), []byte("# Team Lead"), 0644))

	// Preset: PLANNING.md at level 2 — must NOT win when project has an override
	presetDir := filepath.Join(root, "presets", "myorg", "go")
	require.NoError(t, os.MkdirAll(filepath.Join(presetDir, "phases"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(presetDir, "phases", "PLANNING.md"), []byte("# preset PLANNING"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(presetDir, "workflow.yaml"), []byte("tracking: {}"), 0644))

	// Project: has both a workflow.yaml extending the preset AND a local PLANNING.md override
	projectDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, ".wf-agents", "phases"), 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(projectDir, ".wf-agents", "workflow.yaml"),
		[]byte("extends: myorg/go"),
		0644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(projectDir, ".wf-agents", "phases", "PLANNING.md"),
		[]byte("# project PLANNING"),
		0644,
	))

	result, err := FullInstructions(model.Phase("PLANNING"), projectDir, false)
	require.NoError(t, err)
	// Project file wins — preset content must be absent.
	assert.Contains(t, result, "# project PLANNING")
	assert.NotContains(t, result, "# preset PLANNING")
}

// TestFullInstructions_AllSourcesMissing_ReturnsError verifies that when
// CLAUDE_PLUGIN_ROOT points to an empty directory (no workflow/ subdir, no
// presets) and the project has no phase doc, an error is returned containing
// both "phase instructions not found" and the phase name.
// This ensures callers get a clear message rather than empty instructions.
func TestFullInstructions_AllSourcesMissing_ReturnsError(t *testing.T) {
	// CLAUDE_PLUGIN_ROOT has team-lead.md but no phase docs — no workflow/ PLANNING.md, no presets
	root := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "workflow"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "workflow", "team-lead.md"), []byte("# Team Lead"), 0644))

	// Project dir exists but has no .wf-agents/PLANNING.md
	projectDir := t.TempDir()

	_, err := FullInstructions(model.Phase("PLANNING"), projectDir, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "phase instructions not found")
	assert.Contains(t, err.Error(), "PLANNING")
}

// TestFullInstructions_AgentFileResolvesViaWorkflow verifies that the {{AGENT_FILE}}
// placeholder in phase docs is replaced with a path that comes from
// workflow/team-lead.md (not the hardcoded agents/iriski-team-lead.md path).
// Expected: the substituted path points into workflow/, not agents/.
func TestFullInstructions_AgentFileResolvesViaWorkflow(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	// Plugin default team-lead.md lives in workflow/, not agents/
	require.NoError(t, os.MkdirAll(filepath.Join(root, "workflow", "phases"), 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "workflow", "team-lead.md"),
		[]byte("# Team Lead Protocol"),
		0644,
	))

	// Phase doc that uses {{AGENT_FILE}} placeholder
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "workflow", "phases", "PLANNING.md"),
		[]byte("Re-read your instructions at {{AGENT_FILE}}"),
		0644,
	))

	projectDir := t.TempDir()

	result, err := FullInstructions(model.Phase("PLANNING"), projectDir, false)
	require.NoError(t, err)

	// {{AGENT_FILE}} must resolve to workflow/team-lead.md, not agents/iriski-team-lead.md
	assert.Contains(t, result, "workflow/team-lead.md")
	assert.NotContains(t, result, "agents/iriski-team-lead.md")
}

// TestFullInstructions_AgentFileResolvesPresetOverride verifies that when a preset
// provides its own team-lead.md, the {{AGENT_FILE}} placeholder resolves to
// the preset's copy (not the plugin workflow/ default).
func TestFullInstructions_AgentFileResolvesPresetOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	// Plugin default: workflow/team-lead.md
	require.NoError(t, os.MkdirAll(filepath.Join(root, "workflow", "phases"), 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "workflow", "team-lead.md"),
		[]byte("# Default Team Lead"),
		0644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "workflow", "phases", "PLANNING.md"),
		[]byte("Agent: {{AGENT_FILE}}"),
		0644,
	))

	// Preset: has its own team-lead.md — must win when project extends it
	presetDir := filepath.Join(root, "presets", "myorg", "go")
	require.NoError(t, os.MkdirAll(presetDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(presetDir, "team-lead.md"), []byte("# Preset Team Lead"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(presetDir, "workflow.yaml"), []byte("tracking: {}"), 0644))

	// Project: references the preset via "extends"
	projectDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, ".wf-agents"), 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(projectDir, ".wf-agents", "workflow.yaml"),
		[]byte("extends: myorg/go"),
		0644,
	))

	result, err := FullInstructions(model.Phase("PLANNING"), projectDir, false)
	require.NoError(t, err)

	// {{AGENT_FILE}} must resolve to preset's team-lead.md
	assert.Contains(t, result, filepath.Join(presetDir, "team-lead.md"))
}

// TestFullInstructions_TeamLeadMissing_ReturnsError verifies that when team-lead.md
// is absent from all three levels (project, preset, plugin default), FullInstructions
// returns an error mentioning "team-lead protocol not found" rather than silently
// using a hardcoded fallback path.
func TestFullInstructions_TeamLeadMissing_ReturnsError(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	// workflow/ dir exists but contains no team-lead.md
	require.NoError(t, os.MkdirAll(filepath.Join(root, "workflow"), 0755))

	// Project dir exists but has no team-lead.md override
	projectDir := t.TempDir()

	_, err := FullInstructions(model.Phase("PLANNING"), projectDir, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "team-lead protocol not found")
}
