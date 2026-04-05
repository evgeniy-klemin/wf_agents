package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginRoot_EnvVar(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", "/some/plugin/root")
	got, err := PluginRoot()
	require.NoError(t, err)
	assert.Equal(t, "/some/plugin/root", got)
}

func TestPluginRoot_ExeFallback(t *testing.T) {
	// Unset env var to force exe fallback
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	got, err := PluginRoot()
	require.NoError(t, err)
	// The result should be non-empty — we can't assert the exact path since
	// os.Executable() returns the test binary path, but we can verify it's set.
	assert.NotEmpty(t, got)
}

func TestPluginRoot_EnvVarTakesPrecedenceOverExe(t *testing.T) {
	expected := "/explicit/plugin/root"
	t.Setenv("CLAUDE_PLUGIN_ROOT", expected)

	got, err := PluginRoot()
	require.NoError(t, err)
	assert.Equal(t, expected, got)

	// Now clear env and confirm we get a different (exe-based) result
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	got2, err := PluginRoot()
	require.NoError(t, err)
	assert.NotEqual(t, expected, got2)
}
